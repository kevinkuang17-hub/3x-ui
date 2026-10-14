package service

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
)

func TestRealitySelfStream(t *testing.T) {
	stream := map[string]any{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": map[string]any{
			"dest": "www.cloudflare.com:443", "serverNames": []any{"www.cloudflare.com"},
			"privateKey": "keep-me", "shortIds": []any{"ab12"},
			"settings": map[string]any{"publicKey": "pub", "serverName": "www.cloudflare.com"},
		},
	}
	r := RealitySelfStream(stream, "us.example.com", 24873)["realitySettings"].(map[string]any)
	if r["target"] != "127.0.0.1:24873" || r["dest"] != nil {
		t.Fatalf("target = %v dest = %v", r["target"], r["dest"])
	}
	if names := r["serverNames"].([]any); len(names) != 1 || names[0] != "us.example.com" {
		t.Fatalf("serverNames = %v, want the server's own domain", names)
	}
	if r["xver"] != 1 || r["privateKey"] != "keep-me" {
		t.Fatalf("xver = %v privateKey = %v; keys must survive and the visitor address must be passed on", r["xver"], r["privateKey"])
	}
	if r["settings"].(map[string]any)["serverName"] != "us.example.com" {
		t.Fatal("share links must carry the new SNI")
	}
}

func seedRealitySelf(t *testing.T) (*ProtectService, *model.Inbound) {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	t.Cleanup(func() { SetFrontStatus(FrontStatus{}) })
	cert, key := writeTestCert(t, t.TempDir(), []string{"us.example.com"}, time.Now().Add(60*24*time.Hour))
	settings := SettingService{}
	for k, v := range map[string]string{"webCertFile": cert, "webKeyFile": key, "webDomain": "us.example.com"} {
		if err := settings.setString(k, v); err != nil {
			t.Fatal(err)
		}
	}
	stubRecommendNet(t, map[string][]string{"us.example.com": {"203.0.113.5"}}, "203.0.113.5")
	ib := &model.Inbound{
		UserId: 1, Remark: "US A Reality", Port: 2053, Protocol: model.VLESS, Tag: "in-2053", Enable: true,
		Settings:       `{"clients":[],"decryption":"none"}`,
		StreamSettings: `{"network":"tcp","security":"reality","tcpSettings":{},"realitySettings":{"target":"www.cloudflare.com:443","serverNames":["www.cloudflare.com"],"privateKey":"priv","shortIds":["ab12"],"settings":{"publicKey":"pub"}}}`,
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	return &ProtectService{}, ib
}

func TestPrepareRealitySelfGuards(t *testing.T) {
	svc, ib := seedRealitySelf(t)
	if _, err := svc.PrepareRealitySelf(ib.Id); err == nil || !strings.Contains(err.Error(), "decoy") {
		t.Fatalf("without a deployed website visitors would see nothing, got %v", err)
	}
	if err := svc.SaveDecoy(decoy.ModeCompany, decoy.Site{Title: "Star Tech"}); err != nil {
		t.Fatal(err)
	}
	other := &model.Inbound{UserId: 1, Remark: "Old TLS", Port: 443, Protocol: model.VLESS, Tag: "in-443", Enable: true, Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp","security":"tls"}`}
	if err := database.GetDB().Create(other).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareRealitySelf(ib.Id); err == nil || !strings.Contains(err.Error(), "Old TLS") {
		t.Fatalf("a TCP inbound already on 443 must be named, got %v", err)
	}
	if err := database.GetDB().Model(other).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PrepareRealitySelf(ib.Id)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Domain != "us.example.com" || plan.InternalPort == RealityPublicPort || svc.FrontPort() != plan.InternalPort || !svc.FrontLoopback() {
		t.Fatalf("plan = %+v, front port %d loopback %v", plan, svc.FrontPort(), svc.FrontLoopback())
	}
	if enabled, _ := svc.FrontSettings(); !enabled {
		t.Fatal("the website must be switched on behind REALITY")
	}
}

func TestApplyRealitySelfWaitsForTheWebsite(t *testing.T) {
	svc, ib := seedRealitySelf(t)
	if err := svc.SaveDecoy(decoy.ModeCompany, decoy.Site{Title: "Star Tech"}); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PrepareRealitySelf(ib.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ApplyRealitySelf(ib.Id); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("moving REALITY before the website left 443 would leave Xray unable to bind, got %v", err)
	}
	SetFrontStatus(FrontStatus{Listening: true, Addr: "127.0.0.1:" + strconv.Itoa(plan.InternalPort)})
	if _, _, err := svc.ApplyRealitySelf(ib.Id); err != nil {
		t.Fatal(err)
	}
	var got model.Inbound
	if err := database.GetDB().First(&got, ib.Id).Error; err != nil {
		t.Fatal(err)
	}
	var stream map[string]any
	_ = json.Unmarshal([]byte(got.StreamSettings), &stream)
	r := stream["realitySettings"].(map[string]any)
	if got.Port != 443 || r["target"] != "127.0.0.1:"+strconv.Itoa(plan.InternalPort) || r["privateKey"] != "priv" {
		t.Fatalf("port %d reality %v", got.Port, r)
	}
}
