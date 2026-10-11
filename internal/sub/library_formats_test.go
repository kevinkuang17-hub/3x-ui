package sub

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// seedLibrary gives every subscriber custom rules, one rule set and a company DNS.
func seedLibrary(t *testing.T) {
	t.Helper()
	rs := &service.RuleSetService{}
	set, err := rs.Save(model.RuleSet{Name: "ships", Rules: "maersk.com\nGEOSITE,openai\nGEOIP,telegram,no-resolve", DefaultPolicy: "PROXY"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.SaveGlobalRouting(service.RoutingChoice{
		RuleSets:   []service.RuleSetPick{{Id: set.Id, Policy: "🇭🇰 香港节点"}},
		ExtraRules: "bank.example,DIRECT\nads.example,REJECT",
	}); err != nil {
		t.Fatal(err)
	}
	if err := rs.SaveGlobalDns(service.DnsConfig{Enable: true, Company: "kstar.com = 192.168.8.10", CompanyDirect: true}); err != nil {
		t.Fatal(err)
	}
}

func writeSample(t *testing.T, name, body string) {
	t.Helper()
	if dir := os.Getenv("FORMAT_SAMPLES_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJsonSubscriptionCarriesLibrary(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-x", "x-in", 20101, 0, `{"network":"tcp","security":"none"}`)
	seedLibrary(t)
	out, _, err := NewSubJsonService("", "", "", "", NewSubService("")).GetJson("sub-x", "sub.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	writeSample(t, "xray.json", out)
	var doc struct {
		DNS struct {
			Servers []any `json:"servers"`
		} `json:"dns"`
		Routing struct {
			Rules []map[string]any `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	rules := doc.Routing.Rules
	if len(rules) < 5 {
		t.Fatalf("too few rules: %+v", rules)
	}
	want := []struct {
		field, entry, target string
	}{
		{"ip", "192.168.8.10", "direct"},
		{"domain", "domain:kstar.com", "direct"},
		{"domain", "domain:ads.example", "block"},
		{"domain", "domain:maersk.com", "proxy"},
	}
	for i, w := range want {
		list, _ := rules[i][w.field].([]any)
		if len(list) == 0 || list[0] != w.entry || rules[i]["outboundTag"] != w.target {
			t.Fatalf("rule %d = %+v, want %s %s -> %s", i, rules[i], w.field, w.entry, w.target)
		}
	}
	if list, _ := rules[1]["domain"].([]any); len(list) != 2 || list[1] != "domain:bank.example" {
		t.Fatalf("consecutive rules with one target must share a rule: %+v", rules[1])
	}
	if list, _ := rules[3]["domain"].([]any); len(list) != 2 || list[1] != "geosite:openai" {
		t.Fatalf("a template group becomes the proxy outbound: %+v", rules[3])
	}
	first, _ := doc.DNS.Servers[0].(map[string]any)
	if first["address"] != "192.168.8.10" || first["domains"].([]any)[0] != "domain:kstar.com" {
		t.Fatalf("the company DNS must answer for its domains: %+v", doc.DNS.Servers)
	}
}

func TestHappRoutingFromLibrary(t *testing.T) {
	seedSubDB(t)
	seedLibrary(t)
	lib, dns := libraryRulesForSub("anyone")
	link := happRoutingFromLibrary(lib, dns)
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "happ://routing/onadd/"))
	if err != nil {
		t.Fatalf("link %q: %v", link, err)
	}
	var spec map[string]any
	if err := json.Unmarshal(payload, &spec); err != nil {
		t.Fatal(err)
	}
	direct := strings.Join(strList(spec["DirectSites"]), " ")
	if !strings.Contains(direct, "domain:bank.example") || !strings.Contains(direct, "geosite:cn") {
		t.Fatalf("DirectSites = %v", direct)
	}
	if strings.Join(strList(spec["BlockSites"]), " ") != "domain:ads.example" || !strings.Contains(strings.Join(strList(spec["ProxySites"]), " "), "domain:maersk.com") {
		t.Fatalf("spec = %v", spec)
	}
	if !strings.Contains(strings.Join(strList(spec["DirectIp"]), " "), "192.168.8.10") {
		t.Fatal("the company resolver must be reached directly")
	}
}

func TestShadowrocketConfig(t *testing.T) {
	dns := appleDNSFor(service.DnsConfig{Enable: true, Company: "kstar.com = 192.168.8.10", CompanyDirect: true})
	rules := append(dns.direct, "DOMAIN-SUFFIX,maersk.com,🇭🇰 香港节点", "GEOSITE,openai,PROXY", "GEOIP,TELEGRAM,PROXY,no-resolve")
	out := renderShadowrocket("https://sub.example.com/shadowrocket/x", 6, rules, dns)
	for _, want := range []string{
		"DOMAIN-SUFFIX,kstar.com,DIRECT\nDOMAIN-SUFFIX,maersk.com,PROXY\nGEOIP,TELEGRAM,PROXY,no-resolve\nGEOIP,CN,DIRECT\nFINAL,PROXY",
		"kstar.com = server:192.168.8.10",
		"update-url = https://sub.example.com/shadowrocket/x",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Shadowrocket config lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "GEOSITE") {
		t.Fatal("Shadowrocket has no GEOSITE rules; they must be left out")
	}
}

func TestRenderSingbox(t *testing.T) {
	proxies := []map[string]any{
		{"name": "HK A", "type": "vless", "server": "hk.example.com", "port": uint64(443), "uuid": "u1", "flow": "xtls-rprx-vision",
			"network": "tcp", "tls": true, "servername": "www.apple.com", "client-fingerprint": "chrome",
			"reality-opts": map[string]any{"public-key": "pk", "short-id": "ab"}},
		{"name": "US A", "type": "vmess", "server": "us.example.com", "port": 8443, "uuid": "u2", "cipher": "auto",
			"network": "ws", "tls": true, "servername": "us.example.com", "ws-opts": map[string]any{"path": "/ws", "headers": map[string]any{"Host": "cdn.example.com"}}},
		{"name": "JP A", "type": "trojan", "server": "jp.example.com", "port": 443, "password": "pw", "network": "grpc", "tls": true,
			"sni": "jp.example.com", "grpc-opts": map[string]any{"grpc-service-name": "svc"}},
		{"name": "SG A", "type": "ss", "server": "sg.example.com", "port": 8388, "cipher": "2022-blake3-aes-128-gcm", "password": "a:b"},
		{"name": "KR A", "type": "hysteria2", "server": "kr.example.com", "port": 443, "password": "pw", "sni": "kr.example.com",
			"obfs": "salamander", "obfs-password": "ob", "ports": "20000-30000"},
		{"name": "TW A", "type": "tuic", "server": "tw.example.com", "port": 443, "uuid": "u3", "password": "pw",
			"congestion-controller": "bbr", "alpn": []any{"h3"}},
		{"name": "XH", "type": "vless", "server": "x.example.com", "port": 443, "uuid": "u4", "network": "xhttp", "tls": true},
		{"name": "WG", "type": "wireguard", "server": "w.example.com", "port": 51820},
	}
	rules := []libraryRule{
		{"DOMAIN-SUFFIX", "kstar.com", "DIRECT"},
		{"DOMAIN-SUFFIX", "ads.example", "REJECT"},
		{"DOMAIN-SUFFIX", "maersk.com", "🇭🇰 香港节点"},
		{"GEOSITE", "openai", "PROXY"},
	}
	out := renderSingbox(proxies, rules, service.DnsConfig{Enable: true, Company: "kstar.com = 192.168.8.10"})
	writeSample(t, "singbox.json", out)
	var doc struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules   []map[string]any `json:"rules"`
			RuleSet []map[string]any `json:"rule_set"`
		} `json:"route"`
		DNS struct {
			Rules []map[string]any `json:"rules"`
		} `json:"dns"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	tags := map[string]map[string]any{}
	for _, o := range doc.Outbounds {
		tags[o["tag"].(string)] = o
	}
	if _, ok := tags["XH"]; ok {
		t.Fatal("xhttp has no sing-box client and must be left out")
	}
	if _, ok := tags["WG"]; ok {
		t.Fatal("WireGuard is an endpoint in sing-box, not an outbound")
	}
	hk := tags["HK A"]
	tls, _ := hk["tls"].(map[string]any)
	if hk["server_port"] != float64(443) || tls["reality"].(map[string]any)["public_key"] != "pk" || tls["utls"] == nil {
		t.Fatalf("REALITY node = %+v", hk)
	}
	if tags["US A"]["transport"].(map[string]any)["path"] != "/ws" || tags["KR A"]["server_ports"].([]any)[0] != "20000:30000" {
		t.Fatalf("transports = %+v / %+v", tags["US A"], tags["KR A"])
	}
	sel := tags["proxy"]["outbounds"].([]any)
	if len(sel) != 7 || sel[0] != "auto" {
		t.Fatalf("selector = %v", sel)
	}
	var sawReject, sawRuleSet bool
	for _, r := range doc.Route.Rules {
		if r["action"] == "reject" {
			sawReject = true
		}
		if list, ok := r["rule_set"].([]any); ok && list[0] == "geosite-openai" && r["outbound"] == "proxy" {
			sawRuleSet = true
		}
	}
	if !sawReject || !sawRuleSet || len(doc.Route.RuleSet) != 4 {
		t.Fatalf("route = %+v", doc.Route)
	}
	if doc.DNS.Rules[0]["server"] != "dns-company-1" {
		t.Fatalf("company DNS must come first: %+v", doc.DNS.Rules)
	}
}
