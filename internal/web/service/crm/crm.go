// Package crm connects the panel to an external CRM: signed lifecycle webhooks (with a DB
// outbox so nothing is lost while the CRM is down) and single-use SSO login links.
package crm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const (
	// EventPing is sent by the settings page "test" button only.
	EventPing          = "crm.ping"
	maxAttempts        = 20
	deliverBatch       = 50
	deliverTimeout     = 10 * time.Second
	expiringWindow     = 3 * 24 * time.Hour
	trafficWarnPercent = 80
	ssoTTL             = 2 * time.Minute
)

// Forwarded lists every event type the webhook can carry; crmWebhookEvents narrows it.
var Forwarded = []eventbus.EventType{
	eventbus.EventClientTraffic80, eventbus.EventClientDepleted,
	eventbus.EventClientExpiring, eventbus.EventClientExpired,
	eventbus.EventLineDown, eventbus.EventLineUp,
	eventbus.EventNodeDown, eventbus.EventNodeUp,
	eventbus.EventVpsRenew, eventbus.EventXrayRollback,
}

type Service struct {
	settingService service.SettingService
	client         *http.Client
	now            func() time.Time
}

func NewService() *Service {
	return &Service{client: &http.Client{Timeout: deliverTimeout}, now: time.Now}
}

// Payload is the JSON body POSTed to the CRM.
type Payload struct {
	ID     string `json:"id"`
	Event  string `json:"event"`
	Source string `json:"source"`
	Time   int64  `json:"time"`
	Data   any    `json:"data,omitempty"`
}

func (s *Service) enabledURL() (string, bool) {
	on, err := s.settingService.GetCrmWebhookEnable()
	if err != nil || !on {
		return "", false
	}
	u, err := s.settingService.GetCrmWebhookUrl()
	if err != nil || strings.TrimSpace(u) == "" {
		return "", false
	}
	return strings.TrimSpace(u), true
}

func (s *Service) wants(t eventbus.EventType) bool {
	known := false
	for _, f := range Forwarded {
		if f == t {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	list, _ := s.settingService.GetCrmWebhookEvents()
	if strings.TrimSpace(list) == "" {
		return true
	}
	for e := range strings.SplitSeq(list, ",") {
		if strings.TrimSpace(e) == string(t) {
			return true
		}
	}
	return false
}

// HandleEvent is the eventbus subscriber: it only queues, delivery runs from the job.
func (s *Service) HandleEvent(e eventbus.Event) {
	if _, ok := s.enabledURL(); !ok || !s.wants(e.Type) {
		return
	}
	if err := s.Enqueue(e); err != nil {
		logger.Warning("crm: enqueue webhook failed:", err)
	}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) Enqueue(e eventbus.Event) error {
	return s.enqueueTx(database.GetDB(), e)
}

func (s *Service) enqueueTx(tx *gorm.DB, e eventbus.Event) error {
	ts := e.Timestamp
	if ts.IsZero() {
		ts = s.now()
	}
	body, err := json.Marshal(Payload{ID: newID(), Event: string(e.Type), Source: e.Source, Time: ts.Unix(), Data: e.Data})
	if err != nil {
		return err
	}
	return tx.Create(&model.CrmWebhookOutbox{Event: string(e.Type), Payload: string(body), NextAt: 0}).Error
}

// Sign returns the X-3xui-Signature value: HMAC-SHA256 over "<timestamp>.<body>".
func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) post(ctx context.Context, target, event string, body []byte) error {
	secret, _ := s.settingService.GetCrmWebhookSecret()
	ts := s.now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "3x-ui-crm-webhook")
	req.Header.Set("X-3xui-Event", event)
	req.Header.Set("X-3xui-Timestamp", strconv.FormatInt(ts, 10))
	if secret != "" {
		req.Header.Set("X-3xui-Signature", Sign(secret, ts, body))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("CRM answered HTTP %d", resp.StatusCode)
	}
	return nil
}

func backoff(attempts int) time.Duration {
	d := time.Duration(1<<min(attempts, 10)) * 15 * time.Second
	return min(d, 6*time.Hour)
}

// DeliverPending sends due outbox rows; it returns how many were delivered.
func (s *Service) DeliverPending(ctx context.Context) (int, error) {
	target, ok := s.enabledURL()
	if !ok {
		return 0, nil
	}
	db := database.GetDB()
	now := s.now()
	var rows []model.CrmWebhookOutbox
	if err := db.Where("next_at <= ?", now.Unix()).Order("id asc").Limit(deliverBatch).Find(&rows).Error; err != nil {
		return 0, err
	}
	sent := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		if err := s.post(ctx, target, row.Event, []byte(row.Payload)); err != nil {
			if ctx.Err() != nil {
				break
			}
			row.Attempts++
			if row.Attempts >= maxAttempts {
				logger.Warningf("crm: dropping webhook %s after %d attempts: %v", row.Event, row.Attempts, err)
				db.Delete(&model.CrmWebhookOutbox{}, row.Id)
				continue
			}
			db.Model(&model.CrmWebhookOutbox{}).Where("id = ?", row.Id).Updates(map[string]any{
				"attempts": row.Attempts, "next_at": now.Add(backoff(row.Attempts)).Unix(), "last_error": err.Error(),
			})
			continue
		}
		db.Delete(&model.CrmWebhookOutbox{}, row.Id)
		sent++
	}
	return sent, nil
}

// Test posts a crm.ping straight to the CRM (no outbox) so the settings page can show the result.
func (s *Service) Test(ctx context.Context) error {
	target, ok := s.enabledURL()
	if !ok {
		return errors.New("CRM webhook is disabled or has no URL")
	}
	body, _ := json.Marshal(Payload{ID: newID(), Event: EventPing, Source: "3x-ui", Time: s.now().Unix()})
	return s.post(ctx, target, EventPing, body)
}

type lifecycleRow struct {
	Email      string
	SubID      string
	GroupName  string
	Up         int64
	Down       int64
	Total      int64
	ExpiryTime int64
	Enable     bool
}

func lifecycleConditions(r lifecycleRow, now time.Time) map[eventbus.EventType]bool {
	used := r.Up + r.Down
	nowMs := now.UnixMilli()
	return map[eventbus.EventType]bool{
		eventbus.EventClientDepleted:  r.Total > 0 && used >= r.Total,
		eventbus.EventClientTraffic80: r.Total > 0 && used*100 >= r.Total*trafficWarnPercent && used < r.Total,
		eventbus.EventClientExpired:   r.ExpiryTime > 0 && r.ExpiryTime <= nowMs,
		eventbus.EventClientExpiring:  r.ExpiryTime > nowMs && r.ExpiryTime <= now.Add(expiringWindow).UnixMilli(),
	}
}

// ScanLifecycle queues client events whose condition newly became true, marking them in the same
// transaction so each fires once; marks whose condition cleared are removed so renewals fire again.
func (s *Service) ScanLifecycle() ([]eventbus.Event, error) {
	db := database.GetDB()
	var rows []lifecycleRow
	err := db.Table("client_traffics AS t").
		Select("t.email, c.sub_id, c.group_name, t.up, t.down, t.total, t.expiry_time, t.enable").
		Joins("LEFT JOIN clients c ON c.email = t.email").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	var marks []model.CrmEventMark
	if err := db.Find(&marks).Error; err != nil {
		return nil, err
	}
	marked := make(map[string]int, len(marks))
	for _, m := range marks {
		marked[m.Email+"\x00"+m.Event] = m.Id
	}
	now := s.now()
	var events []eventbus.Event
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if seen[r.Email] {
			continue
		}
		seen[r.Email] = true
		for t, on := range lifecycleConditions(r, now) {
			key := r.Email + "\x00" + string(t)
			id, has := marked[key]
			switch {
			case on && !has:
				e := eventbus.Event{Type: t, Source: r.Email, Timestamp: now, Data: &eventbus.ClientLifecycleData{
					Email: r.Email, SubID: r.SubID, Group: r.GroupName, Up: r.Up, Down: r.Down, Total: r.Total, ExpiryTime: r.ExpiryTime,
				}}
				err := db.Transaction(func(tx *gorm.DB) error {
					if err := tx.Create(&model.CrmEventMark{Email: r.Email, Event: string(t)}).Error; err != nil {
						return err
					}
					if !s.wants(t) {
						return nil
					}
					return s.enqueueTx(tx, e)
				})
				if err != nil {
					return events, err
				}
				events = append(events, e)
			case !on && has:
				db.Delete(&model.CrmEventMark{}, id)
			}
		}
	}
	// Clients that no longer exist must not keep marks around.
	for key, id := range marked {
		email, _, _ := strings.Cut(key, "\x00")
		if !seen[email] {
			db.Delete(&model.CrmEventMark{}, id)
		}
	}
	return events, nil
}

// MintSSO creates a single-use login ticket; redirect must be a panel-relative path.
func (s *Service) MintSSO(username, redirect string) (string, int64, error) {
	redirect = strings.TrimSpace(redirect)
	if redirect != "" && !validRedirect(redirect) {
		return "", 0, errors.New("redirect must be a path inside the panel, e.g. /panel/clients")
	}
	db := database.GetDB()
	var user model.User
	q := db.Order("id asc")
	if strings.TrimSpace(username) != "" {
		q = q.Where("username = ?", strings.TrimSpace(username))
	}
	if err := q.First(&user).Error; err != nil {
		return "", 0, errors.New("panel user not found")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", 0, err
	}
	token := hex.EncodeToString(b)
	exp := s.now().Add(ssoTTL).Unix()
	db.Where("expires_at < ?", s.now().Unix()).Delete(&model.SsoTicket{})
	if err := db.Create(&model.SsoTicket{Token: token, Username: user.Username, Redirect: redirect, ExpiresAt: exp, LoginEpoch: user.LoginEpoch}).Error; err != nil {
		return "", 0, err
	}
	return token, exp, nil
}

// ConsumeSSO redeems a ticket exactly once.
func (s *Service) ConsumeSSO(token string) (*model.User, string, error) {
	db := database.GetDB()
	var ticket model.SsoTicket
	if err := db.Where("token = ?", token).First(&ticket).Error; err != nil {
		return nil, "", errors.New("invalid or used login link")
	}
	res := db.Where("token = ?", token).Delete(&model.SsoTicket{})
	if res.Error != nil || res.RowsAffected != 1 {
		return nil, "", errors.New("invalid or used login link")
	}
	if ticket.ExpiresAt < s.now().Unix() {
		return nil, "", errors.New("login link expired")
	}
	var user model.User
	if err := db.Where("username = ?", ticket.Username).First(&user).Error; err != nil {
		return nil, "", errors.New("panel user not found")
	}
	if user.LoginEpoch != ticket.LoginEpoch {
		return nil, "", errors.New("login link revoked by a password change or session reset")
	}
	return &user, ticket.Redirect, nil
}

// PeekSSO reports whether a ticket is currently redeemable without consuming it.
func (s *Service) PeekSSO(token string) bool {
	var count int64
	database.GetDB().Model(&model.SsoTicket{}).Where("token = ? AND expires_at >= ?", token, s.now().Unix()).Count(&count)
	return count == 1
}

var redirectRe = regexp.MustCompile(`^/panel(/[A-Za-z0-9._~\-/]*)?(\?[A-Za-z0-9._~\-=&%]*)?(#[A-Za-z0-9._~\-/=&%]*)?$`)

func validRedirect(r string) bool {
	if !redirectRe.MatchString(r) || strings.Contains(r, "//") || strings.Contains(r, "..") {
		return false
	}
	u, err := url.Parse(r)
	return err == nil && !u.IsAbs() && u.Host == ""
}
