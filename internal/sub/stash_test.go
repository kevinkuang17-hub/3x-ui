package sub

import (
	"strings"
	"testing"

	yaml "github.com/goccy/go-yaml"
)

const mihomoProfile = `mixed-port: 7890
sniffer:
  enable: true
proxies:
  - {name: US A Reality, type: vless, server: us.example.com, port: 443, network: tcp}
  - {name: US A XHTTP, type: vless, server: us.example.com, port: 8443, network: xhttp}
  - {name: US A Hy2, type: hysteria2, server: us.example.com, port: 443}
  - {name: US A AnyTLS, type: anytls, server: us.example.com, port: 9443}
proxy-groups:
  - {name: PROXY, type: select, proxies: [US A Reality, US A XHTTP, US A Hy2]}
  - {name: Only XHTTP, type: select, proxies: [US A XHTTP]}
rule-providers:
  ads: {type: http, behavior: domain, format: mrs, url: "https://example.com/ads.mrs"}
  cn: {type: http, behavior: domain, format: yaml, url: "https://example.com/cn.yaml"}
rules:
  - RULE-SET,ads,REJECT
  - RULE-SET,cn,DIRECT
  - DOMAIN-SUFFIX,bank.example,DIRECT
  - MATCH,PROXY
`

func TestRenderStash(t *testing.T) {
	out, err := renderStash(mihomoProfile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Sniffer       any              `yaml:"sniffer"`
		MixedPort     int              `yaml:"mixed-port"`
		Proxies       []map[string]any `yaml:"proxies"`
		Groups        []map[string]any `yaml:"proxy-groups"`
		RuleProviders map[string]any   `yaml:"rule-providers"`
		Rules         []string         `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Sniffer != nil || doc.MixedPort != 7890 {
		t.Fatalf("sniffer = %v, mixed-port = %d: Mihomo-only keys go, the rest stays", doc.Sniffer, doc.MixedPort)
	}
	var names []string
	for _, p := range doc.Proxies {
		names = append(names, p["name"].(string))
	}
	if strings.Join(names, ",") != "US A Reality,US A Hy2" {
		t.Fatalf("proxies = %v, want XHTTP and AnyTLS left out", names)
	}
	if got := doc.Groups[0]["proxies"].([]any); len(got) != 2 {
		t.Fatalf("PROXY members = %v", got)
	}
	if got := doc.Groups[1]["proxies"].([]any); len(got) != 1 || got[0] != "DIRECT" {
		t.Fatalf("an emptied group must fall back to DIRECT, got %v", got)
	}
	if _, ok := doc.RuleProviders["ads"]; ok {
		t.Fatal("the .mrs provider must be removed")
	}
	if strings.Join(doc.Rules, "|") != "RULE-SET,cn,DIRECT|DOMAIN-SUFFIX,bank.example,DIRECT|MATCH,PROXY" {
		t.Fatalf("rules = %v", doc.Rules)
	}
}
