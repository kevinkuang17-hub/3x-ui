package job

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/op/go-logging"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	xuilogger "github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type fakeXray struct {
	running, crashed, heldBack bool
	restarts                   int
}

func (f *fakeXray) IsXrayRunning() bool        { return f.running }
func (f *fakeXray) DidXrayCrash() bool         { return f.crashed }
func (f *fakeXray) RestartXray(bool) error     { f.restarts++; return nil }
func (f *fakeXray) RunningConfigCurrent() bool { return f.running && !f.heldBack }

func setTemplate(t *testing.T, content string) {
	t.Helper()
	db := database.GetDB()
	db.Where("key = ?", "xrayTemplateConfig").Delete(&model.Setting{})
	if err := db.Create(&model.Setting{Key: "xrayTemplateConfig", Value: content}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestConfigHistoryMarksGoodAndRollsBack(t *testing.T) {
	xuilogger.InitLogger(logging.ERROR)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	settings := &service.SettingService{}
	good, err := settings.GetXrayConfigTemplate()
	if err != nil || good == "" {
		t.Fatalf("default template: %v", err)
	}
	setTemplate(t, good)

	clock := time.Unix(1_800_000_000, 0)
	fx := &fakeXray{running: true}
	j := NewConfigHistoryJob()
	j.xray = fx
	j.now = func() time.Time { return clock }

	j.Run()
	clock = clock.Add(configGoodAfter)
	j.Run()
	snaps, _ := (&service.ConfigHistoryService{}).List()
	if len(snaps) != 1 || !snaps[0].Good {
		t.Fatalf("running version must become good: %+v", snaps)
	}

	// A breaking edit: Xray goes down and stays down.
	setTemplate(t, `{"log":{},"inbounds":[],"outbounds":[{"protocol":"freedom","tag":"broken"}],"routing":{"rules":[]}}`)
	fx.running, fx.crashed = false, true
	for range rollbackAfterFailures {
		clock = clock.Add(20 * time.Second)
		j.Run()
	}
	current, _ := settings.GetXrayConfigTemplate()
	if current == good || fx.restarts != 0 {
		t.Fatal("conservative mode is off by default and must not roll back")
	}

	database.GetDB().Create(&model.Setting{Key: "xrayAutoRollback", Value: "true"})
	for range rollbackAfterFailures {
		clock = clock.Add(20 * time.Second)
		j.Run()
	}
	current, _ = settings.GetXrayConfigTemplate()
	if fx.restarts != 1 {
		t.Fatalf("expected one restart after rollback, got %d", fx.restarts)
	}
	snaps, _ = (&service.ConfigHistoryService{}).List()
	if len(snaps) < 3 || snaps[0].Note != "auto-rollback" {
		t.Fatalf("rollback must be recorded as a new version: %+v", snaps)
	}
	if current == `{"log":{},"inbounds":[],"outbounds":[{"protocol":"freedom","tag":"broken"}],"routing":{"rules":[]}}` {
		t.Fatal("template was not restored")
	}
}

func TestConfigHistoryIgnoresManualStop(t *testing.T) {
	xuilogger.InitLogger(logging.ERROR)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	database.GetDB().Create(&model.Setting{Key: "xrayAutoRollback", Value: "true"})
	fx := &fakeXray{running: false, crashed: false}
	j := NewConfigHistoryJob()
	j.xray = fx
	for range rollbackAfterFailures + 2 {
		j.Run()
	}
	if fx.restarts != 0 {
		t.Fatal("a manually stopped core must never trigger a rollback")
	}
}

func TestConfigHistoryDoesNotTrustRefusedSaves(t *testing.T) {
	xuilogger.InitLogger(logging.ERROR)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	clock := time.Unix(1_800_000_000, 0)
	fx := &fakeXray{running: true, heldBack: true}
	j := NewConfigHistoryJob()
	j.xray = fx
	j.now = func() time.Time { return clock }
	j.Run()
	clock = clock.Add(2 * configGoodAfter)
	j.Run()
	snaps, _ := (&service.ConfigHistoryService{}).List()
	if len(snaps) != 1 || snaps[0].Good {
		t.Fatalf("a version the core refused must not become good: %+v", snaps)
	}
}
