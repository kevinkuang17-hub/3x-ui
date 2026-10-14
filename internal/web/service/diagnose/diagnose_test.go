package diagnose

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/outbound"
)

func boolPtr(v bool) *bool { return &v }

func port(proto string, bound, allowed bool) portFact {
	return portFact{proto: proto, check: &service.PortCheck{Port: 443, Proto: proto, Bound: boolPtr(bound), Allowed: allowed}}
}

func keys(fs []Finding) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Key
	}
	return out
}

func find(fs []Finding, key string) *Finding {
	for i := range fs {
		if fs[i].Key == key {
			return &fs[i]
		}
	}
	return nil
}

func TestAssess(t *testing.T) {
	failed := &outbound.TestOutboundResult{Error: "context deadline exceeded"}
	cases := []struct {
		name    string
		f       facts
		wantKey string
		wantFix string
		absent  string
		ok      bool
	}{
		{
			name:    "a disabled inbound explains everything",
			f:       facts{enabled: false, e2e: failed},
			wantKey: "disabled", wantFix: FixEnable, absent: "e2eFailed",
		},
		{
			name:    "an offline node explains everything",
			f:       facts{enabled: true, nodeOffline: true, nodeName: "US A"},
			wantKey: "nodeOffline", absent: "e2eFailed",
		},
		{
			// ufw lets 443/tcp in but not UDP, so Hysteria2 on 443 gets nothing.
			name:    "Hysteria2 blocked by ufw",
			f:       facts{enabled: true, ports: []portFact{port("udp", true, false)}, e2e: failed},
			wantKey: "firewallBlocked", wantFix: FixFirewall,
		},
		{
			name:    "Xray never bound the port",
			f:       facts{enabled: true, ports: []portFact{port("tcp", false, true)}, address: "us.example.com", addrPort: 443, tcpReach: boolPtr(false), e2e: failed},
			wantKey: "notListening", absent: "unreachable",
		},
		{
			// The server listens and lets traffic in, yet the panel cannot connect from outside.
			name:    "cloud security group or blocked IP",
			f:       facts{enabled: true, ports: []portFact{port("tcp", true, true)}, address: "us.example.com", addrPort: 443, tcpReach: boolPtr(false), e2e: failed},
			wantKey: "unreachable",
		},
		{
			name:    "only the proxied connection fails",
			f:       facts{enabled: true, ports: []portFact{port("tcp", true, true)}, address: "us.example.com", addrPort: 443, tcpReach: boolPtr(true), e2e: failed},
			wantKey: "e2eFailedOnly",
		},
		{
			name:    "no client to connect with",
			f:       facts{enabled: true, noClient: true},
			wantKey: "noClient", wantFix: FixClients,
		},
		{
			name:    "working inbound",
			f:       facts{enabled: true, ports: []portFact{port("tcp", true, true)}, address: "us.example.com", addrPort: 443, tcpReach: boolPtr(true), e2e: &outbound.TestOutboundResult{Success: true, Delay: 180}},
			wantKey: "e2eOk", ok: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := assess(tc.f)
			f := find(got, tc.wantKey)
			if f == nil {
				t.Fatalf("findings %v lack %q", keys(got), tc.wantKey)
			}
			if f.Fix != tc.wantFix {
				t.Errorf("fix = %q, want %q", f.Fix, tc.wantFix)
			}
			if tc.absent != "" && find(got, tc.absent) != nil {
				t.Errorf("findings %v must not include %q", keys(got), tc.absent)
			}
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
		})
	}
}

func TestOutboundEndpoint(t *testing.T) {
	vless := map[string]any{"settings": map[string]any{"vnext": []any{map[string]any{"address": "us.example.com", "port": float64(443)}}}}
	if h, p := outboundEndpoint(vless); h != "us.example.com" || p != 443 {
		t.Fatalf("vless endpoint = %s:%d", h, p)
	}
	hy := map[string]any{"settings": map[string]any{"address": "hk.example.com", "port": float64(8443)}}
	if h, p := outboundEndpoint(hy); h != "hk.example.com" || p != 8443 {
		t.Fatalf("hysteria endpoint = %s:%d", h, p)
	}
}
