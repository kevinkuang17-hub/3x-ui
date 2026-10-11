package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/netsafe"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
)

// cloudflareAPI is a variable so tests can point the client at a fake server.
var cloudflareAPI = "https://api.cloudflare.com/client/v4"

var (
	observedIPs   sync.Map // node id -> public IPv4 last reported by its heartbeat
	cfZoneIDCache sync.Map // zone name -> zone id
	dnsAttempts   sync.Map // node id -> time of the last automatic update attempt
)

const dnsAutoRetry = 5 * time.Minute

// NodeDnsService keeps Cloudflare A records pointed at node IPs, so an IP change on a
// server only needs a DNS update (automatic when cfAutoDns is on) instead of new configs.
type NodeDnsService struct {
	settingService SettingService
	client         *http.Client
	// probe is swappable in tests; it reports whether the master can manage the node.
	probe func(ctx context.Context, n *model.Node) error
}

func (s *NodeDnsService) probeNode(ctx context.Context, n *model.Node) error {
	if s.probe != nil {
		return s.probe(ctx, n)
	}
	_, err := (&NodeService{}).Probe(ctx, n)
	return err
}

func newReportKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// LookupA returns the current content of fqdn's A record, if any.
func (s *NodeDnsService) LookupA(ctx context.Context, fqdn string) (string, bool, error) {
	zid, err := s.zoneID(ctx)
	if err != nil {
		return "", false, err
	}
	var records []struct {
		Content string `json:"content"`
	}
	q := "/zones/" + zid + "/dns_records?type=A&name=" + url.QueryEscape(fqdn)
	if err := s.call(ctx, http.MethodGet, q, nil, &records); err != nil {
		return "", false, err
	}
	if len(records) == 0 {
		return "", false, nil
	}
	return records[0].Content, true, nil
}

func (s *NodeDnsService) httpClient() *http.Client {
	if s.client != nil {
		return s.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// ObservedIP returns the public IPv4 the node last reported, if any.
func ObservedIP(nodeID int) string {
	if v, ok := observedIPs.Load(nodeID); ok {
		return v.(string)
	}
	return ""
}

func (s *NodeDnsService) Configured() bool {
	token, _ := s.settingService.GetCfApiToken()
	zone, _ := s.settingService.GetCfZone()
	return strings.TrimSpace(token) != "" && strings.TrimSpace(zone) != ""
}

// FQDN expands a short label ("hk-a") with the configured zone; full names inside the zone pass.
func (s *NodeDnsService) FQDN(name string) (string, error) {
	zone, _ := s.settingService.GetCfZone()
	zone = strings.Trim(strings.ToLower(strings.TrimSpace(zone)), ".")
	name = strings.Trim(strings.ToLower(strings.TrimSpace(name)), ".")
	if zone == "" {
		return "", common.NewError("set the Cloudflare zone in Settings first")
	}
	if name == "" {
		return "", common.NewError("DNS name is required")
	}
	if name != zone && !strings.HasSuffix(name, "."+zone) {
		name = name + "." + zone
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" ||
			strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", common.NewError("invalid DNS name:", name)
		}
	}
	return name, nil
}

type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

func (s *NodeDnsService) call(ctx context.Context, method, path string, body any, out any) error {
	token, _ := s.settingService.GetCfApiToken()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, cloudflareAPI+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env cfEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare: HTTP %d", resp.StatusCode)
	}
	if !env.Success {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("cloudflare: %s", strings.Join(msgs, "; "))
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

func (s *NodeDnsService) zoneID(ctx context.Context) (string, error) {
	zone, _ := s.settingService.GetCfZone()
	zone = strings.Trim(strings.ToLower(strings.TrimSpace(zone)), ".")
	if v, ok := cfZoneIDCache.Load(zone); ok {
		return v.(string), nil
	}
	var zones []struct {
		ID string `json:"id"`
	}
	if err := s.call(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(zone), nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("cloudflare: zone %s not found for this API token", zone)
	}
	cfZoneIDCache.Store(zone, zones[0].ID)
	return zones[0].ID, nil
}

// UpsertA points fqdn at ip (DNS only, TTL 60) creating the record when missing.
func (s *NodeDnsService) UpsertA(ctx context.Context, fqdn, ip string) error {
	if parsed := net.ParseIP(ip); parsed == nil || parsed.To4() == nil {
		return common.NewError("not an IPv4 address:", ip)
	}
	zid, err := s.zoneID(ctx)
	if err != nil {
		return err
	}
	var records []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	q := "/zones/" + zid + "/dns_records?type=A&name=" + url.QueryEscape(fqdn)
	if err := s.call(ctx, http.MethodGet, q, nil, &records); err != nil {
		return err
	}
	rec := map[string]any{"type": "A", "name": fqdn, "content": ip, "ttl": 60, "proxied": false}
	if len(records) == 0 {
		return s.call(ctx, http.MethodPost, "/zones/"+zid+"/dns_records", rec, nil)
	}
	if records[0].Content == ip {
		return nil
	}
	return s.call(ctx, http.MethodPut, "/zones/"+zid+"/dns_records/"+records[0].ID, rec, nil)
}

// List returns the links, dropping those whose node was deleted since.
func (s *NodeDnsService) List() ([]model.NodeDns, error) {
	db := database.GetDB()
	db.Where("node_id NOT IN (?)", db.Model(&model.Node{}).Select("id")).Delete(&model.NodeDns{})
	var rows []model.NodeDns
	err := db.Order("node_id asc").Find(&rows).Error
	return rows, err
}

func (s *NodeDnsService) record(nodeID int) (*model.NodeDns, error) {
	var row model.NodeDns
	err := database.GetDB().Where("node_id = ?", nodeID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// resolveNodeIP picks the IP to publish: explicit, else the heartbeat's report, else a literal address.
func resolveNodeIP(node *model.Node, explicit string) (string, error) {
	if ip := strings.TrimSpace(explicit); ip != "" {
		return ip, nil
	}
	if ip := ObservedIP(node.Id); ip != "" {
		return ip, nil
	}
	if ip := net.ParseIP(node.Address); ip != nil && ip.To4() != nil {
		return ip.String(), nil
	}
	return "", common.NewError("the node's public IP is not known yet; enter it manually")
}

// Set links a node to a DNS name, publishes its IP, and optionally makes the name the node's
// address so the master and every subscription follow future IP changes.
func (s *NodeDnsService) Set(ctx context.Context, nodeID int, name, ip string, useAsAddress bool) (*model.NodeDns, error) {
	if !s.Configured() {
		return nil, common.NewError("set the Cloudflare API token and zone in Settings first")
	}
	node, err := (&NodeService{}).GetById(nodeID)
	if err != nil {
		return nil, err
	}
	fqdn, err := s.FQDN(name)
	if err != nil {
		return nil, err
	}
	addr, err := resolveNodeIP(node, ip)
	if err != nil {
		return nil, err
	}
	if err := s.UpsertA(ctx, fqdn, addr); err != nil {
		return nil, err
	}
	row := &model.NodeDns{NodeId: nodeID, Name: fqdn, LastIp: addr, SyncedAt: time.Now().Unix(), ReportKey: newReportKey()}
	if prev, _ := s.record(nodeID); prev != nil && prev.ReportKey != "" {
		row.ReportKey = prev.ReportKey
	}
	if err := database.GetDB().Save(row).Error; err != nil {
		return nil, err
	}
	if useAsAddress && node.Address != fqdn {
		if err := s.switchAddress(ctx, node, fqdn); err != nil {
			return row, err
		}
	}
	return row, nil
}

// switchAddress moves the node onto its domain only once the master can reach it there, and
// refreshes the cached runtime client so pushes stop going to the old address.
func (s *NodeDnsService) switchAddress(ctx context.Context, node *model.Node, fqdn string) error {
	candidate := *node
	candidate.Address = fqdn
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.probeNode(pctx, &candidate); err != nil {
		return common.NewError("the DNS record is saved, but the master cannot reach the node at", fqdn,
			"yet (DNS still propagating, or the node's certificate is not valid for this name):", err)
	}
	nodes := &NodeService{}
	if err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Node{}).Where("id = ?", node.Id).Update("address", fqdn).Error; err != nil {
			return err
		}
		return nodes.MarkNodeDirtyTx(tx, node.Id)
	}); err != nil {
		return err
	}
	if mgr := runtime.GetManager(); mgr != nil {
		mgr.InvalidateNode(node.Id)
	}
	return nil
}

// Sync republishes the node's record, with ip overriding the detected address ("change IP").
func (s *NodeDnsService) Sync(ctx context.Context, nodeID int, ip string) (*model.NodeDns, error) {
	row, err := s.record(nodeID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, common.NewError("this node has no DNS name yet")
	}
	node, err := (&NodeService{}).GetById(nodeID)
	if err != nil {
		return nil, err
	}
	addr, err := resolveNodeIP(node, ip)
	if err != nil {
		return nil, err
	}
	err = s.UpsertA(ctx, row.Name, addr)
	updates := map[string]any{"synced_at": time.Now().Unix(), "last_error": ""}
	if err != nil {
		updates["last_error"] = err.Error()
	} else {
		updates["last_ip"] = addr
		row.LastIp = addr
	}
	database.GetDB().Model(&model.NodeDns{}).Where("node_id = ?", nodeID).Updates(updates)
	return row, err
}

func (s *NodeDnsService) Unlink(nodeID int) error {
	return database.GetDB().Where("node_id = ?", nodeID).Delete(&model.NodeDns{}).Error
}

var dnsChecks sync.Map // node id -> time of the last full ObserveIP pass

// ShouldObserveIP keeps heartbeats cheap: a full DNS check runs when the reported IP changes,
// and otherwise at most every few minutes (to retry an earlier failed update).
func ShouldObserveIP(nodeID int, ip string) bool {
	if ObservedIP(nodeID) != ip {
		return true
	}
	last, ok := dnsChecks.Load(nodeID)
	return !ok || time.Since(last.(time.Time)) >= dnsAutoRetry
}

// ReportIP handles the node's own cron report. The claimed IP must equal the caller's address,
// so a leaked key can only point the record at the machine holding it (and a proxy in front of
// the master fails loudly instead of publishing the proxy's address).
func (s *NodeDnsService) ReportIP(ctx context.Context, key, claimedIP, remoteIP string) (string, bool, error) {
	ip := net.ParseIP(strings.TrimSpace(claimedIP))
	if key == "" || ip == nil || ip.To4() == nil || netsafe.IsBlockedIP(ip) {
		return "", false, common.NewError("a public IPv4 address is required")
	}
	if remote := net.ParseIP(remoteIP); remote == nil || !remote.Equal(ip) {
		return "", false, common.NewError("the reported IP", ip.String(), "does not match the address the request came from:", remoteIP)
	}
	var row model.NodeDns
	if err := database.GetDB().Where("report_key = ?", key).First(&row).Error; err != nil {
		return "", false, common.NewError("unknown report key")
	}
	addr := ip.String()
	observedIPs.Store(row.NodeId, addr)
	if row.LastIp == addr {
		return addr, false, nil
	}
	if !s.Configured() {
		return addr, false, common.NewError("Cloudflare is not configured on the master")
	}
	if last, ok := dnsAttempts.Load(row.NodeId); ok && time.Since(last.(time.Time)) < time.Minute {
		return addr, false, common.NewError("too many updates; try again in a minute")
	}
	dnsAttempts.Store(row.NodeId, time.Now())
	if _, err := s.Sync(ctx, row.NodeId, addr); err != nil {
		return addr, false, err
	}
	logger.Infof("node dns: %s now points at %s (reported by the node)", row.Name, addr)
	return addr, true, nil
}

// ObserveIP records a heartbeat-reported IP and, with cfAutoDns on, follows a changed IP.
func (s *NodeDnsService) ObserveIP(nodeID int, ip string) {
	if net.ParseIP(ip) == nil {
		return
	}
	observedIPs.Store(nodeID, ip)
	dnsChecks.Store(nodeID, time.Now())
	if on, _ := s.settingService.GetCfAutoDns(); !on || !s.Configured() {
		return
	}
	row, err := s.record(nodeID)
	if err != nil || row == nil || row.LastIp == ip {
		return
	}
	if last, ok := dnsAttempts.Load(nodeID); ok && time.Since(last.(time.Time)) < dnsAutoRetry {
		return
	}
	dnsAttempts.Store(nodeID, time.Now())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := s.Sync(ctx, nodeID, ip); err != nil {
		logger.Warningf("node dns: auto-update %s -> %s failed: %v", row.Name, ip, err)
		return
	}
	logger.Infof("node dns: %s now points at %s", row.Name, ip)
}
