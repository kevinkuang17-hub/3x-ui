package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestSaveFrontGuards(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	t.Cleanup(func() { SetFrontStatus(FrontStatus{}) })
	svc := &ProtectService{}
	if err := svc.SaveFront(true, false, false); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("without a panel certificate the 443 site must be refused, got %v", err)
	}
	for k, v := range map[string]string{"webCertFile": "/etc/x/cert.pem", "webKeyFile": "/etc/x/key.pem"} {
		if err := database.GetDB().Create(&model.Setting{Key: k, Value: v}).Error; err != nil {
			t.Fatal(err)
		}
	}
	ib := &model.Inbound{UserId: 1, Enable: true, Port: FrontPort, Protocol: model.VLESS, Tag: "in-443", Remark: "US A", Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveFront(true, false, false); err == nil || !strings.Contains(err.Error(), "US A") {
		t.Fatalf("a local inbound on 443 must be named as the conflict, got %v", err)
	}
	udp := &model.Inbound{UserId: 1, Enable: true, Port: FrontPort, Protocol: model.Hysteria, Tag: "hy-443", Settings: `{"clients":[]}`}
	if err := database.GetDB().Create(udp).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(ib).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveFront(true, true, true); err == nil || !strings.Contains(err.Error(), "restart the panel first") {
		t.Fatalf("closing the panel port before the 443 site runs would lock the admin out, got %v", err)
	}
	if err := svc.SaveFront(true, false, false); err != nil {
		t.Fatalf("a UDP-only inbound on 443 does not block the TCP site: %v", err)
	}
	SetFrontStatus(FrontStatus{Listening: true})
	if err := svc.SaveFront(true, true, false); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Fatalf("the panel port may only be closed from a page opened over 443, got %v", err)
	}
	if err := svc.SaveFront(true, true, true); err != nil {
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
