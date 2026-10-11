package service

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

func TestPlanExpiry(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	nowMs := now.UnixMilli()
	month := &model.Plan{Days: 30}
	span := 30 * dayMillis
	cases := []struct {
		name    string
		plan    *model.Plan
		current int64
		mode    string
		want    int64
	}{
		{"unlimited plan clears expiry", &model.Plan{}, nowMs + 5, PlanModeRenew, 0},
		{"renew active extends from expiry", month, nowMs + dayMillis, PlanModeRenew, nowMs + dayMillis + span},
		{"renew expired starts now", month, nowMs - dayMillis, PlanModeRenew, nowMs + span},
		{"renew never-expiring starts now", month, 0, PlanModeRenew, nowMs + span},
		{"reset ignores remaining time", month, nowMs + dayMillis, PlanModeReset, nowMs + span},
		{"renew carries unstarted delay days", month, -dayMillis, PlanModeRenew, nowMs + dayMillis + span},
		{"delay start fresh", &model.Plan{Days: 30, DelayStart: true}, 0, PlanModeRenew, -span},
		{"delay start stacks unused", &model.Plan{Days: 30, DelayStart: true}, -span, PlanModeRenew, -2 * span},
		{"delay start extends running", &model.Plan{Days: 30, DelayStart: true}, nowMs + dayMillis, PlanModeRenew, nowMs + dayMillis + span},
	}
	for _, c := range cases {
		if got := PlanExpiry(c.plan, c.current, c.mode, now); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func TestPlanValidationAndZeroValues(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	svc := &PlanService{}
	if err := svc.Add(&model.Plan{Name: " "}); err == nil {
		t.Fatal("empty name must be rejected")
	}
	if err := svc.Add(&model.Plan{Name: "forever", Days: maxPlanDays + 1}); err == nil {
		t.Fatal("absurd durations must be rejected")
	}
	if err := svc.Add(&model.Plan{Name: "bad", DelayStart: true}); err == nil {
		t.Fatal("delay start without days must be rejected")
	}
	p := &model.Plan{Name: "Monthly 100G", TotalGB: 100 << 30, Days: 30, ResetTraffic: true}
	if err := svc.Add(p); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := svc.Update(p.Id, &model.Plan{Name: "Monthly 100G", TotalGB: 100 << 30, Days: 30, ResetTraffic: false}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := svc.Get(0, "Monthly 100G")
	if err != nil || got.ResetTraffic {
		t.Fatalf("false must persist through update, got %+v err=%v", got, err)
	}
}

func TestPlanApplySetsLimitsResetsTrafficAndGroup(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
	db := database.GetDB()

	const email = "buyer@example.com"
	settings, _ := json.Marshal(map[string]any{"clients": []map[string]any{{
		"email": email, "id": "ce8d33df-3a64-4f10-8f9b-91c3a8e0c222", "enable": false, "subId": "sub-buyer",
		"totalGB": 1 << 30, "expiryTime": 1,
	}}})
	ib := &model.Inbound{Tag: "plan-in", Enable: true, Port: 50101, Protocol: model.VLESS, StreamSettings: `{"network":"tcp"}`, Settings: string(settings)}
	if err := db.Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	clientSvc := ClientService{}
	inboundSvc := InboundService{}
	clients, err := inboundSvc.GetClients(ib)
	if err != nil {
		t.Fatal(err)
	}
	if err := clientSvc.SyncInbound(nil, ib.Id, clients); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&xray.ClientTraffic{InboundId: ib.Id, Email: email, Up: 500, Down: 500, Total: 1 << 30, Enable: false}).Error; err != nil {
		t.Fatal(err)
	}

	plan := &model.Plan{Name: "Monthly", TotalGB: 100 << 30, Days: 30, LimitIp: 3, Group: "vip", ResetTraffic: true}
	before := time.Now()
	res, _, err := (&PlanService{}).Apply(&clientSvc, &inboundSvc, plan, []string{email, "ghost@example.com"}, "")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Applied != 1 || res.Failed["ghost@example.com"] == "" {
		t.Fatalf("result = %+v", res)
	}

	rec, err := clientSvc.GetRecordByEmail(nil, email)
	if err != nil {
		t.Fatal(err)
	}
	if rec.TotalGB != 100<<30 || rec.LimitIP != 3 || !rec.Enable || rec.Group != "vip" {
		t.Fatalf("record = %+v", rec)
	}
	if rec.ExpiryTime < before.UnixMilli()+30*dayMillis-1000 {
		t.Fatalf("expiry %d not ~30 days out", rec.ExpiryTime)
	}
	var tr xray.ClientTraffic
	if err := db.Where("email = ?", email).First(&tr).Error; err != nil {
		t.Fatal(err)
	}
	if tr.Up != 0 || tr.Down != 0 {
		t.Fatalf("traffic not reset: %+v", tr)
	}
}
