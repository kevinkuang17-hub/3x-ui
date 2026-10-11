package job

import (
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// rollbackAfterFailures is how many consecutive checks Xray must be down (and not manually stopped,
// the watchdog having retried) before conservative mode restores the last good template.
const rollbackAfterFailures = 3

// configGoodAfter is how long Xray must run with a version before it counts as good.
const configGoodAfter = 90 * time.Second

// ConfigHistoryJob snapshots the Xray template, marks versions Xray ran with as good, and in
// conservative mode (xrayAutoRollback) rolls a breaking edit back to the last good version.
type ConfigHistoryJob struct {
	history        service.ConfigHistoryService
	settingService service.SettingService
	xray           xrayHealth
	running        sync.Mutex
	runningSince   time.Time
	runningHash    string
	downChecks     int
	now            func() time.Time
}

// xrayHealth is the part of XrayService the job needs; tests swap it.
type xrayHealth interface {
	IsXrayRunning() bool
	DidXrayCrash() bool
	RestartXray(isForce bool) error
}

func NewConfigHistoryJob() *ConfigHistoryJob {
	return &ConfigHistoryJob{xray: &service.XrayService{}, now: time.Now}
}

func (j *ConfigHistoryJob) Run() {
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()
	now := j.now()
	snap, err := j.history.Capture(now, "")
	if err != nil {
		logger.Warning("config history: capture failed:", err)
		return
	}
	if j.xray.IsXrayRunning() {
		j.downChecks = 0
		if j.runningHash != snap.Hash {
			j.runningHash, j.runningSince = snap.Hash, now
		}
		if !snap.Good && now.Sub(j.runningSince) >= configGoodAfter {
			_ = j.history.MarkGood(snap.Id)
		}
		return
	}
	j.runningHash = ""
	if !j.xray.DidXrayCrash() {
		j.downChecks = 0
		return
	}
	j.downChecks++
	if j.downChecks < rollbackAfterFailures || snap.Good {
		return
	}
	if on, _ := j.settingService.GetXrayAutoRollback(); !on {
		return
	}
	good, err := j.history.LastGood(snap.Hash)
	if err != nil || good == nil {
		return
	}
	j.downChecks = 0
	logger.Warningf("conservative mode: Xray keeps failing with the latest config; restoring version %d", good.Id)
	if _, err := j.history.Restore(good.Id, "auto-rollback"); err != nil {
		logger.Error("conservative mode: rollback failed:", err)
		return
	}
	if err := j.xray.RestartXray(false); err != nil {
		logger.Error("conservative mode: restart after rollback failed:", err)
	}
	if EventBus != nil {
		EventBus.Publish(eventbus.Event{Type: eventbus.EventXrayRollback, Source: "xray", Timestamp: now})
	}
}
