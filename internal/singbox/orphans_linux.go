//go:build linux

package singbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// killStray ends sing-box processes a crashed panel left behind: they still hold the inbound
// ports, so the new one could not bind. Only the panel runs a binary with this name.
func killStray(binaryPath string) int {
	base := filepath.Base(binaryPath)
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	killed := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		argv0, _, _ := strings.Cut(string(cmdline), "\x00")
		if filepath.Base(argv0) != base {
			continue
		}
		if syscall.Kill(pid, syscall.SIGKILL) == nil {
			killed++
		}
	}
	return killed
}
