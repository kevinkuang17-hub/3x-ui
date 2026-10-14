package sub

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func sidecarInbound(protocol model.Protocol, serverName string) *model.Inbound {
	return &model.Inbound{
		Listen: "203.0.113.5", Port: 443, Protocol: protocol, Remark: "HK A",
		Settings:       `{"clients":[{"email":"ivy@hk","password":"p/w","enable":true}]}`,
		StreamSettings: `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"` + serverName + `"}}`,
	}
}

// The panel's QR codes (frontend/src/lib/xray/inbound-link.ts) emit the same strings.
func TestSidecarLinks(t *testing.T) {
	cases := []struct {
		name string
		in   *model.Inbound
		want string
	}{
		{
			"AnyTLS sends the certificate name as sni", sidecarInbound(model.AnyTLS, "hk.example.com"),
			"anytls://p%2Fw@203.0.113.5:443/?sni=hk.example.com#HK%20A-ivy%40hk",
		},
		{
			"AnyTLS on a bare IP sends no sni", sidecarInbound(model.AnyTLS, ""),
			"anytls://p%2Fw@203.0.113.5:443/#HK%20A-ivy%40hk",
		},
		{
			"NaiveProxy dials the certificate domain as the email", sidecarInbound(model.Naive, "hk.example.com"),
			"naive+https://ivy%40hk:p%2Fw@hk.example.com:443#HK%20A-ivy%40hk",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (&SubService{}).GetLink(tc.in, "ivy@hk"); got != tc.want {
				t.Fatalf("link\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A Mihomo anytls proxy becomes a sing-box anytls outbound with TLS on, not a skipped proxy.
func TestSingboxOutboundFromAnyTLS(t *testing.T) {
	out := singboxOutbound(map[string]any{
		"name": "HK A", "type": "anytls", "server": "203.0.113.5", "port": 443, "password": "pw", "sni": "hk.example.com",
	})
	tls, _ := out["tls"].(map[string]any)
	if out["type"] != "anytls" || out["password"] != "pw" || tls["enabled"] != true || tls["server_name"] != "hk.example.com" {
		t.Fatalf("outbound %v", out)
	}
}
