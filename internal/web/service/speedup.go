package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// speedupSysctls is the network tuning applied in one click: BBR with fq pacing, TCP Fast Open and
// larger socket buffers for long, fast links. Values only raise limits; they never cap a server.
var speedupSysctls = [][2]string{
	// Congestion control goes first: if the kernel refuses it, Apply stops before changing anything else.
	{"net.ipv4.tcp_congestion_control", "bbr"},
	{"net.core.default_qdisc", "fq"},
	{"net.ipv4.tcp_fastopen", "3"},
	{"net.core.rmem_max", "33554432"},
	{"net.core.wmem_max", "33554432"},
	{"net.ipv4.tcp_rmem", "4096 131072 33554432"},
	{"net.ipv4.tcp_wmem", "4096 16384 33554432"},
	{"net.ipv4.tcp_mtu_probing", "1"},
	{"net.ipv4.tcp_slow_start_after_idle", "0"},
	{"net.ipv4.tcp_notsent_lowat", "131072"},
	{"net.core.somaxconn", "4096"},
	{"net.ipv4.tcp_max_syn_backlog", "8192"},
	{"net.core.netdev_max_backlog", "16384"},
}

const (
	speedupConf    = "etc/sysctl.d/99-x-ui-speedup.conf"
	speedupBackup  = "etc/sysctl.d/99-x-ui-speedup.backup"
	speedupModules = "etc/modules-load.d/x-ui-bbr.conf"
)

// SpeedupService applies kernel network tuning on this server. Root is "/" in production and a
// temp dir in tests; modprobe is swappable for the same reason.
type SpeedupService struct {
	Root     string
	modprobe func(ctx context.Context, module string) error
}

type SpeedupStatus struct {
	Kernel            string   `json:"kernel"`
	CongestionControl string   `json:"congestionControl"`
	Qdisc             string   `json:"qdisc"`
	Available         []string `json:"available"`
	FastOpen          string   `json:"fastOpen"`
	RmemMax           string   `json:"rmemMax"`
	Applied           bool     `json:"applied"`
	BbrOn             bool     `json:"bbrOn"`
}

func (s *SpeedupService) root() string {
	if s.Root != "" {
		return s.Root
	}
	return "/"
}

func (s *SpeedupService) path(rel string) string {
	return filepath.Join(s.root(), filepath.FromSlash(rel))
}

func (s *SpeedupService) procKey(key string) string {
	return s.path("proc/sys/" + strings.ReplaceAll(key, ".", "/"))
}

func (s *SpeedupService) read(key string) string {
	b, err := os.ReadFile(s.procKey(key))
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

func (s *SpeedupService) write(key, value string) error {
	return os.WriteFile(s.procKey(key), []byte(value+"\n"), 0o644)
}

func (s *SpeedupService) supported() error {
	if goruntime.GOOS != "linux" && s.Root == "" {
		return common.NewError("network tuning is only available on Linux servers")
	}
	return nil
}

func (s *SpeedupService) Status() (*SpeedupStatus, error) {
	if err := s.supported(); err != nil {
		return nil, err
	}
	kernel, _ := os.ReadFile(s.path("proc/sys/kernel/osrelease"))
	st := &SpeedupStatus{
		Kernel:            strings.TrimSpace(string(kernel)),
		CongestionControl: s.read("net.ipv4.tcp_congestion_control"),
		Qdisc:             s.read("net.core.default_qdisc"),
		Available:         strings.Fields(s.read("net.ipv4.tcp_available_congestion_control")),
		FastOpen:          s.read("net.ipv4.tcp_fastopen"),
		RmemMax:           s.read("net.core.rmem_max"),
	}
	_, err := os.Stat(s.path(speedupConf))
	st.Applied = err == nil
	st.BbrOn = st.CongestionControl == "bbr"
	return st, nil
}

func (s *SpeedupService) loadModule(ctx context.Context, module string) error {
	if s.modprobe != nil {
		return s.modprobe(ctx, module)
	}
	return exec.CommandContext(ctx, "modprobe", module).Run()
}

// Apply switches to BBR and writes the tuning to sysctl.d so it survives reboots. The values it
// replaces are saved once, so Revert can restore them without a reboot.
func (s *SpeedupService) Apply() (*SpeedupStatus, error) {
	if err := s.supported(); err != nil {
		return nil, err
	}
	if !slices.Contains(strings.Fields(s.read("net.ipv4.tcp_available_congestion_control")), "bbr") {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = s.loadModule(ctx, "tcp_bbr")
		cancel()
		if !slices.Contains(strings.Fields(s.read("net.ipv4.tcp_available_congestion_control")), "bbr") {
			return nil, common.NewError("this kernel has no BBR (needs Linux 4.9+ and a KVM/dedicated server, not OpenVZ/LXC)")
		}
		if err := writeFileMkdir(s.path(speedupModules), "tcp_bbr\n"); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(s.path(speedupBackup)); errors.Is(err, os.ErrNotExist) {
		var backup strings.Builder
		for _, kv := range speedupSysctls {
			if v := s.read(kv[0]); v != "" {
				fmt.Fprintf(&backup, "%s = %s\n", kv[0], v)
			}
		}
		if err := writeFileMkdir(s.path(speedupBackup), backup.String()); err != nil {
			return nil, err
		}
	}
	var conf strings.Builder
	conf.WriteString("# Written by x-ui one-click speedup; remove it from the panel (Protect & speed) to undo.\n")
	var skipped []string
	for _, kv := range speedupSysctls {
		if err := s.write(kv[0], kv[1]); err != nil {
			if kv[0] == "net.ipv4.tcp_congestion_control" {
				return nil, common.NewError("could not switch to BBR:", err.Error())
			}
			skipped = append(skipped, kv[0])
			continue
		}
		fmt.Fprintf(&conf, "%s = %s\n", kv[0], kv[1])
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&conf, "# Not settable on this server: %s\n", strings.Join(skipped, ", "))
	}
	if err := writeFileMkdir(s.path(speedupConf), conf.String()); err != nil {
		return nil, err
	}
	return s.Status()
}

// Revert restores the values Apply replaced and removes its files.
func (s *SpeedupService) Revert() (*SpeedupStatus, error) {
	if err := s.supported(); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(s.path(speedupBackup)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			_ = s.write(strings.TrimSpace(key), strings.TrimSpace(value))
		}
	}
	for _, rel := range []string{speedupConf, speedupBackup, speedupModules} {
		if err := os.Remove(s.path(rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return s.Status()
}

func writeFileMkdir(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
