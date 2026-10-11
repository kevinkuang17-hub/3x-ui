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
	return
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
