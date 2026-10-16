package job

import (
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// ChinaWatchJob turns the ISP probes' verdicts into alerts: one event when a line stops being
// reachable from China and one when it comes back, not one per probe round.
type ChinaWatchJob struct {
	mu      sync.Mutex
	probes  service.IspProbeService
	blocked map[int]bool
	now     func() time.Time
}

func NewChinaWatchJob() *ChinaWatchJob {
	return &ChinaWatchJob{blocked: map[int]bool{}}
}

func (j *ChinaWatchJob) Run() {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	if j.now != nil {
		now = j.now()
	}
	verdicts := service.ChinaVerdicts(now)
	if len(verdicts) == 0 && len(j.blocked) == 0 {
		return
	}
	targets, err := j.probes.Targets()
	if err != nil {
		return
	}
	byID := make(map[int]service.ProbeTarget, len(targets))
	for _, t := range targets {
		byID[t.InboundId] = t
	}
	var events []eventbus.Event
	for id, v := range verdicts {
		t, known := byID[id]
		if !known {
			continue
		}
		down := v.State != "ok"
		if down == j.blocked[id] {
			continue
		}
		kind := eventbus.EventChinaOk
		if down {
			kind = eventbus.EventChinaBlocked
			j.blocked[id] = true
		} else {
			delete(j.blocked, id)
		}
		events = append(events, eventbus.Event{
			Type: kind, Source: t.Name, Timestamp: now,
			Data: &eventbus.ChinaLineData{InboundId: id, Node: t.Node, Host: t.Host, Port: t.Port, State: v.State, Probes: v.Probes},
		})
	}
	// A line without fresh results (probes offline, line removed) is no longer known to be blocked.
	for id := range j.blocked {
		if _, ok := verdicts[id]; !ok {
			delete(j.blocked, id)
		}
	}
	if EventBus != nil {
		for _, e := range events {
			EventBus.Publish(e)
		}
	}
}
