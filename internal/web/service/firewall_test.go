package service

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func fakeUfw(t *testing.T, status string) *[]string {
	t.Helper()
	var calls []string
	old := runFirewall
	t.Cleanup(func() { runFirewall = old })
	runFirewall = func(_ context.Context, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "status" {
			return status, nil
		}
		return "Rule added", nil
	}
	return &calls
}

// The x-ui menu's firewall opens 443 for TCP only, so a Hysteria2 inbound on 443 got no traffic.
func TestInboundFirewallRulesOpenUDPForHysteria(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	node := 2
	for _, ib := range []model.Inbound{
		{Tag: "hy2", Port: 443, Protocol: model.Hysteria, Enable: true, Settings: `{"clients":[]}`, StreamSettings: `{"network":"hysteria","security":"tls"}`},
		{Tag: "reality", Port: 8443, Protocol: model.VLESS, Enable: true, Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp","security":"reality"}`},
		{Tag: "ss", Port: 9000, Protocol: model.Shadowsocks, Enable: true, Settings: `{"network":"tcp,udp","clients":[]}`, StreamSettings: `{"network":"tcp"}`},
		{Tag: "off", Port: 9100, Protocol: model.VLESS, Enable: false, Settings: `{"clients":[]}`},
		{Tag: "local-only", Port: 9200, Listen: "127.0.0.1", Protocol: model.VLESS, Enable: true, Settings: `{"clients":[]}`},
		{Tag: "on-node", Port: 9300, Protocol: model.VLESS, Enable: true, NodeID: &node, Settings: `{"clients":[]}`},
	} {
		if err := database.GetDB().Create(&ib).Error; err != nil {
			t.Fatal(err)
		}
	}
	rules, err := InboundFirewallRules()
	if err != nil {
		t.Fatal(err)
	}
	want := []FirewallRule{{443, "udp"}, {8443, "tcp"}, {9000, "tcp"}, {9000, "udp"}}
	if len(rules) != len(want) {
		t.Fatalf("rules = %v, want %v", rules, want)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Fatalf("rules = %v, want %v", rules, want)
		}
	}
}

func TestAllowPortsRunsUfwOnlyWhenActive(t *testing.T) {
	fakeUfw(t, "Status: inactive\n")
	svc := &ProtectService{}
	if _, err := svc.AllowPorts([]FirewallRule{{443, "udp"}}); err == nil {
		t.Fatal("with ufw inactive there is nothing to open, and saying so beats a silent no-op")
	}
	calls := fakeUfw(t, "Status: active\n\nTo Action From\n")
	done, err := svc.AllowPorts([]FirewallRule{{443, "udp"}, {443, "udp"}, {8443, "tcp"}})
	if err != nil || len(done) != 2 {
		t.Fatalf("done = %v, %v", done, err)
	}
	if got := strings.Join(*calls, "; "); !strings.Contains(got, "ufw allow 443/udp") || !strings.Contains(got, "ufw allow 8443/tcp") {
		t.Fatalf("ufw calls = %s", got)
	}
	if _, err := svc.AllowPorts([]FirewallRule{{443, "sctp"}}); err == nil {
		t.Fatal("only tcp and udp rules may reach ufw")
	}
}

func TestUfwAllows(t *testing.T) {
	status := `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
443/tcp                    ALLOW IN    Anywhere
2053                       ALLOW IN    Anywhere
20000:30000/udp            ALLOW IN    Anywhere
443/tcp (v6)               ALLOW IN    Anywhere (v6)
`
	cases := []struct {
		port  int
		proto string
		want  bool
	}{
		{443, "tcp", true},
		{443, "udp", false}, // the case that leaves Hysteria2 on 443 without traffic
		{2053, "udp", true},
		{25000, "udp", true},
		{25000, "tcp", false},
		{8443, "tcp", false},
	}
	for _, tc := range cases {
		if got := ufwAllows(status, tc.port, tc.proto); got != tc.want {
			t.Errorf("ufwAllows(%d/%s) = %v, want %v", tc.port, tc.proto, got, tc.want)
		}
	}
	open := strings.Replace(status, "Default: deny (incoming)", "Default: allow (incoming)", 1)
	if !ufwAllows(open, 8443, "udp") {
		t.Error("an incoming default of allow lets every port in")
	}
}

// A port Xray holds reads as bound; one nobody holds means the inbound never came up.
func TestCheckPortSeesBoundPorts(t *testing.T) {
	fakeUfw(t, "Status: inactive\n")
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	svc := &ProtectService{}
	pc, err := svc.CheckPort(port, "tcp")
	if err != nil || pc.Bound == nil || !*pc.Bound || !pc.Allowed || pc.Firewall != "" {
		t.Fatalf("held port: %+v, %v", pc, err)
	}
	_ = l.Close()
	if pc, _ = svc.CheckPort(port, "tcp"); pc.Bound == nil || *pc.Bound {
		t.Fatalf("released port must read as free: %+v", pc)
	}
}
