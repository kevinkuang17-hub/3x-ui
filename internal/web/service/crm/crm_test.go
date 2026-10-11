package crm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

var testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func setupCRM(t *testing.T, settings map[string]string) *Service {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	for k, v := range settings {
		if err := database.GetDB().Create(&model.Setting{Key: k, Value: v}).Error; err != nil {
			t.Fatalf("seed setting %s: %v", k, err)
		}
	}
	s := NewService()
	s.now = func() time.Time { return testNow }
	return s
}

func seedTraffic(t *testing.T, email string, up, total, expiry int64) {
	t.Helper()
	db := database.GetDB()
	if err := db.Create(&model.ClientRecord{Email: email, SubID: "sub-" + email, Enable: true, Group: "vip"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: 1, Email: email, Up: up, Total: total, ExpiryTime: expiry, Enable: true}).Error; err != nil {
		t.Fatal(err)
	}
}

func eventTypes(events []eventbus.Event) map[string]eventbus.EventType {
	out := map[string]eventbus.EventType{}
	for _, e := range events {
		out[e.Source+"|"+string(e.Type)] = e.Type
	}
	return out
}

func TestScanLifecycleFiresOncePerCrossing(t *testing.T) {
	s := setupCRM(t, nil)
	day := int64(24 * time.Hour / time.Millisecond)
	seedTraffic(t, "full", 100, 100, 0)
	seedTraffic(t, "warn", 85, 100, 0)
	seedTraffic(t, "soon", 0, 0, testNow.UnixMilli()+day)
	seedTraffic(t, "gone", 0, 0, testNow.UnixMilli()-day)
	seedTraffic(t, "fine", 10, 100, testNow.UnixMilli()+30*day)

	first, err := s.ScanLifecycle()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	got := eventTypes(first)
	want := []string{"full|client.depleted", "warn|client.traffic80", "soon|client.expiring", "gone|client.expired"}
	if len(got) != len(want) {
		t.Fatalf("first scan events = %v, want %v", got, want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing %s in %v", k, got)
		}
	}
	for _, e := range first {
		if d, ok := e.Data.(*eventbus.ClientLifecycleData); !ok || d.SubID != "sub-"+e.Source || d.Group != "vip" {
			t.Fatalf("event data = %+v", e.Data)
		}
	}

	if again, _ := s.ScanLifecycle(); len(again) != 0 {
		t.Fatalf("second scan must be silent, got %v", eventTypes(again))
	}

	// A traffic reset clears the mark, so the next exhaustion fires again.
	database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "full").Update("up", 0)
	if ev, _ := s.ScanLifecycle(); len(ev) != 0 {
		t.Fatalf("reset itself must not fire, got %v", eventTypes(ev))
	}
	database.GetDB().Model(&xray.ClientTraffic{}).Where("email = ?", "full").Update("up", 100)
	ev, _ := s.ScanLifecycle()
	if _, ok := eventTypes(ev)["full|client.depleted"]; !ok || len(ev) != 1 {
		t.Fatalf("re-exhaustion must fire once, got %v", eventTypes(ev))
	}
}

func TestDeliverPendingSignsAndRetries(t *testing.T) {
	var mu sync.Mutex
	status := http.StatusInternalServerError
	var gotSig, gotTS, gotEvent string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotSig, gotTS, gotEvent = r.Header.Get("X-3xui-Signature"), r.Header.Get("X-3xui-Timestamp"), r.Header.Get("X-3xui-Event")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	s := setupCRM(t, map[string]string{"crmWebhookEnable": "true", "crmWebhookUrl": srv.URL, "crmWebhookSecret": "s3cret"})

	s.HandleEvent(eventbus.Event{Type: eventbus.EventLineDown, Source: "HK A", Data: &eventbus.LineHealthData{InboundId: 7}})
	s.HandleEvent(eventbus.Event{Type: eventbus.EventLoginAttempt, Source: "x"})

	sent, err := s.DeliverPending(context.Background())
	if err != nil || sent != 0 {
		t.Fatalf("failing CRM: sent=%d err=%v", sent, err)
	}
	var row model.CrmWebhookOutbox
	if err := database.GetDB().First(&row).Error; err != nil {
		t.Fatalf("failed delivery must stay queued: %v", err)
	}
	if row.Attempts != 1 || row.NextAt <= testNow.Unix() {
		t.Fatalf("retry not scheduled: %+v", row)
	}
	var count int64
	database.GetDB().Model(&model.CrmWebhookOutbox{}).Count(&count)
	if count != 1 {
		t.Fatalf("login events must not be forwarded, outbox=%d", count)
	}

	mu.Lock()
	status = http.StatusOK
	mu.Unlock()
	s.now = func() time.Time { return testNow.Add(time.Hour) }
	if sent, err := s.DeliverPending(context.Background()); err != nil || sent != 1 {
		t.Fatalf("recovered CRM: sent=%d err=%v", sent, err)
	}
	database.GetDB().Model(&model.CrmWebhookOutbox{}).Count(&count)
	if count != 0 {
		t.Fatal("delivered row must be removed")
	}

	mu.Lock()
	defer mu.Unlock()
	ts, _ := strconv.ParseInt(gotTS, 10, 64)
	if gotEvent != "line.down" || gotSig != Sign("s3cret", ts, gotBody) {
		t.Fatalf("bad headers: event=%q sig=%q", gotEvent, gotSig)
	}
	var p Payload
	if err := json.Unmarshal(gotBody, &p); err != nil || p.Event != "line.down" || p.Source != "HK A" || p.ID == "" {
		t.Fatalf("payload = %+v, err=%v", p, err)
	}
}

func TestEventFilterSetting(t *testing.T) {
	s := setupCRM(t, map[string]string{"crmWebhookEvents": "client.expired, line.down"})
	if !s.wants(eventbus.EventLineDown) || s.wants(eventbus.EventNodeUp) {
		t.Fatal("event list must narrow the forwarded set")
	}
}

func TestSSOTicketIsSingleUseAndShortLived(t *testing.T) {
	s := setupCRM(t, nil)
	if err := database.GetDB().Create(&model.User{Username: "crm-admin", Password: "x"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"https://evil.example/", "//evil.example/", "panel"} {
		if _, _, err := s.MintSSO("crm-admin", bad); err == nil {
			t.Fatalf("redirect %q must be rejected", bad)
		}
	}
	token, exp, err := s.MintSSO("crm-admin", "/panel/clients")
	if err != nil || exp != testNow.Add(ssoTTL).Unix() || len(token) != 64 {
		t.Fatalf("mint: token=%q exp=%d err=%v", token, exp, err)
	}
	user, redirect, err := s.ConsumeSSO(token)
	if err != nil || user.Username != "crm-admin" || redirect != "/panel/clients" {
		t.Fatalf("consume: user=%v redirect=%q err=%v", user, redirect, err)
	}
	if _, _, err := s.ConsumeSSO(token); err == nil {
		t.Fatal("a ticket must not be reusable")
	}

	token, _, _ = s.MintSSO("crm-admin", "")
	s.now = func() time.Time { return testNow.Add(ssoTTL + time.Second) }
	if _, _, err := s.ConsumeSSO(token); err == nil {
		t.Fatal("an expired ticket must be refused")
	}
	if _, _, err := s.MintSSO("nobody", ""); err == nil {
		t.Fatal("unknown user must be refused")
	}
}
