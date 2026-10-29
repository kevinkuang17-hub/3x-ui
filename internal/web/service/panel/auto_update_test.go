package panel

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type autoUpdateRecorder struct {
	mu      sync.Mutex
	started int
	nodes   []int
	events  []eventbus.Event
}

func stubAutoUpdate(t *testing.T, info PanelUpdateInfo) *autoUpdateRecorder {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	rec := &autoUpdateRecorder{}
	oldInfo, oldStart, oldNodes := autoUpdateInfo, autoUpdateStart, autoUpdateNodes
	t.Cleanup(func() { autoUpdateInfo, autoUpdateStart, autoUpdateNodes = oldInfo, oldStart, oldNodes })
	autoUpdateInfo = func(*PanelService) (*PanelUpdateInfo, error) { return &info, nil }
	autoUpdateStart = func(*PanelService) error {
		rec.mu.Lock()
		rec.started++
		rec.mu.Unlock()
		return nil
	}
	autoUpdateNodes = func(ids []int, _ bool) { rec.nodes = append(rec.nodes, ids...) }
	bus := eventbus.New(eventbus.DefaultBufferSize)
	bus.Subscribe("test", func(e eventbus.Event) {
		rec.mu.Lock()
		rec.events = append(rec.events, e)
		rec.mu.Unlock()
	})
	service.SetEventBus(bus)
	t.Cleanup(func() { service.SetEventBus(nil); bus.Stop() })
	return rec
}

func beijingAt(t *testing.T, day, hour int) time.Time {
	t.Helper()
	return time.Date(2026, 10, day, hour, 7, 0, 0, beijing())
}

// The hour is Beijing time whatever the server's zone, and a day gets one attempt: a failing
// update must not retry every hour through the night.
func TestAutoUpdateRunsOnceAtTheHour(t *testing.T) {
	rec := stubAutoUpdate(t, PanelUpdateInfo{CurrentVersion: "3.10.2-k1", LatestVersion: "v3.10.2-k2", UpdateAvailable: true})
	s := &PanelService{}
	if got := s.RunAutoUpdate(beijingAt(t, 16, 4)); got != "" {
		t.Fatalf("off by default, yet ran: %q", got)
	}
	if err := s.SetAutoUpdate(true, 4, true); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	db.Create(&model.Node{Name: "HK A", Address: "a", Port: 1, ApiToken: "t", Enable: true, Status: "online", PanelVersion: "v3.10.2-k1"})
	db.Create(&model.Node{Name: "HK B", Address: "b", Port: 1, ApiToken: "t", Enable: true, Status: "online", PanelVersion: "v3.10.2-k2"})
	db.Create(&model.Node{Name: "HK C", Address: "c", Port: 1, ApiToken: "t", Enable: true, Status: "offline", PanelVersion: "v3.10.2-k1"})

	if got := s.RunAutoUpdate(beijingAt(t, 16, 3)); got != "" {
		t.Fatalf("ran at 03:07 Beijing: %q", got)
	}
	// 04:07 Beijing is 20:07 UTC the day before: the zone must not shift the hour.
	if got := s.RunAutoUpdate(beijingAt(t, 16, 4).UTC()); got != "started:v3.10.2-k2" {
		t.Fatalf("at 04:07 Beijing got %q", got)
	}
	if got := s.RunAutoUpdate(beijingAt(t, 16, 4).Add(30 * time.Minute)); got != "" {
		t.Fatalf("second run the same day: %q", got)
	}
	if rec.started != 1 || len(rec.nodes) != 1 {
		t.Fatalf("panel updates %d, nodes %v; want one update and only the outdated online node", rec.started, rec.nodes)
	}
	cfg := s.GetAutoUpdate()
	if !cfg.Enable || cfg.Hour != 4 || cfg.LastRun != "2026-10-16" || cfg.LastResult != "started:v3.10.2-k2" {
		t.Fatalf("config = %+v", cfg)
	}
	if events, _ := (&service.SettingService{}).GetTgEnabledEvents(); events != "login.attempt,cpu.high,panel.update" {
		t.Fatalf("turning auto update on must enable its Telegram notice, events = %q", events)
	}
}

// After the restart the panel says whether the new version runs or update.sh put the old one back.
func TestAutoUpdateOutcomeAfterRestart(t *testing.T) {
	cases := []struct {
		name, from, state string
	}{
		{"new version running", "3.10.1-k3", "success"},
		{"rolled back", config.GetBaseVersion(), "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := stubAutoUpdate(t, PanelUpdateInfo{})
			saveAutoUpdateState(autoUpdateState{LastRun: "2026-10-16", PendingFrom: tc.from, PendingTo: "v3.10.3-k1"})
			(&PanelService{}).ReportAutoUpdateOutcome()
			time.Sleep(200 * time.Millisecond)
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.events) != 1 {
				t.Fatalf("events = %+v", rec.events)
			}
			if d := rec.events[0].Data.(*eventbus.PanelUpdateData); d.State != tc.state || d.From != tc.from {
				t.Fatalf("event data = %+v, want state %s", d, tc.state)
			}
			if st := loadAutoUpdateState(); st.PendingFrom != "" {
				t.Fatalf("the outcome must be reported once, state = %+v", st)
			}
		})
	}
}
