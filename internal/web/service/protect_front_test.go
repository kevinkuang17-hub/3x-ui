package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
)

func TestSaveFrontGuards(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	t.Cleanup(func() { SetFrontStatus(FrontStatus{}) })
	svc := &ProtectService{}
	if err := svc.SaveFront(true, false, false, 0); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("without a panel certificate the 443 site must be refused, got %v", err)
	}
	for k, v := range map[string]string{"webCertFile": "/etc/x/cert.pem", "webKeyFile": "/etc/x/key.pem"} {
		if err := database.GetDB().Create(&model.Setting{Key: k, Value: v}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ib := &model.Inbound{UserId: 1, Enable: true, Port: DefaultFrontPort, Protocol: model.VLESS, Tag: "in-443", Remark: "US A", Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveFront(true, false, false, 0); err == nil || !strings.Contains(err.Error(), "US A") {
		t.Fatalf("a local inbound on 443 must be named as the conflict, got %v", err)
	}
	udp := &model.Inbound{UserId: 1, Enable: true, Port: DefaultFrontPort, Protocol: model.Hysteria, Tag: "hy-443", Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(udp).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(ib).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveFront(true, true, true, 0); err == nil || !strings.Contains(err.Error(), "restart the panel first") {
		t.Fatalf("closing the panel port before the 443 site runs would lock the admin out, got %v", err)
	}
	if err := svc.SaveFront(true, false, false, 0); err != nil {
		t.Fatalf("a UDP-only inbound on 443 does not block the TCP site: %v", err)
	}
	SetFrontStatus(FrontStatus{Listening: true})
	if err := svc.SaveFront(true, true, false, 0); err == nil || !strings.Contains(err.Error(), "website address") {
		t.Fatalf("the panel port may only be closed from a page opened over 443, got %v", err)
	}
	if err := svc.SaveFront(true, true, true, 0); err != nil {
		t.Fatal(err)
	}
	if enabled, frontOnly := svc.FrontSettings(); !enabled || !frontOnly {
		t.Fatalf("FrontSettings = %v, %v", enabled, frontOnly)
	}
	if err := (&SettingService{}).ClearPanelAccess(); err != nil {
		t.Fatal(err)
	}
	if _, frontOnly := svc.FrontSettings(); frontOnly {
		t.Fatal("the CLI recovery must reopen the panel port")
	}
}

// The website may sit on another port than 443 (its address then carries that port), which also
// gets it out of the way of an inbound that already holds 443.
func TestSaveFrontOnCustomPort(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	t.Cleanup(func() { SetFrontStatus(FrontStatus{}) })
	for k, v := range map[string]string{"webCertFile": "/etc/x/cert.pem", "webKeyFile": "/etc/x/key.pem"} {
		if err := database.GetDB().Create(&model.Setting{Key: k, Value: v}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ib := &model.Inbound{UserId: 1, Enable: true, Port: 443, Protocol: model.VLESS, Tag: "in-443", Remark: "US A", Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	svc := &ProtectService{}
	if err := svc.SaveFront(true, false, false, 8443); err != nil {
		t.Fatalf("8443 is free, so the website may use it: %v", err)
	}
	if got := svc.FrontPort(); got != 8443 {
		t.Fatalf("FrontPort = %d, want 8443", got)
	}
	if v := svc.Front(); v.Port != 8443 || v.Conflict != "" {
		t.Fatalf("Front = %+v, want port 8443 without conflict", v)
	}
	if err := svc.SaveFront(true, false, false, 443); err == nil || !strings.Contains(err.Error(), "US A") {
		t.Fatalf("moving onto the inbound's port must be refused, got %v", err)
	}
	SetFrontStatus(FrontStatus{Listening: true})
	if err := svc.SaveFront(true, true, true, 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveFront(true, true, true, 9443); err == nil || !strings.Contains(err.Error(), "panel port again") {
		t.Fatalf("moving the website while it is the only way in would lock the admin out, got %v", err)
	}
	if err := svc.SaveFront(true, false, false, 70000); err == nil {
		t.Fatal("a port above 65535 must be refused")
	}
}

// Hysteria2's masquerade embeds the deployed decoy, so it must be the page visitors see on the site.
func TestDecoyPageFollowsDeployedSite(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	svc := &ProtectService{}
	if _, err := svc.DecoyPage(); err == nil || !strings.Contains(err.Error(), "decoy website") {
		t.Fatalf("with no decoy deployed there is nothing to embed, got %v", err)
	}
	if err := svc.SaveDecoy(decoy.ModeCompany, decoy.Site{Title: "Star Tech Trading"}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.DecoyPage()
	if err != nil || !strings.Contains(page, "Star Tech Trading") || !strings.Contains(strings.ToLower(page), "<html") {
		t.Fatalf("page = %.80q, %v", page, err)
	}
}
