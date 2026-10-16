package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
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
	// Node is empty for an inbound on this panel's own server.
	Node string `json:"node"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// probeMasterAddress is where China reaches this panel's own inbounds; tests replace it.
var probeMasterAddress = cachedPublicIPv4

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

// maxProbes bounds how many probes self-enrollment can create, should the key leak.
const maxProbes = 100

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// EnrollKey returns the shared enrollment key, creating it on first use; reset replaces it, so
// the old one-line command stops working.
func (s *IspProbeService) EnrollKey(reset bool) (string, error) {
	settings := SettingService{}
	key, _ := settings.GetProbeEnrollKey()
	if key != "" && !reset {
		return key, nil
	}
	key, err := randomHex(16)
	if err != nil {
		return "", err
	}
	return key, settings.SetProbeEnrollKey(key)
}

// ispFromText reads the carrier out of a location line such as "中国 广东 广州 电信".
func ispFromText(text string) string {
	switch {
	case strings.Contains(text, "电信"):
		return "telecom"
	case strings.Contains(text, "联通"):
		return "unicom"
	case strings.Contains(text, "移动"):
		return "mobile"
	}
	return "other"
}

// Enroll registers a router that ran the shared command and returns its own probe token. The same
// device enrolling again gets its existing probe back, with a fresh token.
func (s *IspProbeService) Enroll(key, deviceId, name, remoteIP string) (string, error) {
	want, _ := (&SettingService{}).GetProbeEnrollKey()
	if want == "" || key != want {
		return "", common.NewError("unknown enrollment key")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "probe " + remoteIP
	}
	if len(name) > 64 {
		name = name[:64]
	}
	deviceId = strings.TrimSpace(deviceId)
	token, err := randomHex(20)
	if err != nil {
		return "", err
	}
	db := database.GetDB()
	var p model.IspProbe
	if deviceId != "" && db.Where("device_id = ?", deviceId).First(&p).Error == nil {
		err := db.Model(&p).Updates(map[string]any{"token_hash": hashProbeToken(token), "name": name, "isp": ispFromText(name), "last_ip": remoteIP}).Error
		return token, err
	}
	var count int64
	db.Model(&model.IspProbe{}).Count(&count)
	if count >= maxProbes {
		return "", common.NewErrorf("already %d probes; delete some first", count)
	}
	p = model.IspProbe{Name: name, Isp: ispFromText(name), TokenHash: hashProbeToken(token), DeviceId: deviceId, LastIp: remoteIP}
	return token, db.Create(&p).Error
}

// SubOrigin is the subscription server's public origin ("" when unknown): from inside China the
// panel domain may be blocked while the subscription domain, often behind a CDN, still answers.
func (s *IspProbeService) SubOrigin() string {
	settings := SettingService{}
	if enabled, _ := settings.GetSubEnable(); !enabled {
		return ""
	}
	if uri, _ := settings.GetSubURI(); uri != "" {
		if u, err := url.Parse(uri); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
		return ""
	}
	domain, _ := settings.GetSubDomain()
	if domain == "" {
		return ""
	}
	cert, _ := settings.GetSubCertFile()
	key, _ := settings.GetSubKeyFile()
	port, _ := settings.GetSubPort()
	scheme := "http"
	if cert != "" && key != "" {
		scheme = "https"
	}
	origin := scheme + "://" + domain
	if standard := (scheme == "https" && port == 443) || (scheme == "http" && port == 80); !standard {
		origin += ":" + strconv.Itoa(port)
	}
	return origin
}

// SubProbeURL is the probe URL on the subscription server, "" when its address is unknown.
func (s *IspProbeService) SubProbeURL(token string) string {
	if origin := s.SubOrigin(); origin != "" {
		return origin + "/probe/" + token
	}
	return ""
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

// Targets lists the TCP lines a probe should test: every enabled inbound, on a node or on this
// server, at the address subscriptions advertise.
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
	if err := db.Where("enable = ?", true).Order("id asc").Find(&inbounds).Error; err != nil {
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
	var master *model.Node
	out := make([]ProbeTarget, 0, len(inbounds))
	for _, ib := range inbounds {
		if skip[ib.Id] || linehealth.IsUDP(ib) || ib.Port <= 0 {
			continue
		}
		node, nodeName := (*model.Node)(nil), ""
		if ib.NodeID != nil {
			if node = byID[*ib.NodeID]; node == nil {
				continue
			}
			nodeName = node.Name
		} else {
			if master == nil {
				master = &model.Node{Address: probeMasterAddress()}
			}
			node = master
		}
		host := linehealth.ProbeHost(ib, node)
		if host == "" {
			continue
		}
		name := ib.Remark
		if name == "" {
			name = ib.Tag
		}
		out = append(out, ProbeTarget{InboundId: ib.Id, Name: name, Node: nodeName, Host: host, Port: ib.Port})
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
	// PingMs: -1 lost, 0 not measured (older agents, no ping tool).
	PingMs  int  `json:"pingMs"`
	Proxied bool `json:"proxied"`
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
		ping := it.PingMs
		if ping < -1 {
			ping = -1
		}
		rows = append(rows, model.IspProbeResult{
			ProbeId: p.Id, InboundId: it.InboundId, Ok: it.Ok, LatencyMs: lat, CheckedAt: now,
			PingMs: ping, Proxied: it.Proxied,
		})
	}
	db := database.GetDB()
	db.Model(&model.IspProbe{}).Where("id = ?", p.Id).Updates(map[string]any{"last_seen": now, "last_ip": remoteIP})
	if len(rows) == 0 {
		return 0, nil
	}
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "probe_id"}, {Name: "inbound_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"ok", "latency_ms", "checked_at", "ping_ms", "proxied"}),
	}).Create(&rows).Error
	return len(rows), err
}

type ProbeMatrix struct {
	Probes  []model.IspProbe       `json:"probes"`
	Targets []ProbeTarget          `json:"targets"`
	Results []model.IspProbeResult `json:"results"`
	// Verdicts maps an inbound id to what the fresh results say about it from inside China.
	Verdicts map[int]ChinaVerdict `json:"verdicts"`
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
	m.Verdicts = ChinaVerdicts(time.Now())
	return m, nil
}

// ChinaVerdict sums up one line from the fresh, trustworthy probe results.
type ChinaVerdict struct {
	// State: ok (some probe gets through), port (TCP fails everywhere but the host answers
	// ping: the port is blocked), ip (nothing gets through: the IP is blocked) or blocked
	// (TCP fails everywhere, no ping to tell why).
	State  string `json:"state"`
	Probes int    `json:"probes"`
	TcpOk  int    `json:"tcpOk"`
	PingOk int    `json:"pingOk"`
}

// freshResults drops stale results, results a local proxy answered, and every result of a probe
// that fails most of its lines without any ping answer: it is more likely broken itself.
func freshResults(now time.Time) []model.IspProbeResult {
	var rows []model.IspProbeResult
	database.GetDB().Where("checked_at >= ? AND proxied = ?", now.Add(-ProbeFresh).Unix(), false).Find(&rows)
	type tally struct{ total, failed, pinged int }
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
		if r.PingMs > 0 {
			t.pinged++
		}
	}
	kept := rows[:0]
	for _, r := range rows {
		// A probe that gets ping answers has a network, so its TCP failures are real blocks.
		if p := perProbe[r.ProbeId]; p.total >= probeSanityMin && p.failed*2 > p.total && p.pinged == 0 {
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

// ChinaVerdicts judges every line at least two fresh probes checked.
func ChinaVerdicts(now time.Time) map[int]ChinaVerdict {
	byLine := map[int]*ChinaVerdict{}
	pinged := map[int]int{}
	for _, r := range freshResults(now) {
		v := byLine[r.InboundId]
		if v == nil {
			v = &ChinaVerdict{}
			byLine[r.InboundId] = v
		}
		v.Probes++
		if r.Ok {
			v.TcpOk++
		}
		if r.PingMs != 0 {
			pinged[r.InboundId]++
			if r.PingMs > 0 {
				v.PingOk++
			}
		}
	}
	out := make(map[int]ChinaVerdict, len(byLine))
	for id, v := range byLine {
		switch {
		case v.TcpOk > 0:
			v.State = "ok"
		case v.Probes < 2:
			continue
		case v.PingOk > 0:
			v.State = "port"
		case pinged[id] > 0:
			v.State = "ip"
		default:
			v.State = "blocked"
		}
		out[id] = *v
	}
	return out
}

// BlockedFromProbes returns inbounds that every fresh probe (at least two) failed to reach:
// the line is up but blocked for users, so line health treats it as down.
func BlockedFromProbes(now time.Time) map[int]bool {
	out := map[int]bool{}
	for id, v := range ChinaVerdicts(now) {
		if v.State != "ok" {
			out[id] = true
		}
	}
	return out
}
