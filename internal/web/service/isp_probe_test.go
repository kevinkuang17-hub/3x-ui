package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func seedProbeLines(t *testing.T) (tcp, udp, hosted int) {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	node := &model.Node{Name: "HK A", Scheme: "https", Address: "hk.example.com", Port: 2053, ApiToken: "t", Enable: true}
	db.Create(node)
	mk := func(tag string, port int, proto model.Protocol, stream string) int {
		ib := &model.Inbound{UserId: 1, Enable: true, Port: port, Protocol: proto, Tag: tag, Remark: tag, Settings: `{"clients":[]}`, StreamSettings: stream, NodeID: &node.Id}
		if err := db.Create(ib).Error; err != nil {
			t.Fatal(err)
		}
		return ib.Id
	}
	tcp = mk("hk-tcp", 30001, model.VLESS, `{"network":"tcp"}`)
	udp = mk("hk-hy", 30002, model.Hysteria, `{}`)
	hosted = mk("hk-cdn", 30003, model.VLESS, `{"network":"ws"}`)
	db.Create(&model.Host{InboundId: hosted, Remark: "cdn"})
	return tcp, udp, hosted
}

func TestProbeTargetsAndReports(t *testing.T) {
	tcp, udp, hosted := seedProbeLines(t)
	s := &IspProbeService{}
	if _, _, err := s.Add(" ", "telecom"); err == nil {
		t.Fatal("empty name must be refused")
	}
	p1, tok1, err := s.Add("Home Telecom", "telecom")
	if err != nil {
		t.Fatal(err)
	}
	_, tok2, _ := s.Add("Office Mobile", "mobile")

	if _, err := s.TargetsText("nope", "1.2.3.4"); err == nil {
		t.Fatal("unknown tokens must be refused")
	}
	text, err := s.TargetsText(tok1, "198.51.100.9")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) != "1 hk.example.com 30001" && !strings.Contains(text, " hk.example.com 30001") {
		t.Fatalf("targets = %q", text)
	}
	if strings.Contains(text, "30002") || strings.Contains(text, "30003") {
		t.Fatalf("UDP and CDN-fronted lines must not be probed: %q", text)
	}

	n, err := s.Report(tok1, "198.51.100.9", []ProbeReportItem{{InboundId: tcp, Ok: false}, {InboundId: udp, Ok: true}, {InboundId: hosted, Ok: true}})
	if err != nil || n != 1 {
		t.Fatalf("report stored %d, err %v; only real targets count", n, err)
	}
	now := time.Now()
	if blocked := BlockedFromProbes(now); blocked[tcp] {
		t.Fatal("one failing probe is not enough to call a line blocked")
	}
	if _, err := s.Report(tok2, "203.0.113.4", []ProbeReportItem{{InboundId: tcp, Ok: false}}); err != nil {
		t.Fatal(err)
	}
	if !BlockedFromProbes(now)[tcp] {
		t.Fatal("all fresh probes failing must mark the line blocked")
	}
	// The first probe recovers: an upsert, not a second row.
	if _, err := s.Report(tok1, "198.51.100.9", []ProbeReportItem{{InboundId: tcp, Ok: true, LatencyMs: 42}}); err != nil {
		t.Fatal(err)
	}
	if BlockedFromProbes(now)[tcp] {
		t.Fatal("one probe reaching the line means it is not blocked")
	}
	if BlockedFromProbes(now.Add(ProbeFresh + time.Minute))[tcp] {
		t.Fatal("stale results must be ignored")
	}
	m, err := s.Matrix()
	if err != nil || len(m.Probes) != 2 || len(m.Targets) != 1 || len(m.Results) != 2 {
		t.Fatalf("matrix = %+v err=%v", m, err)
	}
	var probe model.IspProbe
	database.GetDB().First(&probe, p1.Id)
	if probe.LastIp != "198.51.100.9" || probe.LastSeen == 0 {
		t.Fatalf("probe last seen not tracked: %+v", probe)
	}
	if err := s.Delete(p1.Id); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.Matrix(); len(m.Results) != 1 {
		t.Fatal("deleting a probe drops its results")
	}
}

func TestBlockedFromProbesIgnoresBrokenProbes(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	now := time.Now()
	// Probes 1 and 2 fail everything (e.g. no TCP tool on the router): they must not hide lines.
	for probe := 1; probe <= 2; probe++ {
		for line := 1; line <= 4; line++ {
			db.Create(&model.IspProbeResult{ProbeId: probe, InboundId: line, Ok: false, CheckedAt: now.Unix()})
		}
	}
	if blocked := BlockedFromProbes(now); len(blocked) != 0 {
		t.Fatalf("all-failing probes must be ignored, blocked %v", blocked)
	}
	// Probes 3 and 4 reach most lines but not line 1: that line is genuinely blocked.
	for probe := 3; probe <= 4; probe++ {
		for line := 1; line <= 4; line++ {
			db.Create(&model.IspProbeResult{ProbeId: probe, InboundId: line, Ok: line != 1, CheckedAt: now.Unix()})
		}
	}
	db.Where("probe_id IN ?", []int{1, 2}).Delete(&model.IspProbeResult{})
	if blocked := BlockedFromProbes(now); !blocked[1] || len(blocked) != 1 {
		t.Fatalf("blocked = %v, want only line 1", blocked)
	}
}

// The panel's own inbounds are checked too, at its public address; before, only node lines were.
func TestProbeTargetsIncludeLocalInbounds(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	old := probeMasterAddress
	t.Cleanup(func() { probeMasterAddress = old })
	probeMasterAddress = func() string { return "203.0.113.7" }
	ib := &model.Inbound{
		UserId: 1, Enable: true, Port: 443, Protocol: model.VLESS, Tag: "us-a", Remark: "US A Reality",
		Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp"}`,
	}
	if err := database.GetDB().Create(ib).Error; err != nil {
		t.Fatal(err)
	}
	targets, err := (&IspProbeService{}).Targets()
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != (ProbeTarget{InboundId: ib.Id, Name: "US A Reality", Host: "203.0.113.7", Port: 443}) {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestChinaVerdicts(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	now := time.Now()
	add := func(probe, line int, ok bool, ping int, proxied bool) {
		db.Create(&model.IspProbeResult{ProbeId: probe, InboundId: line, Ok: ok, PingMs: ping, Proxied: proxied, CheckedAt: now.Unix()})
	}
	// Line 1: TCP blocked everywhere, host answers ping: the port is blocked.
	add(1, 1, false, 40, false)
	add(2, 1, false, 55, false)
	// Line 2: nothing answers: the IP is blocked.
	add(1, 2, false, -1, false)
	add(2, 2, false, -1, false)
	// Line 3: one probe gets through.
	add(1, 3, true, 30, false)
	add(2, 3, false, -1, false)
	// Line 4: the only "success" came through the router's own proxy, so it does not count.
	add(1, 4, true, 1, true)
	add(2, 4, false, -1, false)
	add(3, 4, false, -1, false)
	// Probe 3 keeps working lines elsewhere so it is not judged broken.
	add(3, 5, true, 20, false)
	add(3, 6, true, 20, false)

	got := ChinaVerdicts(now)
	want := map[int]string{1: "port", 2: "ip", 3: "ok", 4: "ip"}
	for line, state := range want {
		if got[line].State != state {
			t.Errorf("line %d: %+v, want %s", line, got[line], state)
		}
	}
	if got[4].Probes != 2 {
		t.Errorf("line 4 counted %d probes; the proxied result must be dropped", got[4].Probes)
	}
	if !BlockedFromProbes(now)[1] || BlockedFromProbes(now)[3] {
		t.Error("line health must hide blocked lines and keep reachable ones")
	}
}

// A probe failing everything still proves a real block when its pings come back.
func TestBrokenProbeRuleKeepsProbesWithPing(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	now := time.Now()
	for probe := 1; probe <= 2; probe++ {
		for line := 1; line <= 3; line++ {
			db.Create(&model.IspProbeResult{ProbeId: probe, InboundId: line, Ok: false, PingMs: 35, CheckedAt: now.Unix()})
		}
	}
	if v := ChinaVerdicts(now); len(v) != 3 || v[1].State != "port" {
		t.Fatalf("verdicts = %+v; every port blocked with the hosts answering ping", v)
	}
}
