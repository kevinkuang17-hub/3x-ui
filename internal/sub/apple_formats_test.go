package sub

import (
	"strings"
	"testing"
)

func appleSampleProxies() []map[string]any {
	return []map[string]any{
		{
			"name": "HK A", "type": "vmess", "server": "hk.example.com", "port": 443, "uuid": "u-1", "tls": true, "servername": "hk.example.com",
			"network": "ws", "ws-opts": map[string]any{"path": "/ws", "headers": map[string]any{"Host": "cdn.example.com"}},
		},
		{"name": "US A", "type": "trojan", "server": "us.example.com", "port": 443, "password": "pw", "sni": "us.example.com"},
		{
			"name": "JP A", "type": "vless", "server": "jp.example.com", "port": 443, "uuid": "u-2", "tls": true, "flow": "xtls-rprx-vision",
			"servername": "www.apple.com", "reality-opts": map[string]any{"public-key": "PBK", "short-id": "ab12"},
		},
		{"name": "SG A", "type": "ss", "server": "sg.example.com", "port": 8388, "cipher": "aes-128-gcm", "password": "sspw"},
		{"name": "gRPC", "type": "vless", "server": "x.example.com", "port": 443, "uuid": "u-3", "network": "grpc"},
		{"name": "WG", "type": "wireguard", "server": "w.example.com", "port": 51820},
	}
}

func TestRenderSurgeProfile(t *testing.T) {
	out := renderAppleProfile(formatSurge, appleSampleProxies(), "https://sub.example.com/surge/abc", 12)
	for _, want := range []string{
		"#!MANAGED-CONFIG https://sub.example.com/surge/abc interval=43200",
		`HK A = vmess, hk.example.com, 443, username=u-1, vmess-aead=true, ws=true, ws-path=/ws, ws-headers=Host:"cdn.example.com", tls=true, sni=hk.example.com, skip-cert-verify=false`,
		"US A = trojan, us.example.com, 443, password=pw, sni=us.example.com, skip-cert-verify=false",
		"SG A = ss, sg.example.com, 8388, encrypt-method=aes-128-gcm, password=sspw, udp-relay=true",
		"Proxy = select, HK A, US A, SG A",
		"FINAL,Proxy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("surge profile missing %q\n%s", want, out)
		}
	}
	for _, absent := range []string{"JP A =", "gRPC", "WG"} {
		if strings.Contains(out, absent) {
			t.Errorf("surge cannot express %q but it was emitted", absent)
		}
	}
}

func TestRenderLoonProfile(t *testing.T) {
	out := renderAppleProfile(formatLoon, appleSampleProxies(), "", 0)
	for _, want := range []string{
		`HK A = vmess,hk.example.com,443,auto,"u-1",transport=ws,path=/ws,host=cdn.example.com,over-tls=true,tls-name=hk.example.com,skip-cert-verify=false`,
		`JP A = VLESS,jp.example.com,443,"u-2",transport=tcp,over-tls=true,tls-name=www.apple.com,skip-cert-verify=false,flow=xtls-rprx-vision,public-key=PBK,short-id=ab12`,
		`SG A = Shadowsocks,sg.example.com,8388,aes-128-gcm,"sspw",udp=true`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("loon profile missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "gRPC") {
		t.Error("grpc must be skipped")
	}
}

func TestRenderQuantumultXList(t *testing.T) {
	out := renderAppleProfile(formatQX, appleSampleProxies(), "", 12)
	for _, want := range []string{
		"vmess=hk.example.com:443, method=chacha20-poly1305, password=u-1, obfs=wss, obfs-uri=/ws, obfs-host=hk.example.com, tls-verification=true, tag=HK A",
		"trojan=us.example.com:443, password=pw, over-tls=true, tls-host=us.example.com, tls-verification=true, tag=US A",
		"vless=jp.example.com:443, method=none, password=u-2, obfs=over-tls, obfs-host=www.apple.com, tls-verification=true, reality-base64-pubkey=PBK, reality-hex-shortid=ab12, vless-flow=xtls-rprx-vision, tag=JP A",
		"shadowsocks=sg.example.com:8388, method=aes-128-gcm, password=sspw, udp-relay=true, tag=SG A",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("qx list missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "[Proxy]") {
		t.Error("quantumult x gets a bare server list")
	}
}

func TestAppleNameSanitised(t *testing.T) {
	if got := appleName(" a,b=c\"d "); got != "a b-c'd" {
		t.Fatalf("got %q", got)
	}
}

func TestAppleFormatsSkipWhatTheyCannotExpress(t *testing.T) {
	proxies := []map[string]any{
		{"name": "PQ", "type": "vless", "server": "pq.example.com", "port": 443, "uuid": "u", "tls": true, "encryption": "mlkem768x25519plus.native.0rtt.abc"},
		{"name": "HY", "type": "hysteria2", "server": "hy.example.com", "port": 443, "password": "p", "obfs": "salamander", "obfs-password": "s"},
		{"name": "PIN", "type": "trojan", "server": "pin.example.com", "port": 443, "password": "p", "fingerprint": "abcd"},
		{"name": "V6", "type": "trojan", "server": "2001:db8::1", "port": 443, "password": "p"},
	}
	surge := renderAppleProfile(formatSurge, proxies, "", 12)
	if strings.Contains(surge, "HY =") || !strings.Contains(surge, "server-cert-fingerprint-sha256=abcd") {
		t.Fatalf("surge: obfuscated hysteria2 must be skipped and pins kept\n%s", surge)
	}
	loon := renderAppleProfile(formatLoon, proxies, "", 12)
	if strings.Contains(loon, "PQ =") || strings.Contains(loon, "PIN =") || !strings.Contains(loon, "salamander-password=s") {
		t.Fatalf("loon output wrong\n%s", loon)
	}
	qx := renderAppleProfile(formatQX, proxies, "", 12)
	if strings.Contains(qx, "tag=PQ") || !strings.Contains(qx, "trojan=[2001:db8::1]:443") {
		t.Fatalf("qx output wrong\n%s", qx)
	}
}
