package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// fakeCloudflare keeps A records in memory and speaks just enough of the v4 API.
type fakeCloudflare struct {
	mu      sync.Mutex
	records map[string]string // name -> ip
	writes  int
}

func (f *fakeCloudflare) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
	}
	if r.Header.Get("Authorization") != "Bearer cf-token" {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "bad token"}}})
		return
	}
	switch {
	case r.URL.Path == "/zones":
		reply([]map[string]string{{"id": "z1"}})
	case r.URL.Path == "/zones/z1/dns_records" && r.Method == http.MethodGet:
		name := r.URL.Query().Get("name")
		if ip, ok := f.records[name]; ok {
			reply([]map[string]string{{"id": "rec-" + name, "content": ip}})
			return
		}
		reply([]any{})
	case strings.HasPrefix(r.URL.Path, "/zones/z1/dns_records"):
		var rec struct {
			Name    string `json:"name"`
			Content string `json:"content"`
			Proxied bool   `json:"proxied"`
		}
		_ = json.NewDecoder(r.Body).Decode(&rec)
		if rec.Proxied {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.records[rec.Name] = rec.Content
		f.writes++
		reply(map[string]string{"id": "rec-" + rec.Name})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func setupCloudflare(t *testing.T, withZone bool) *fakeCloudflare {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	f := &fakeCloudflare{records: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	prev := cloudflareAPI
	cloudflareAPI = srv.URL
	t.Cleanup(func() { cloudflareAPI = prev })
	cfZoneIDCache.Range(func(k, _ any) bool { cfZoneIDCache.Delete(k); return true })
	if withZone {
		db := database.GetDB()
		db.Create(&model.Setting{Key: "cfApiToken", Value: "cf-token"})
		db.Create(&model.Setting{Key: "cfZone", Value: "Example.com"})
	}
	return f
}

func TestNodeDnsFQDN(t *testing.T) {
	setupCloudflare(t, true)
	s := &NodeDnsService{}
	for in, want := range map[string]string{"hk-a": "hk-a.example.com", "HK-A.example.com.": "hk-a.example.com"} {
		if got, err := s.FQDN(in); err != nil || got != want {
			t.Errorf("FQDN(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "hk a", "-hk", "hk_a"} {
		if _, err := s.FQDN(bad); err == nil {
			t.Errorf("FQDN(%q) must fail", bad)
		}
	}
}

func TestNodeDnsSetSyncAndAutoFollow(t *testing.T) {
	f := setupCloudflare(t, true)
	db := database.GetDB()
	node := &model.Node{Name: "HK A", Scheme: "https", Address: "198.51.100.10", Port: 2053, ApiToken: "t", Enable: true}
	if err := db.Create(node).Error; err != nil {
		t.Fatal(err)
	}
	s := &NodeDnsService{probe: func(context.Context, *model.Node) error { return errors.New("not yet") }}
	ctx := context.Background()
	if _, err := s.Set(ctx, node.Id, "hk-a", "", true); err == nil {
		t.Fatal("the address must not switch while the master cannot reach the node there")
	}
	var unchanged model.Node
	db.First(&unchanged, node.Id)
	if unchanged.Address != "198.51.100.10" {
		t.Fatalf("address switched despite failed probe: %q", unchanged.Address)
	}
	s.probe = func(context.Context, *model.Node) error { return nil }
	if _, err := s.Set(ctx, node.Id, "hk-a", "", true); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if f.records["hk-a.example.com"] != "198.51.100.10" {
		t.Fatalf("record = %v", f.records)
	}
	var got model.Node
	db.First(&got, node.Id)
	if got.Address != "hk-a.example.com" {
		t.Fatalf("node address = %q, want the DNS name", got.Address)
	}

	// Manual "change IP".
	if _, err := s.Sync(ctx, node.Id, "198.51.100.20"); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if f.records["hk-a.example.com"] != "198.51.100.20" {
		t.Fatalf("record after sync = %v", f.records)
	}

	// Heartbeat-reported change only follows when cfAutoDns is on.
	writes := f.writes
	s.ObserveIP(node.Id, "198.51.100.30")
	if f.writes != writes || ObservedIP(node.Id) != "198.51.100.30" {
		t.Fatal("auto DNS must stay off by default")
	}
	db.Create(&model.Setting{Key: "cfAutoDns", Value: "true"})
	dnsAttempts.Delete(node.Id)
	s.ObserveIP(node.Id, "198.51.100.30")
	if f.records["hk-a.example.com"] != "198.51.100.30" {
		t.Fatalf("auto DNS did not follow: %v", f.records)
	}
	if ShouldObserveIP(node.Id, "198.51.100.30") {
		t.Fatal("an unchanged IP must not trigger another check right away")
	}
	if !ShouldObserveIP(node.Id, "198.51.100.31") {
		t.Fatal("a changed IP must trigger a check")
	}

	// The node's own report: only for its key, only for the caller's own address.
	var link model.NodeDns
	db.Where("node_id = ?", node.Id).First(&link)
	if link.ReportKey == "" {
		t.Fatal("a linked node needs a report key")
	}
	if _, _, err := s.ReportIP(ctx, link.ReportKey, "198.51.100.40", "203.0.113.1"); err == nil {
		t.Fatal("a claimed IP different from the caller must be refused")
	}
	if _, _, err := s.ReportIP(ctx, "wrong", "198.51.100.40", "198.51.100.40"); err == nil {
		t.Fatal("unknown keys must be refused")
	}
	if _, _, err := s.ReportIP(ctx, link.ReportKey, "10.1.2.3", "10.1.2.3"); err == nil {
		t.Fatal("private addresses must be refused")
	}
	dnsAttempts.Delete(node.Id)
	if _, changed, err := s.ReportIP(ctx, link.ReportKey, "198.51.100.40", "198.51.100.40"); err != nil || !changed {
		t.Fatalf("report: changed=%v err=%v", changed, err)
	}
	if f.records["hk-a.example.com"] != "198.51.100.40" {
		t.Fatalf("report did not move the record: %v", f.records)
	}

	// Deleting the node drops its link on the next listing.
	db.Delete(&model.Node{}, node.Id)
	if rows, _ := s.List(); len(rows) != 0 {
		t.Fatalf("orphaned link kept: %+v", rows)
	}
}

func TestNodeDnsRequiresConfiguration(t *testing.T) {
	setupCloudflare(t, false)
	if _, err := (&NodeDnsService{}).Set(context.Background(), 1, "hk-a", "198.51.100.1", false); err == nil {
		t.Fatal("Set must refuse without Cloudflare settings")
	}
}

func TestNodeJoinNamesAndDNS(t *testing.T) {
	f := setupCloudflare(t, true)
	db := database.GetDB()
	db.Create(&model.Node{Name: "HK A", Scheme: "https", Address: "198.51.100.1", Port: 2053, ApiToken: "t", Enable: true})

	var probed []string
	j := &NodeJoinService{probe: func(_ context.Context, n *model.Node) error {
		probed = append(probed, n.Address+"/"+n.TlsVerifyMode)
		if n.Address == "hk-c.example.com" {
			return nil
		}
		return errors.New("unreachable")
	}}
	token, _, err := j.MintTicket()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := j.Prepare(ctx, token, "10.0.0.5", "hk", ""); err == nil {
		t.Fatal("private IPs must be refused")
	}
	if _, err := j.Prepare(ctx, token, "100.64.1.1", "hk", ""); err == nil {
		t.Fatal("CGNAT addresses must be refused")
	}
	if _, err := j.Prepare(ctx, token, "203.0.113.9", "hk", "198.51.100.77"); err == nil {
		t.Fatal("the IP must match the caller")
	}
	// hk-b is taken by an unrelated record, so the join must skip to HK C.
	f.records["hk-b.example.com"] = "192.0.2.200"
	prep, err := j.Prepare(ctx, token, "203.0.113.9", "hk", "203.0.113.9")
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prep.Name != "HK C" || prep.Domain != "hk-c.example.com" || f.records["hk-c.example.com"] != "203.0.113.9" || f.records["hk-b.example.com"] != "192.0.2.200" {
		t.Fatalf("prepare = %+v records=%v", prep, f.records)
	}
	again, _ := j.Prepare(ctx, token, "203.0.113.9", "hk", "")
	if again.Name != "HK C" {
		t.Fatalf("re-running prepare must keep the reservation, got %+v", again)
	}

	if _, err := j.Finish(ctx, token, NodeJoinFinish{Scheme: "http", Port: 40123, ApiToken: "node-token"}); err == nil {
		t.Fatal("plain http must need an explicit opt-in")
	}
	res, err := j.Finish(ctx, token, NodeJoinFinish{Scheme: "https", Port: 40123, BasePath: "abc", ApiToken: "node-token"})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	n := res.Node
	if n.Name != "HK C" || n.Address != "hk-c.example.com" || n.TlsVerifyMode != "verify" || n.BasePath != "/abc/" || res.ReportKey == "" {
		t.Fatalf("node = %+v", n)
	}
	if len(probed) != 1 {
		t.Fatalf("domain with verification should be tried first, probed %v", probed)
	}
	var link model.NodeDns
	if err := db.Where("node_id = ?", n.Id).First(&link).Error; err != nil || link.Name != "hk-c.example.com" || link.ReportKey != res.ReportKey {
		t.Fatalf("dns link = %+v err=%v", link, err)
	}
	if _, err := j.Finish(ctx, token, NodeJoinFinish{Scheme: "https", Port: 40123, ApiToken: "x"}); err == nil {
		t.Fatal("a used ticket must not register a second node")
	}
}

func TestNodeJoinFallsBackAndReportsUnreachable(t *testing.T) {
	setupCloudflare(t, false)
	var probed []string
	j := &NodeJoinService{
		probe: func(_ context.Context, n *model.Node) error {
			probed = append(probed, n.Address+"/"+n.TlsVerifyMode)
			return errors.New("connection refused")
		},
		fingerprint: func(context.Context, *model.Node) (string, error) {
			return "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=", nil
		},
	}
	token, _, _ := j.MintTicket()
	prep, err := j.Prepare(context.Background(), token, "203.0.113.50", "", "")
	if err != nil || prep.Name != "NODE A" || prep.Domain != "" {
		t.Fatalf("prepare = %+v err=%v", prep, err)
	}
	if _, err := j.Finish(context.Background(), token, NodeJoinFinish{Scheme: "https", Port: 1234, ApiToken: "x"}); err == nil {
		t.Fatal("unreachable node must not be added")
	}
	want := []string{"203.0.113.50/verify", "203.0.113.50/pin"}
	if strings.Join(probed, ",") != strings.Join(want, ",") {
		t.Fatalf("probed %v, want %v", probed, want)
	}
	var count int64
	database.GetDB().Model(&model.NodeJoinTicket{}).Where("token = ?", token).Count(&count)
	if count != 1 {
		t.Fatal("a failed finish must keep the ticket so the script can be re-run")
	}
}

func TestNextNodeName(t *testing.T) {
	taken := map[string]bool{"US A": true, "US B": true}
	if got := nextNodeName("US", taken); got != "US C" {
		t.Fatalf("got %q", got)
	}
	if letterName(26) != "A2" {
		t.Fatalf("letterName(26) = %q", letterName(26))
	}
}

func TestNodeJoinPinsWhenNoCertificateMatches(t *testing.T) {
	setupCloudflare(t, false)
	const pin = "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
	j := &NodeJoinService{
		probe: func(_ context.Context, n *model.Node) error {
			if n.TlsVerifyMode == "pin" && n.PinnedCertSha256 == pin {
				return nil
			}
			return errors.New("x509: certificate is valid for other.example, not 203.0.113.60")
		},
		fingerprint: func(context.Context, *model.Node) (string, error) { return pin, nil },
	}
	token, _, _ := j.MintTicket()
	if _, err := j.Prepare(context.Background(), token, "203.0.113.60", "us", ""); err != nil {
		t.Fatal(err)
	}
	res, err := j.Finish(context.Background(), token, NodeJoinFinish{Scheme: "https", Port: 2053, ApiToken: "x"})
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if res.Node.TlsVerifyMode != "pin" || res.Node.PinnedCertSha256 != pin || res.Node.Name != "US A" {
		t.Fatalf("node = %+v", res.Node)
	}
}
