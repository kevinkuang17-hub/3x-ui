package singbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildRoutesEachUserToItsRelay(t *testing.T) {
	insts := []Instance{
		{Id: 9, Tag: "inbound-8443", Protocol: Naive, Port: 8443, ServerName: "hk.example.com",
			CertFile: "/c.pem", KeyFile: "/k.pem", Users: []User{{"bob", "pb"}}},
		{Id: 2, Tag: "inbound-443", Protocol: AnyTLS, Listen: "0.0.0.0", Port: 443, ServerName: "hk.example.com",
			CertFile: "/c.pem", KeyFile: "/k.pem", Users: []User{{"alice", "pa"}, {"carol", "pc"}}},
	}
	raw, err := Build(insts, "relay-secret")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds  []map[string]any `json:"inbounds"`
		Outbounds []struct {
			Tag      string `json:"tag"`
			Port     int    `json:"server_port"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"outbounds"`
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Inbounds[0]["type"] != "anytls" || cfg.Inbounds[1]["type"] != "naive" {
		t.Fatalf("inbounds not ordered by id: %v", cfg.Inbounds)
	}
	if cfg.Inbounds[1]["listen"] != "::" {
		t.Fatalf("empty listen must bind every address, got %v", cfg.Inbounds[1]["listen"])
	}
	users, _ := json.Marshal(cfg.Inbounds[1]["users"])
	if string(users) != `[{"password":"pb","username":"bob"}]` {
		t.Fatalf("naive users %s", users)
	}
	if len(cfg.Outbounds) != 3 || cfg.Outbounds[1].Tag != "relay-2-1" || cfg.Outbounds[1].Username != "carol" ||
		cfg.Outbounds[1].Password != "relay-secret" || cfg.Outbounds[1].Port != RelayPortForInbound(2) {
		t.Fatalf("outbounds %+v", cfg.Outbounds)
	}
	rule, _ := json.Marshal(cfg.Route.Rules[1])
	if string(rule) != `{"auth_user":["carol"],"inbound":["inbound-443"],"outbound":"relay-2-1"}` {
		t.Fatalf("rule %s", rule)
	}
	last, _ := json.Marshal(cfg.Route.Rules[len(cfg.Route.Rules)-1])
	if string(last) != `{"action":"reject","inbound":["inbound-443","inbound-8443"]}` {
		t.Fatalf("an unmatched connection must be rejected, last rule %s", last)
	}
}

func TestRelayPortStaysInItsWindow(t *testing.T) {
	for _, id := range []int{1, 1000, 1001, 123456} {
		if p := RelayPortForInbound(id); p <= RelayBasePort || p > RelayBasePort+relaySlots {
			t.Fatalf("id %d -> %d", id, p)
		}
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestEnsureBinaryDownloadsOnce(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("release layout differs")
	}
	t.Setenv("XUI_BIN_FOLDER", t.TempDir())
	old := fetch
	t.Cleanup(func() { fetch = old })
	var urls []string
	fetch = func(_ context.Context, url string) ([]byte, error) {
		urls = append(urls, url)
		dir := "sing-box-" + Version + "-linux-" + runtime.GOARCH
		return tarGz(t, map[string]string{dir + "/LICENSE": "license", dir + "/sing-box": "#!/bin/sh\n"}), nil
	}
	for range 2 {
		if err := EnsureBinary(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(BinaryPath())
	if err != nil || string(got) != "#!/bin/sh\n" {
		t.Fatalf("binary %q, %v", got, err)
	}
	if len(urls) != 1 || !strings.HasSuffix(urls[0], "/v"+Version+"/"+archiveName("linux", runtime.GOARCH)) {
		t.Fatalf("downloads %v", urls)
	}
}

// fakeSingBox stands in for sing-box: "check" exits 0, "run" records its pid and sleeps.
func fakeSingBox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)
	pids := filepath.Join(dir, "pids")
	script := "#!/bin/sh\nif [ \"$1\" = run ]; then echo $$ >> " + pids + "; exec sleep 60; fi\nexit 0\n"
	if err := os.WriteFile(BinaryPath(), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return pids
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ := os.ReadFile(path)
		n := strings.Count(string(data), "\n")
		if n > 0 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagerRestartsOnlyOnChange(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses a shell script as the binary")
	}
	pids := fakeSingBox(t)
	m := &Manager{}
	t.Cleanup(m.Stop)
	inst := Instance{Id: 1, Tag: "a", Protocol: AnyTLS, Port: 443, CertFile: "/c", KeyFile: "/k", Users: []User{{"alice", "p"}}}
	ctx := context.Background()
	if err := m.Apply(ctx, []Instance{inst}); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, []Instance{inst}); err != nil {
		t.Fatal(err)
	}
	if n := countLines(t, pids); n != 1 || !m.Status().Running {
		t.Fatalf("same config: %d starts, running %v", n, m.Status().Running)
	}
	inst.Users = append(inst.Users, User{"bob", "q"})
	if err := m.Apply(ctx, []Instance{inst}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := countLines(t, pids); n != 2 {
		t.Fatalf("changed config: %d starts, want 2", n)
	}
	written, _ := os.ReadFile(configPath())
	if !strings.Contains(string(written), `"username": "bob"`) {
		t.Fatalf("config on disk lacks the new user:\n%s", written)
	}
	if err := m.Apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if m.Status().Running {
		t.Fatal("no inbounds left, sing-box must stop")
	}
}
