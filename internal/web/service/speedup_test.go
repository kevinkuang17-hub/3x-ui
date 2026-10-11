package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeProc(t *testing.T, values map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for key, v := range values {
		p := filepath.Join(root, "proc", "sys", filepath.FromSlash(strings.ReplaceAll(key, ".", "/")))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func baseProc() map[string]string {
	m := map[string]string{
		"kernel.osrelease":                          "6.1.0-test",
		"net.ipv4.tcp_available_congestion_control": "reno cubic",
	}
	for _, kv := range speedupSysctls {
		m[kv[0]] = "orig"
	}
	m["net.ipv4.tcp_congestion_control"] = "cubic"
	return m
}

func TestSpeedupApplyAndRevert(t *testing.T) {
	root := fakeProc(t, baseProc())
	loaded := false
	s := &SpeedupService{Root: root, modprobe: func(_ context.Context, module string) error {
		if module != "tcp_bbr" {
			return errors.New("unexpected module " + module)
		}
		loaded = true
		return os.WriteFile(filepath.Join(root, "proc/sys/net/ipv4/tcp_available_congestion_control"), []byte("reno cubic bbr\n"), 0o644)
	}}

	st, err := s.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !loaded || !st.BbrOn || !st.Applied || st.Qdisc != "fq" {
		t.Fatalf("after Apply: loaded=%v status=%+v", loaded, st)
	}
	conf, _ := os.ReadFile(filepath.Join(root, speedupConf))
	if !strings.Contains(string(conf), "net.ipv4.tcp_congestion_control = bbr") {
		t.Fatalf("the tuning must persist in sysctl.d, got:\n%s", conf)
	}
	if _, err := os.Stat(filepath.Join(root, speedupModules)); err != nil {
		t.Fatal("a module loaded on demand must load again after reboot")
	}

	// A second Apply must not overwrite the saved originals with the tuned values.
	if _, err := s.Apply(); err != nil {
		t.Fatal(err)
	}
	st, err = s.Revert()
	if err != nil {
		t.Fatal(err)
	}
	if st.Applied || st.CongestionControl != "cubic" || st.Qdisc != "orig" {
		t.Fatalf("Revert must restore the original values, got %+v", st)
	}
	for _, rel := range []string{speedupConf, speedupBackup, speedupModules} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Errorf("%s left behind after Revert", rel)
		}
	}
}

func TestSpeedupRefusesKernelsWithoutBBR(t *testing.T) {
	root := fakeProc(t, baseProc())
	s := &SpeedupService{Root: root, modprobe: func(context.Context, string) error { return errors.New("not found") }}
	if _, err := s.Apply(); err == nil || !strings.Contains(err.Error(), "BBR") {
		t.Fatalf("want a BBR error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, speedupConf)); !os.IsNotExist(err) {
		t.Fatal("nothing may be written when BBR is missing")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "proc/sys/net/core/default_qdisc")); strings.TrimSpace(string(b)) != "orig" {
		t.Fatal("no setting may change when BBR is missing")
	}
}
