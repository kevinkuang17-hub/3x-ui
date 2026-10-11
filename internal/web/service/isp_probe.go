package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/linehealth"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const (
	// ProbeFresh is how long a probe result counts toward line health.
	ProbeFresh = 15 * time.Minute
	maxReport  = 2000
	// probeSanityMin is how many lines a probe must report before its failure rate is judged.
	probeSanityMin = 3
)

type IspProbeService struct{}

type ProbeTarget struct {
	InboundId int    `json:"inboundId"`
	Name      string `json:"name"`
	Node      string `json:"node"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
}

func hashProbeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Add registers a probe and returns its token; only the hash is stored.
func (s *IspProbeService) Add(name, isp string) (*model.IspProbe, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", common.NewError("probe name is required")
	}
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	token := hex.EncodeToString(b)
	p := &model.IspProbe{Name: name, Isp: strings.TrimSpace(isp), TokenHash: hashProbeToken(token)}
	if err := database.GetDB().Create(p).Error; err != nil {
		return nil, "", err
	}
	return p, token, nil
}

func (s *IspProbeService) Delete(id int) error {
	db := database.GetDB()
	db.Where("probe_id = ?", id).Delete(&model.IspProbeResult{})
	return db.Delete(&model.IspProbe{}, id).Error
}

func (s *IspProbeService) byToken(token string) (*model.IspProbe, error) {
	var p model.IspProbe
	if token == "" || database.GetDB().Where("token_hash = ?", hashProbeToken(token)).First(&p).Error != nil {
		return nil, common.NewError("unknown probe")
	}
	return &p, nil
}

// Targets lists the TCP lines a probe should test: node-hosted inbounds reachable at the
// address subscriptions advertise.
func (s *IspProbeService) Targets() ([]ProbeTarget, error) {
	db := database.GetDB()
	var nodes []model.Node
	if err := db.Where("enable = ?", true).Find(&nodes).Error; err != nil {
		return nil, err
	}
	byID := make(map[int]*model.Node, len(nodes))
	for i := range nodes {
		byID[nodes[i].Id] = &nodes[i]
	}
	var inbounds []*model.Inbound
	if err := db.Where("enable = ? AND node_id IS NOT NULL", true).Order("id asc").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	// Lines fronted by managed hosts (CDN, relays) are not reached at the node's own address.
	var hosted []int
	if err := db.Model(&model.Host{}).Where("is_disabled = ?", false).Distinct("inbound_id").Pluck("inbound_id", &hosted).Error; err != nil {
		return nil, err
	}
	skip := make(map[int]bool, len(hosted))
	for _, id := range hosted {
		skip[id] = true
	}
	out := make([]ProbeTarget, 0, len(inbounds))
	for _, ib := range inbounds {
		node := byID[*ib.NodeID]
		if node == nil || skip[ib.Id] || linehealth.IsUDP(ib) || ib.Port <= 0 {
			continue
		}
		host := linehealth.ProbeHost(ib, node)
		if host == "" {
			continue
		}
		name := ib.Remark
		if name == "" {
			name = ib.Tag
		}
		out = append(out, ProbeTarget{InboundId: ib.Id, Name: name, Node: node.Name, Host: host, Port: ib.Port})
	}
	return out, nil
}

// TargetsText is the agent's view: one "inboundId host port" line per target, easy to parse in sh.
func (s *IspProbeService) TargetsText(token, remoteIP string) (string, error) {
	p, err := s.byToken(token)
	if err != nil {
		return "", err
	}
	database.GetDB().Model(&model.IspProbe{}).Where("id = ?", p.Id).
		Updates(map[string]any{"last_seen": time.Now().Unix(), "last_ip": remoteIP})
	targets, err := s.Targets()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, t := range targets {
		fmt.Fprintf(&b, "%d %s %d\n", t.InboundId, t.Host, t.Port)
	}
	return b.String(), nil
}

type ProbeReportItem struct {
	InboundId int  `json:"inboundId"`
	Ok        bool `json:"ok"`
	LatencyMs int  `json:"latencyMs"`
}

// Report stores the agent's latest results, ignoring inbounds it was not asked to test.
func (s *IspProbeService) Report(token, remoteIP string, items []ProbeReportItem) (int, error) {
	p, err := s.byToken(token)
	if err != nil {
		return 0, err
	}
	if len(items) > maxReport {
		items = items[:maxReport]
	}
	targets, err := s.Targets()
	if err != nil {
		return 0, err
	}
	valid := make(map[int]bool, len(targets))
	for _, t := range targets {
		valid[t.InboundId] = true
	}
	now := time.Now().Unix()
	rows := make([]model.IspProbeResult, 0, len(items))
	for _, it := range items {
		if !valid[it.InboundId] {
			continue
		}
		lat := it.LatencyMs
		if !it.Ok || lat < 0 {
			lat = 0
		}
		rows = append(rows, model.IspProbeResult{ProbeId: p.Id, InboundId: it.InboundId, Ok: it.Ok, LatencyMs: lat, CheckedAt: now})
	}
	db := database.GetDB()
	db.Model(&model.IspProbe{}).Where("id = ?", p.Id).Updates(map[string]any{"last_seen": now, "last_ip": remoteIP})
	if len(rows) == 0 {
		return 0, nil
	}
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "probe_id"}, {Name: "inbound_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ok", "latency_ms", "checked_at"}),
	}).Create(&rows).Error
	return len(rows), err
}

type ProbeMatrix struct {
	Probes  []model.IspProbe       `json:"probes"`
	Targets []ProbeTarget          `json:"targets"`
	Results []model.IspProbeResult `json:"results"`
}

func (s *IspProbeService) Matrix() (*ProbeMatrix, error) {
	db := database.GetDB()
	m := &ProbeMatrix{}
	if err := db.Order("id asc").Find(&m.Probes).Error; err != nil {
		return nil, err
	}
	targets, err := s.Targets()
	if err != nil {
		return nil, err
	}
	m.Targets = targets
	if err := db.Find(&m.Results).Error; err != nil {
		return nil, err
	}
	return m, nil
}

// BlockedFromProbes returns inbounds that every fresh probe (at least two) failed to reach:
// the line is up but blocked for users, so line health treats it as down. A probe failing most
// of its lines is more likely broken itself (no network, no TCP tool), so it is ignored.
func BlockedFromProbes(now time.Time) map[int]bool {
	var rows []model.IspProbeResult
	database.GetDB().Where("checked_at >= ?", now.Add(-ProbeFresh).Unix()).Find(&rows)
	type tally struct{ total, failed int }
	perProbe := map[int]*tally{}
	for _, r := range rows {
		t := perProbe[r.ProbeId]
		if t == nil {
			t = &tally{}
			perProbe[r.ProbeId] = t
		}
		t.total++
		if !r.Ok {
			t.failed++
		}
	}
	counts := map[int]*tally{}
	for _, r := range rows {
		if p := perProbe[r.ProbeId]; p.total >= probeSanityMin && p.failed*2 > p.total {
			continue
		}
		t := counts[r.InboundId]
		if t == nil {
			t = &tally{}
			counts[r.InboundId] = t
		}
		t.total++
		if !r.Ok {
			t.failed++
		}
	}
	out := map[int]bool{}
	for id, t := range counts {
		if t.total >= 2 && t.failed == t.total {
			out[id] = true
		}
	}
	return out
}
