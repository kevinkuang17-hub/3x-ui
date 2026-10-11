package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestConfigHistoryCaptureDedupesAndTrims(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	s := &ConfigHistoryService{}
	now := time.Unix(1_800_000_000, 0)
	first, err := s.Capture(now, "")
	if err != nil || first == nil {
		t.Fatalf("Capture: %v", err)
	}
	again, _ := s.Capture(now, "")
	if again.Id != first.Id {
		t.Fatal("an unchanged template must not create a new version")
	}
	db := database.GetDB()
	for i := range configHistoryKeep + 5 {
		db.Where("key = ?", "xrayTemplateConfig").Delete(&model.Setting{})
		db.Create(&model.Setting{Key: "xrayTemplateConfig", Value: `{"v":` + string(rune('0'+i%10)) + `,"i":"` + time.Duration(i).String() + `"}`})
		if _, err := s.Capture(now, ""); err != nil {
			t.Fatal(err)
		}
	}
	snaps, _ := s.List()
	if len(snaps) != configHistoryKeep {
		t.Fatalf("history must keep the newest %d versions, has %d", configHistoryKeep, len(snaps))
	}
}

func TestConfigHistoryRestore(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	s := &ConfigHistoryService{}
	original, err := s.Capture(time.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkGood(original.Id); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	db.Where("key = ?", "xrayTemplateConfig").Delete(&model.Setting{})
	db.Create(&model.Setting{Key: "xrayTemplateConfig", Value: `{"outbounds":[]}`})
	if _, err := s.Capture(time.Now(), ""); err != nil {
		t.Fatal(err)
	}
	if good, _ := s.LastGood(""); good == nil || good.Id != original.Id {
		t.Fatalf("LastGood = %+v", good)
	}
	if _, err := s.Restore(original.Id, "manual"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, _ := (&SettingService{}).GetXrayConfigTemplate()
	if got == `{"outbounds":[]}` {
		t.Fatal("template not restored")
	}
	if _, err := s.Restore(9999, ""); err == nil {
		t.Fatal("unknown versions must be refused")
	}
}
