package singbox

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

// Status is what the panel shows about the sidecar.
type Status struct {
	Running bool   `json:"running" example:"true"`
	Error   string `json:"error" example:""`
	Version string `json:"version" example:"1.14.3"`
}

// Manager keeps one sing-box process serving every AnyTLS and NaiveProxy inbound on this server.
type Manager struct {
	mu      sync.Mutex
	swept   bool
	cmd     *exec.Cmd
	done    chan struct{}
	config  []byte
	lastErr string
	output  *lastLines
}

var (
	manager     *Manager
	managerOnce sync.Once
)

func GetManager() *Manager {
	managerOnce.Do(func() { manager = &Manager{} })
	return manager
}

func configPath() string {
	return filepath.Join(config.GetBinFolderPath(), "singbox", "config.json")
}

// Apply serves exactly insts: it starts, restarts on a changed config, or stops sing-box. A process
// that died is started again on the next call.
func (m *Manager) Apply(ctx context.Context, insts []Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(insts) == 0 {
		m.stopLocked()
		m.config, m.lastErr = nil, ""
		return nil
	}
	cfg, err := Build(insts, RelayPassword())
	if err != nil {
		return m.failLocked(err)
	}
	if m.runningLocked() && bytes.Equal(cfg, m.config) {
		return nil
	}
	if err := EnsureBinary(ctx); err != nil {
		return m.failLocked(err)
	}
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return m.failLocked(err)
	}
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		return m.failLocked(err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	out, err := exec.CommandContext(checkCtx, BinaryPath(), "check", "-c", path).CombinedOutput()
	cancel()
	if err != nil {
		return m.failLocked(errors.New("sing-box rejected the config: " + strings.TrimSpace(string(out))))
	}
	m.stopLocked()
	if err := m.startLocked(path); err != nil {
		return m.failLocked(err)
	}
	m.config, m.lastErr = cfg, ""
	return nil
}

func (m *Manager) failLocked(err error) error {
	m.lastErr = err.Error()
	return err
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Running: m.runningLocked(), Error: m.lastErr, Version: Version}
	if !st.Running && st.Error == "" && m.output != nil && m.config != nil {
		st.Error = m.output.last()
	}
	return st
}

// Stop ends the process, e.g. on panel shutdown.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	m.config = nil
}

func (m *Manager) runningLocked() bool {
	if m.done == nil {
		return false
	}
	select {
	case <-m.done:
		return false
	default:
		return true
	}
}

func (m *Manager) startLocked(path string) error {
	if !m.swept {
		m.swept = true
		if n := killStray(BinaryPath()); n > 0 {
			logger.Warning("sing-box: ended", n, "process(es) left by a previous panel run")
		}
	}
	cmd := exec.Command(BinaryPath(), "run", "-c", path)
	m.output = &lastLines{}
	cmd.Stdout, cmd.Stderr = m.output, m.output
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		err := cmd.Wait()
		if err != nil {
			logger.Warning("sing-box exited:", err)
		}
		close(done)
	}()
	m.cmd, m.done = cmd, done
	return nil
}

func (m *Manager) stopLocked() {
	if !m.runningLocked() {
		m.cmd, m.done = nil, nil
		return
	}
	_ = m.cmd.Process.Signal(os.Interrupt)
	select {
	case <-m.done:
	case <-time.After(5 * time.Second):
		_ = m.cmd.Process.Kill()
		<-m.done
	}
	m.cmd, m.done = nil, nil
}

// lastLines forwards sing-box output to the panel log and keeps the last line for Status.
type lastLines struct {
	mu   sync.Mutex
	line string
}

func (l *lastLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			l.line = line
			logger.Info("sing-box |", line)
		}
	}
	return len(p), nil
}

func (l *lastLines) last() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}
