package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

const appTemplate = `{
  "outbounds": [{"protocol": "freedom", "tag": "direct", "settings": {"domainStrategy": "AsIs"}}, {"protocol": "blackhole", "tag": "blocked"}],
  "routing": {"rules": [{"type": "field", "domain": ["geosite:netflix"], "outboundTag": "blocked"}]}
}`

func TestAddAndStripApps(t *testing.T) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(appTemplate), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := addApps(cfg, []string{"netflix", "youtube", "netflix"}); err != nil {
		t.Fatal(err)
	}
	outs := cfg["outbounds"].([]any)
	if len(outs) != 4 {
		t.Fatalf("want 2 clones, outbounds=%v", outs)
	}
	clone := outs[2].(map[string]any)
	if clone["tag"] != "app-netflix" || clone["protocol"] != "freedom" || clone["settings"].(map[string]any)["domainStrategy"] != "AsIs" {
		t.Fatalf("clone must copy the default outbound: %v", clone)
	}
	if outs[0].(map[string]any)["tag"] != "direct" {
		t.Fatal("the original default outbound must stay first")
	}
	rules := cfg["routing"].(map[string]any)["rules"].([]any)
	if len(rules) != 3 || rules[0].(map[string]any)["outboundTag"] != "blocked" {
		t.Fatalf("app rules must come after existing rules: %v", rules)
	}
	sys := cfg["policy"].(map[string]any)["system"].(map[string]any)
	if sys["statsOutboundUplink"] != true || sys["statsOutboundDownlink"] != true {
		t.Fatal("outbound stats must be switched on")
	}
	if err := addApps(cfg, []string{"nope"}); err == nil {
		t.Fatal("unknown apps must be refused")
	}

	stripApps(cfg)
	if len(cfg["outbounds"].([]any)) != 2 || len(cfg["routing"].(map[string]any)["rules"].([]any)) != 1 {
		t.Fatalf("strip must remove only app entries: %v", cfg)
	}
}

func TestAppTrafficReadsOutboundStats(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	db.Create(&model.OutboundTraffics{Tag: "app-youtube", Up: 10, Down: 900})
	db.Create(&model.OutboundTraffics{Tag: "app-netflix", Up: 5, Down: 5000})
	db.Create(&model.OutboundTraffics{Tag: "direct", Up: 1, Down: 1})
	got, err := (&AppTrafficService{}).Traffic()
	if err != nil || len(got) != 2 || got[0].App != "netflix" || got[1].Down != 900 {
		t.Fatalf("traffic = %+v err=%v", got, err)
	}
	db.Where("key = ?", "xrayTemplateConfig").Delete(&model.Setting{})
	var cfg map[string]any
	_ = json.Unmarshal([]byte(appTemplate), &cfg)
	_ = addApps(cfg, []string{"telegram"})
	b, _ := json.Marshal(cfg)
	db.Create(&model.Setting{Key: "xrayTemplateConfig", Value: string(b)})
	apps, err := (&AppTrafficService{}).EnabledApps()
	if err != nil || len(apps) != 1 || apps[0] != "telegram" {
		t.Fatalf("enabled = %v err=%v", apps, err)
	}
}
