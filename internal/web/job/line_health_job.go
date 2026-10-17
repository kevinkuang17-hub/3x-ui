package job

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/linehealth"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const (
	lineHealthFailThreshold = 3
	lineHealthDialTimeout   = 3 * time.Second
	lineHealthConcurrency   = 32
	// A round where this many lines fail and they are the majority points at the master's own uplink.
	lineHealthMassFailMin = 3
)

type lineVerdict int

const (
	lineUnknown lineVerdict = iota
	lineOK
	lineFail
)

// LineHealthJob probes node-hosted inbounds and hides the unreachable ones from subscriptions
// (see internal/linehealth) so users are not handed dead lines; recovery restores them.
type LineHealthJob struct {
	settingService service.SettingService
	nodeService    service.NodeService
	running        sync.Mutex
	fails          map[int]int
	dial           func(ctx context.Context, address string) error
	now            func() time.Time
}

func NewLineHealthJob() *LineHealthJob {
	return &LineHealthJob{fails: map[int]int{}, dial: dialTCP}
}

func dialTCP(ctx context.Context, address string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (j *LineHealthJob) Run() {
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()

	if enabled, err := j.settingService.GetSubLineHealth(); err != nil || !enabled {
		j.fails = map[int]int{}
		linehealth.Reset()
		return
	}
	nodes, err := j.nodeService.GetAll()
	if err != nil {
		logger.Warning("line health: load nodes failed:", err)
		return
	}
	nodesByID := make(map[int]*model.Node, len(nodes))
	for _, n := range nodes {
		nodesByID[n.Id] = n
	}
	var inbounds []*model.Inbound
	if err := database.GetDB().Where("enable = ? AND node_id IS NOT NULL", true).Find(&inbounds).Error; err != nil {
		logger.Warning("line health: load inbounds failed:", err)
		return
	}
	// Inbounds fronted by managed hosts (CDN, relays) are reached through those, not the node itself.
	var hosted []int
	if err := database.GetDB().Model(&model.Host{}).Where("is_disabled = ?", false).Distinct("inbound_id").Pluck("inbound_id", &hosted).Error; err != nil {
		logger.Warning("line health: load hosts failed:", err)
		return
	}
	hostedSet := make(map[int]bool, len(hosted))
	for _, id := range hosted {
		hostedSet[id] = true
	}
	probe := inbounds[:0:0]
	for _, ib := range inbounds {
		if !hostedSet[ib.Id] {
			probe = append(probe, ib)
		}
	}
	verdicts := j.probeAll(probe, nodesByID)
	j.apply(inbounds, verdicts, service.BlockedFromProbes(j.clock()), downNodes(nodes))
}

// downNodes are enabled nodes the heartbeat reports offline while another node still answers:
// the server is really down, not the master's own network.
func downNodes(nodes []*model.Node) map[int]bool {
	down, online := map[int]bool{}, 0
	for _, n := range nodes {
		switch {
		case !n.Enable:
		case n.Status == "online":
			online++
		case n.Status == "offline":
			down[n.Id] = true
		}
	}
	if online == 0 {
		return nil
	}
	return down
}

func (j *LineHealthJob) probeAll(inbounds []*model.Inbound, nodesByID map[int]*model.Node) map[int]lineVerdict {
	verdicts := make(map[int]lineVerdict, len(inbounds))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, lineHealthConcurrency)
	for _, ib := range inbounds {
		node := nodesByID[*ib.NodeID]
		if node == nil || !node.Enable {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(ib *model.Inbound, node *model.Node) {
			defer wg.Done()
			defer func() { <-sem }()
			v := j.probeOne(ib, node)
			mu.Lock()
			verdicts[ib.Id] = v
			mu.Unlock()
		}(ib, node)
	}
	wg.Wait()
	return verdicts
}

func (j *LineHealthJob) probeOne(ib *model.Inbound, node *model.Node) lineVerdict {
	// A node that stopped answering heartbeats serves nothing, whatever the protocol.
	if node.Status == "offline" {
		return lineFail
	}
	if linehealth.IsUDP(ib) {
		// No handshake to test over UDP: trust the node's own report, and only while it answers.
		if node.Status != "online" || node.XrayState == "" {
			return lineUnknown
		}
		if node.XrayState == "running" {
			return lineOK
		}
		return lineFail
	}
	host := linehealth.ProbeHost(ib, node)
	if host == "" || ib.Port <= 0 {
		return lineUnknown
	}
	ctx, cancel := context.WithTimeout(context.Background(), lineHealthDialTimeout)
	defer cancel()
	if err := j.dial(ctx, net.JoinHostPort(host, strconv.Itoa(ib.Port))); err != nil {
		return lineFail
	}
	return lineOK
}

func (j *LineHealthJob) clock() time.Time {
	if j.now != nil {
		return j.now()
	}
	return time.Now()
}

// apply turns verdicts into hide/restore decisions; blocked (ISP probes) counts as failure. Lines on
// a node confirmed down skip the mass-failure guard: one big node going away is not a master blip.
func (j *LineHealthJob) apply(inbounds []*model.Inbound, verdicts map[int]lineVerdict, blocked, down map[int]bool) {
	onDownNode := make(map[int]bool)
	for _, ib := range inbounds {
		if ib.NodeID != nil && down[*ib.NodeID] {
			onDownNode[ib.Id] = true
		}
	}
	probed, failed := 0, 0
	for id, v := range verdicts {
		if onDownNode[id] {
			continue
		}
		if v != lineUnknown {
			probed++
		}
		if v == lineFail {
			failed++
		}
	}
	if failed > 0 && (failed == probed || (failed >= lineHealthMassFailMin && failed*2 > probed)) {
		logger.Warningf("line health: %d of %d lines failed at once; assuming a master-side network problem, ignoring this round's master checks", failed, probed)
		for id, v := range verdicts {
			if v == lineFail && !onDownNode[id] {
				verdicts[id] = lineUnknown
			}
		}
	}
	for id := range blocked {
		if _, probedHere := verdicts[id]; probedHere {
			verdicts[id] = lineFail
		}
	}

	byID := make(map[int]*model.Inbound, len(inbounds))
	for _, ib := range inbounds {
		byID[ib.Id] = ib
	}
	var events []eventbus.Event
	for id, v := range verdicts {
		switch v {
		case lineOK:
			j.fails[id] = 0
			if linehealth.Set(id, false) {
				events = append(events, lineEvent(eventbus.EventLineUp, byID[id]))
			}
		case lineFail:
			j.fails[id]++
			if j.fails[id] >= lineHealthFailThreshold && linehealth.Set(id, true) {
				events = append(events, lineEvent(eventbus.EventLineDown, byID[id]))
			}
		}
	}
	// Lines that left the probe set (deleted, disabled, node gone) must not stay hidden.
	for _, id := range linehealth.Down() {
		if _, ok := verdicts[id]; !ok {
			linehealth.Set(id, false)
			delete(j.fails, id)
		}
	}
	for id := range j.fails {
		if _, ok := verdicts[id]; !ok {
			delete(j.fails, id)
		}
	}
	if EventBus != nil {
		for _, e := range events {
			EventBus.Publish(e)
		}
	}
}

func lineEvent(t eventbus.EventType, ib *model.Inbound) eventbus.Event {
	source := ib.Remark
	if source == "" {
		source = ib.Tag
	}
	nodeID := 0
	if ib.NodeID != nil {
		nodeID = *ib.NodeID
	}
	return eventbus.Event{
		Type:      t,
		Source:    source,
		Timestamp: time.Now(),
		Data:      &eventbus.LineHealthData{InboundId: ib.Id, NodeId: nodeID, Tag: ib.Tag, Port: ib.Port},
	}
}
