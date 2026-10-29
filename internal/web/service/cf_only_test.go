package service

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
)

const ufwDenyStatus = "Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\n\nTo                         Action      From\n--                         ------      ----\n22/tcp                     ALLOW IN    Anywhere\n"

// cfOnlyHarness fakes ufw (status output plus the rules it holds) and Cloudflare's range list.
type cfOnlyHarness struct {
	status string
	calls  []string
	ranges []string
	fetch  error
}

func newCfOnlyHarness(t *testing.T, status string, ranges ...string) *cfOnlyHarness {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	h := &cfOnlyHarness{status: status, ranges: ranges}
	oldRun, oldFetch := runFirewall, fetchCloudflareRanges
	t.Cleanup(func() { runFirewall, fetchCloudflareRanges = oldRun, oldFetch })
	runFirewall = func(_ context.Context, name string, args ...string) (string, error) {
		cmd := name + " " + strings.Join(args, " ")
		if len(args) > 0 && args[0] == "status" {
			return h.status, nil
		}
		h.calls = append(h.calls, cmd)
		return "Rule added", nil
	}
	fetchCloudflareRanges = func(context.Context) ([]string, error) { return h.ranges, h.fetch }
	return h
}

func (h *cfOnlyHarness) take() []string {
	c := h.calls
	h.calls = nil
	return c
}

func sameCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("ufw calls:\n  got  %q\n  want %q", got, want)
	}
}

var testCfRanges = []string{"173.245.48.0/20", "103.21.244.0/22", "104.16.0.0/13", "162.158.0.0/15", "172.64.0.0/13", "2606:4700::/32"}

// Cloudflare is let in before the open-to-everyone rule goes, so the port is never unreachable.
func TestEnableCfOnlyReplacesTheOpenRule(t *testing.T) {
	h := newCfOnlyHarness(t, ufwDenyStatus+"2053/tcp                   ALLOW IN    Anywhere\n2053/tcp (v6)              ALLOW IN    Anywhere (v6)\n", testCfRanges...)
	if err := (&ProtectService{}).EnableCfOnly(2053, false, true); err != nil {
		t.Fatal(err)
	}
	want := []string{}
	for _, r := range testCfRanges {
		want = append(want, "ufw allow proto tcp from "+r+" to any port 2053")
	}
	want = append(want, "ufw delete allow 2053/tcp")
	sameCalls(t, h.take(), want)
	if st := (&ProtectService{}).CfOnly(); !slices.Equal(st.Ports, []int{2053}) || st.Ranges != len(testCfRanges) {
		t.Fatalf("status = %+v", st)
	}
}

// x-ui's own menu opens a port for both protocols; its UDP half (Hysteria2 on 443) must survive.
func TestEnableCfOnlyKeepsUDPOfABothProtocolsRule(t *testing.T) {
	h := newCfOnlyHarness(t, ufwDenyStatus+"443                        ALLOW IN    Anywhere\n", testCfRanges[:5]...)
	if err := (&ProtectService{}).EnableCfOnly(443, true, false); err != nil {
		t.Fatal(err)
	}
	calls := h.take()
	sameCalls(t, calls[len(calls)-2:], []string{"ufw delete allow 443", "ufw allow 443/udp"})
}

func TestEnableCfOnlyRefusals(t *testing.T) {
	cases := []struct {
		name, status string
		ranges       []string
		fetch        error
		panel, viaCf bool
		want         error
	}{
		{"ufw off filters nothing", "Status: inactive\n", testCfRanges, nil, false, true, errCfOnlyNoUfw},
		{"default allow lets everyone in anyway", "Status: active\nDefault: allow (incoming), allow (outgoing)\n", testCfRanges, nil, false, true, errCfOnlyDefaultAllow},
		{"panel opened by IP would lock its admin out", ufwDenyStatus, testCfRanges, nil, true, false, errCfOnlyNotViaCloudflare},
		{"no range list", ufwDenyStatus, nil, errors.New("timeout"), false, true, errCfOnlyRanges},
		{"junk instead of ranges", ufwDenyStatus, []string{"1.2.3.4/24; ufw disable", "a", "b", "c", "d"}, nil, false, true, errCfOnlyRanges},
		{"suspiciously short list", ufwDenyStatus, []string{"104.16.0.0/13"}, nil, false, true, errCfOnlyRanges},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newCfOnlyHarness(t, c.status, c.ranges...)
			h.fetch = c.fetch
			err := (&ProtectService{}).EnableCfOnly(2053, c.viaCf, c.panel)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			sameCalls(t, h.take(), nil)
			if st := (&ProtectService{}).CfOnly(); len(st.Ports) != 0 {
				t.Fatalf("a refused port must not be recorded, status = %+v", st)
			}
		})
	}
}

// Turning it off reopens the port before the Cloudflare-only rules go, again without a gap.
func TestDisableCfOnlyReopensThePort(t *testing.T) {
	h := newCfOnlyHarness(t, ufwDenyStatus, testCfRanges[:5]...)
	svc := &ProtectService{}
	if err := svc.EnableCfOnly(2096, true, false); err != nil {
		t.Fatal(err)
	}
	h.take()
	if err := svc.DisableCfOnly(2096); err != nil {
		t.Fatal(err)
	}
	want := []string{"ufw allow 2096/tcp"}
	for _, r := range testCfRanges[:5] {
		want = append(want, "ufw delete allow proto tcp from "+r+" to any port 2096")
	}
	sameCalls(t, h.take(), want)
	if st := svc.CfOnly(); len(st.Ports) != 0 || st.Ranges != 0 {
		t.Fatalf("status = %+v", st)
	}
}

// Cloudflare adds and retires edge ranges; the daily refresh follows on every protected port.
func TestRefreshCfOnlyFollowsTheRangeList(t *testing.T) {
	h := newCfOnlyHarness(t, ufwDenyStatus, testCfRanges[:5]...)
	svc := &ProtectService{}
	for _, p := range []int{2053, 2096} {
		if err := svc.EnableCfOnly(p, true, false); err != nil {
			t.Fatal(err)
		}
	}
	h.take()
	h.ranges = append(slices.Clone(testCfRanges[1:5]), "131.0.72.0/22")
	if err := svc.RefreshCfOnly(); err != nil {
		t.Fatal(err)
	}
	sameCalls(t, h.take(), []string{
		"ufw allow proto tcp from 131.0.72.0/22 to any port 2053",
		"ufw allow proto tcp from 131.0.72.0/22 to any port 2096",
		"ufw delete allow proto tcp from 173.245.48.0/20 to any port 2053",
		"ufw delete allow proto tcp from 173.245.48.0/20 to any port 2096",
	})

	// A failed download keeps the rules in place rather than dropping Cloudflare.
	h.fetch = errors.New("timeout")
	if err := svc.RefreshCfOnly(); !errors.Is(err, errCfOnlyRanges) {
		t.Fatalf("err = %v", err)
	}
	sameCalls(t, h.take(), nil)
}

// "Open all inbound ports" must not undo Cloudflare-only on a port a TCP inbound also names.
func TestAllowPortsSkipsCfOnlyTCP(t *testing.T) {
	h := newCfOnlyHarness(t, ufwDenyStatus, testCfRanges[:5]...)
	svc := &ProtectService{}
	if err := svc.EnableCfOnly(443, true, false); err != nil {
		t.Fatal(err)
	}
	h.take()
	done, err := svc.AllowPorts([]FirewallRule{{443, "tcp"}, {443, "udp"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(done, []FirewallRule{{443, "udp"}}) {
		t.Fatalf("done = %v", done)
	}
	sameCalls(t, h.take(), []string{"ufw allow 443/udp"})
}
