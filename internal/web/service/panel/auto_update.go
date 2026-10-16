package panel

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/version"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// AutoUpdateZone is the clock the update hour is set in; the panel's own zone is often UTC.
const AutoUpdateZone = "Asia/Shanghai"

// AutoUpdateConfig is the unattended self-update setting and what its last run did.
type AutoUpdateConfig struct {
	Enable bool `json:"enable" example:"true"`
	// Hour is when it runs each day, 0-23 in Beijing time.
	Hour  int  `json:"hour" example:"4"`
	Nodes bool `json:"nodes" example:"true"`
	// LastRun is the Beijing date of the last check, LastResult what it did then.
	LastRun    string `json:"lastRun" example:"2026-10-16"`
	LastResult string `json:"lastResult" example:"updated:v3.10.2-k1:v3.10.2-k2"`
}

type autoUpdateState struct {
	LastRun    string `json:"lastRun"`
	LastResult string `json:"lastResult"`
	// PendingFrom is the version an update started from, checked on the next start.
	PendingFrom string `json:"pendingFrom"`
	PendingTo   string `json:"pendingTo"`
	Nodes       int    `json:"nodes"`
}

// Seams for tests.
var (
	autoUpdateInfo  = func(s *PanelService) (*PanelUpdateInfo, error) { return s.GetUpdateInfo() }
	autoUpdateStart = func(s *PanelService) error { _, err := s.StartUpdate(); return err }
	autoUpdateNodes = func(ids []int, dev bool) { _, _ = (&service.NodeService{}).UpdatePanels(ids, dev) }
)

func loadAutoUpdateState() autoUpdateState {
	var st autoUpdateState
	raw, _ := (&service.SettingService{}).GetAutoUpdateState()
	_ = json.Unmarshal([]byte(raw), &st)
	return st
}

func saveAutoUpdateState(st autoUpdateState) {
	raw, _ := json.Marshal(st)
	if err := (&service.SettingService{}).SetAutoUpdateState(string(raw)); err != nil {
		logger.Warning("auto update: save state:", err)
	}
}

func (s *PanelService) GetAutoUpdate() AutoUpdateConfig {
	settings := service.SettingService{}
	enable, _ := settings.GetAutoUpdateEnable()
	hour, _ := settings.GetAutoUpdateHour()
	nodes, _ := settings.GetAutoUpdateNodes()
	st := loadAutoUpdateState()
	return AutoUpdateConfig{Enable: enable, Hour: hour, Nodes: nodes, LastRun: st.LastRun, LastResult: st.LastResult}
}

// SetAutoUpdate stores the setting; turning it on also turns on the Telegram notice for it, since
// an update nobody started should not go unnoticed.
func (s *PanelService) SetAutoUpdate(enable bool, hour int, nodes bool) error {
	if hour < 0 || hour > 23 {
		return fmt.Errorf("hour must be 0-23")
	}
	settings := service.SettingService{}
	if err := settings.SetAutoUpdate(enable, hour, nodes); err != nil {
		return err
	}
	if !enable {
		return nil
	}
	events, _ := settings.GetTgEnabledEvents()
	for _, e := range strings.Split(events, ",") {
		if strings.TrimSpace(e) == string(eventbus.EventPanelUpdate) {
			return nil
		}
	}
	if strings.TrimSpace(events) != "" {
		events += ","
	}
	return settings.SetTgEnabledEvents(events + string(eventbus.EventPanelUpdate))
}

func beijing() *time.Location {
	if loc, err := time.LoadLocation(AutoUpdateZone); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

// RunAutoUpdate is the hourly check: at the set hour, once a day, it tells outdated nodes to update
// and then updates this panel, which restarts it. It returns what it did.
func (s *PanelService) RunAutoUpdate(now time.Time) string {
	settings := service.SettingService{}
	// An update that failed before it could restart the panel leaves it running the old version.
	if st := loadAutoUpdateState(); st.PendingFrom != "" && s.GetUpdateStatus().State == updateStateFailed {
		s.ReportAutoUpdateOutcome()
	}
	if enable, _ := settings.GetAutoUpdateEnable(); !enable {
		return ""
	}
	hour, _ := settings.GetAutoUpdateHour()
	local := now.In(beijing())
	today := local.Format("2006-01-02")
	st := loadAutoUpdateState()
	if local.Hour() != hour || st.LastRun == today {
		return ""
	}
	st.LastRun = today
	info, err := autoUpdateInfo(s)
	if err != nil {
		st.LastResult = "error:" + err.Error()
		saveAutoUpdateState(st)
		return st.LastResult
	}
	nodes := 0
	if withNodes, _ := settings.GetAutoUpdateNodes(); withNodes {
		nodes = s.updateOutdatedNodes(info)
	}
	if !info.UpdateAvailable {
		st.LastResult = fmt.Sprintf("current:%s:%d", info.CurrentVersion, nodes)
		saveAutoUpdateState(st)
		return st.LastResult
	}
	st.PendingFrom, st.PendingTo, st.Nodes = config.GetBaseVersion(), info.LatestVersion, nodes
	st.LastResult = "started:" + info.LatestVersion
	saveAutoUpdateState(st)
	service.PublishEvent(eventbus.Event{
		Type: eventbus.EventPanelUpdate, Source: "panel", Timestamp: now,
		Data: &eventbus.PanelUpdateData{From: st.PendingFrom, To: info.LatestVersion, State: "started", Nodes: nodes},
	})
	if err := autoUpdateStart(s); err != nil {
		st.PendingFrom, st.PendingTo = "", ""
		st.LastResult = "error:" + err.Error()
		saveAutoUpdateState(st)
	}
	return st.LastResult
}

// updateOutdatedNodes tells every online node behind the latest release to update itself.
func (s *PanelService) updateOutdatedNodes(info *PanelUpdateInfo) int {
	nodes, err := (&service.NodeService{}).GetAll()
	if err != nil {
		return 0
	}
	dev := info.Channel == "dev"
	var ids []int
	for _, n := range nodes {
		if !n.Enable || n.Status != "online" {
			continue
		}
		if cmp, ok := version.Compare(n.PanelVersion, info.LatestVersion); dev || (ok && cmp < 0) {
			ids = append(ids, n.Id)
		}
	}
	if len(ids) > 0 {
		autoUpdateNodes(ids, dev)
	}
	return len(ids)
}

// ReportAutoUpdateOutcome runs at start: after an unattended update it says whether the new
// version is running or update.sh put the old one back.
func (s *PanelService) ReportAutoUpdateOutcome() {
	st := loadAutoUpdateState()
	if st.PendingFrom == "" {
		return
	}
	current := config.GetBaseVersion()
	state := "success"
	if cmp, ok := version.Compare(current, st.PendingFrom); ok && cmp <= 0 {
		state = "failed"
	}
	if state == "success" {
		st.LastResult = fmt.Sprintf("updated:%s:%s", st.PendingFrom, current)
	} else {
		st.LastResult = fmt.Sprintf("failed:%s:%s", st.PendingFrom, st.PendingTo)
	}
	data := &eventbus.PanelUpdateData{From: st.PendingFrom, To: st.PendingTo, State: state, Nodes: st.Nodes}
	if state == "success" {
		data.To = current
	}
	st.PendingFrom, st.PendingTo = "", ""
	saveAutoUpdateState(st)
	service.PublishEvent(eventbus.Event{Type: eventbus.EventPanelUpdate, Source: "panel", Timestamp: time.Now(), Data: data})
}
