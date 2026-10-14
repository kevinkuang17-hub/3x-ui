package service

import (
	"context"
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
