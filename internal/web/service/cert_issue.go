package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// CertIssueStatus is the progress of a certificate request through acme.sh and Cloudflare DNS.
type CertIssueStatus struct {
	Domain   string `json:"domain" example:"hk.example.com"`
	Running  bool   `json:"running" example:"false"`
	Done     bool   `json:"done" example:"true"`
	Error    string `json:"error" example:""`
	Step     string `json:"step" example:"issue"`
	Log      string `json:"log" example:"Cert success."`
	CertFile string `json:"certFile" example:"/root/cert/hk.example.com/fullchain.pem"`
	KeyFile  string `json:"keyFile" example:"/root/cert/hk.example.com/privkey.pem"`
	// Panel is true when the certificate was also set as the panel's HTTPS certificate.
	Panel bool `json:"panel" example:"true"`
}

// CertIssueService obtains a Let's Encrypt certificate with a DNS challenge on Cloudflare, so
// neither port 80 nor a reachable panel is needed; acme.sh's own cron renews it.
type CertIssueService struct {
	settings SettingService
}

var certIssue struct {
	mu sync.Mutex
	st CertIssueStatus
}

// certRoot is where issued certificates are installed, the same folder the x-ui menu uses.
var certRoot = "/root/cert"

// runAcme runs a command with extra environment; tests replace it.
var runAcme = func(ctx context.Context, env []string, name string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	}
	return string(out), code, err
}

var certDomainPattern = regexp.MustCompile(`^(?i)[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

// AcmeCertPaths is where a certificate issued from the panel for domain lives.
func AcmeCertPaths(domain string) (certFile, keyFile string) {
	dir := filepath.Join(certRoot, domain)
	return filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
}

func acmeHome() string {
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/root"
}

func (s *CertIssueService) Status() CertIssueStatus {
	certIssue.mu.Lock()
	defer certIssue.mu.Unlock()
	return certIssue.st
}

// Start begins issuing in the background; Status reports the outcome. onPanelCert runs after the
// certificate became the panel's own, since the panel only loads it on start.
func (s *CertIssueService) Start(domain string, usePanel bool, onPanelCert func()) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if runtime.GOOS != "linux" {
		return common.NewError("certificates can only be requested from the panel on Linux")
	}
	if !certDomainPattern.MatchString(domain) {
		return common.NewErrorf("not a domain name: %q", domain)
	}
	token, _ := s.settings.GetCfApiToken()
	if strings.TrimSpace(token) == "" {
		return common.NewError("set a Cloudflare API token (Zone DNS edit) in the panel settings first")
	}
	certIssue.mu.Lock()
	if certIssue.st.Running {
		certIssue.mu.Unlock()
		return common.NewError("a certificate request is already running")
	}
	certIssue.st = CertIssueStatus{Domain: domain, Running: true, Step: "install"}
	certIssue.mu.Unlock()
	go s.issue(domain, strings.TrimSpace(token), usePanel, onPanelCert)
	return nil
}

func setIssueStep(step, log string) {
	certIssue.mu.Lock()
	defer certIssue.mu.Unlock()
	certIssue.st.Step = step
	if log != "" {
		certIssue.st.Log = logTail(log, 4000)
	}
}

func finishIssue(err error, mutate func(*CertIssueStatus)) {
	certIssue.mu.Lock()
	defer certIssue.mu.Unlock()
	certIssue.st.Running = false
	certIssue.st.Done = err == nil
	if err != nil {
		certIssue.st.Error = err.Error()
	}
	if mutate != nil {
		mutate(&certIssue.st)
	}
}

func logTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func (s *CertIssueService) issue(domain, token string, usePanel bool, onPanelCert func()) {
	err := s.issueSteps(domain, token, usePanel)
	if err != nil {
		finishIssue(err, nil)
		return
	}
	if usePanel && onPanelCert != nil {
		onPanelCert()
	}
	certFile, keyFile := AcmeCertPaths(domain)
	finishIssue(nil, func(st *CertIssueStatus) {
		st.CertFile, st.KeyFile, st.Panel, st.Step = certFile, keyFile, usePanel, "done"
	})
}

func (s *CertIssueService) issueSteps(domain, token string, usePanel bool) error {
	home := acmeHome()
	env := []string{"HOME=" + home, "CF_Token=" + token}
	acme := filepath.Join(home, ".acme.sh", "acme.sh")
	run := func(timeout time.Duration, name string, args ...string) (string, int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return runAcme(ctx, env, name, args...)
	}
	if _, err := os.Stat(acme); err != nil {
		out, _, err := run(3*time.Minute, "sh", "-c", "curl -fsSL https://get.acme.sh | sh")
		setIssueStep("install", out)
		if err != nil {
			return common.NewErrorf("installing acme.sh failed: %v", err)
		}
	}
	setIssueStep("issue", "")
	if out, _, err := run(time.Minute, acme, "--set-default-ca", "--server", "letsencrypt"); err != nil {
		setIssueStep("issue", out)
		return common.NewErrorf("acme.sh --set-default-ca: %v", err)
	}
	out, code, err := run(10*time.Minute, acme, "--issue", "--dns", "dns_cf", "-d", domain, "--keylength", "ec-256")
	setIssueStep("issue", out)
	// acme.sh exits 2 when a valid certificate is already there and not due for renewal.
	if err != nil && code != 2 {
		return common.NewErrorf("acme.sh --issue failed (%v); the log below says why", err)
	}
	certFile, keyFile := AcmeCertPaths(domain)
	if err := os.MkdirAll(filepath.Dir(certFile), 0o755); err != nil {
		return err
	}
	setIssueStep("installCert", "")
	out, _, err = run(2*time.Minute, acme, "--install-cert", "-d", domain, "--ecc",
		"--key-file", keyFile, "--fullchain-file", certFile,
		"--reloadcmd", "command -v x-ui >/dev/null 2>&1 && x-ui restart || true")
	setIssueStep("installCert", out)
	if err != nil {
		return common.NewErrorf("acme.sh --install-cert: %v", err)
	}
	_ = os.Chmod(keyFile, 0o600)
	if err := s.settings.setString("acmeDomain", domain); err != nil {
		return err
	}
	if usePanel {
		if err := s.settings.SetCertFile(certFile); err != nil {
			return err
		}
		return s.settings.SetKeyFile(keyFile)
	}
	return nil
}
