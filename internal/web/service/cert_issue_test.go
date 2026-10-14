package service

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
)

type acmeCall struct {
	name string
	args []string
	env  []string
}

// stubAcme answers acme.sh calls; issueCode is the exit code of --issue, and --install-cert
// writes a real certificate for the domain, as acme.sh would.
func stubAcme(t *testing.T, issueCode int) *[]acmeCall {
	t.Helper()
	oldRun, oldRoot := runAcme, certRoot
	t.Cleanup(func() { runAcme, certRoot = oldRun, oldRoot })
	certRoot = t.TempDir()
	calls := &[]acmeCall{}
	runAcme = func(_ context.Context, env []string, name string, args ...string) (string, int, error) {
		*calls = append(*calls, acmeCall{name, args, env})
		switch {
		case slices.Contains(args, "--issue"):
			if issueCode != 0 {
				return "Error add txt for domain", issueCode, errors.New("exit status")
			}
			return "Cert success.", 0, nil
		case slices.Contains(args, "--install-cert"):
			domain := args[slices.Index(args, "-d")+1]
			writeTestCert(t, filepath.Join(certRoot, domain), []string{domain}, time.Now().Add(80*24*time.Hour))
		}
		return "", 0, nil
	}
	return calls
}

func waitIssue(t *testing.T, s *CertIssueService) CertIssueStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.Status(); !st.Running {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("certificate request never finished")
	return CertIssueStatus{}
}

func TestCertIssueRefusesWithoutDomainOrToken(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	stubAcme(t, 0)
	s := &CertIssueService{}
	if err := s.Start("not a domain", false, nil); err == nil || !strings.Contains(err.Error(), "not a domain name") {
		t.Fatalf("bad domain: err = %v", err)
	}
	if err := s.Start("hk.example.com", false, nil); err == nil || !strings.Contains(err.Error(), "Cloudflare API token") {
		t.Fatalf("no token: err = %v", err)
	}
}

// The issued certificate must be found by the one-click dialog even when the panel itself keeps
// plain HTTP, or every TLS preset would stay blocked as "needs a certificate".
func TestCertIssueInstallsAndFeedsRecommendation(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	calls := stubAcme(t, 0)
	settings := SettingService{}
	if err := settings.setString("cfApiToken", "cf-token"); err != nil {
		t.Fatal(err)
	}
	restarted := false
	s := &CertIssueService{}
	if err := s.Start("HK.example.com", false, func() { restarted = true }); err != nil {
		t.Fatal(err)
	}
	st := waitIssue(t, s)
	certFile, keyFile := AcmeCertPaths("hk.example.com")
	if !st.Done || st.Error != "" || st.CertFile != certFile || st.KeyFile != keyFile || st.Panel || restarted {
		t.Fatalf("status %+v restarted %v", st, restarted)
	}
	var issue acmeCall
	for _, c := range *calls {
		if slices.Contains(c.args, "--issue") {
			issue = c
		}
	}
	if !slices.Contains(issue.env, "CF_Token=cf-token") || strings.Join(issue.args, " ") != "--issue --dns dns_cf -d hk.example.com --keylength ec-256" {
		t.Fatalf("issue call %+v", issue)
	}
	if web, _ := settings.GetCertFile(); web != "" {
		t.Fatalf("panel certificate changed to %q without being asked", web)
	}

	stubRecommendNet(t, map[string][]string{"hk.example.com": {"203.0.113.5"}}, "203.0.113.5")
	got, err := (&InboundRecommendService{}).Recommend(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Domain != "hk.example.com" || got.CertState != CertStateOK || got.CertFile != certFile {
		t.Fatalf("recommendation domain/cert/file = %q/%q/%q", got.Domain, got.CertState, got.CertFile)
	}
}

func TestCertIssueForPanelSetsPanelCertAndRestarts(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	stubAcme(t, 2) // already issued and not due: still installed
	settings := SettingService{}
	if err := settings.setString("cfApiToken", "cf-token"); err != nil {
		t.Fatal(err)
	}
	restarted := false
	s := &CertIssueService{}
	if err := s.Start("hk.example.com", true, func() { restarted = true }); err != nil {
		t.Fatal(err)
	}
	st := waitIssue(t, s)
	certFile, _ := AcmeCertPaths("hk.example.com")
	if web, _ := settings.GetCertFile(); !st.Done || !st.Panel || !restarted || web != certFile {
		t.Fatalf("status %+v restarted %v webCertFile %q", st, restarted, web)
	}
}

func TestCertIssueFailureKeepsSettings(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	stubAcme(t, 1)
	settings := SettingService{}
	if err := settings.setString("cfApiToken", "cf-token"); err != nil {
		t.Fatal(err)
	}
	s := &CertIssueService{}
	if err := s.Start("hk.example.com", true, nil); err != nil {
		t.Fatal(err)
	}
	st := waitIssue(t, s)
	if st.Done || !strings.Contains(st.Error, "acme.sh --issue failed") || st.Log != "Error add txt for domain" {
		t.Fatalf("status %+v", st)
	}
	if web, _ := settings.GetCertFile(); web != "" {
		t.Fatalf("webCertFile = %q after a failed request", web)
	}
	if d, _ := settings.getString("acmeDomain"); d != "" {
		t.Fatalf("acmeDomain = %q after a failed request", d)
	}
}
