package service

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestBuildClashDNS(t *testing.T) {
	cfg := DnsConfig{
		Enable:          true,
		Company:         "kstar.com, hz.kstar.grp = 10.1.1.53, 10.1.1.54\n# note\ncorp.example = tls://10.2.0.1",
		CompanyDirect:   true,
		ForeignViaProxy: true,
	}
	dns, direct := BuildClashDNS(cfg, "🚀 手动选择")
	policy := dns["nameserver-policy"].(map[string]any)
	if got := policy["+.kstar.com"].([]any); len(got) != 2 || got[0] != "10.1.1.53" {
		t.Fatalf("company domain must use the company servers, got %v", got)
	}
	foreign := policy["geosite:geolocation-!cn"].([]any)
	if foreign[0] != "https://1.1.1.1/dns-query#🚀 手动选择" {
		t.Fatalf("foreign DoH must go through the proxy group, got %v", foreign)
	}
	if !slices.Contains(dns["fake-ip-filter"].([]any), any("+.hz.kstar.grp")) {
		t.Fatal("company domains must get real IPs, not fake ones")
	}
	if !slices.Equal(direct, []string{"DOMAIN-SUFFIX,kstar.com,DIRECT", "DOMAIN-SUFFIX,hz.kstar.grp,DIRECT", "DOMAIN-SUFFIX,corp.example,DIRECT"}) {
		t.Fatalf("direct = %v", direct)
	}
	again, _ := BuildClashDNS(DnsConfig{Enable: true}, "")
	if again["nameserver-policy"].(map[string]any)["geosite:geolocation-!cn"].([]any)[0] != "https://1.1.1.1/dns-query" {
		t.Fatal("the proxy suffix leaked into the shared defaults")
	}
	if off, _ := BuildClashDNS(DnsConfig{}, ""); off != nil {
		t.Fatal("a disabled DNS config must leave the profile's DNS alone")
	}
	for _, bad := range []string{"kstar.com 10.1.1.53", "kstar.com = not-a-server", "= 10.1.1.1"} {
		if _, err := NormalizeDns(DnsConfig{Company: bad}); err == nil {
			t.Errorf("company line %q must be rejected", bad)
		}
	}
}

func TestImportClashConfig(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	conf := `
dns:
  enhanced-mode: redir-host
  nameserver: [223.5.5.5, https://doh.pub/dns-query]
  fallback: [https://1.1.1.1/dns-query]
  nameserver-policy:
    "+.kstar.com,gm.kstar.grp": [192.168.8.10]
    "+.google.com": [https://8.8.8.8/dns-query]
    "geosite:cn": [223.5.5.5]
rules:
  - DOMAIN-SUFFIX,kstar.com,DIRECT
  - DOMAIN-SUFFIX,teams.microsoft.com,Teams
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - DOMAIN-KEYWORD,ads,REJECT
  - RULE-SET,proxy,🚀 节点选择
  - DOMAIN-SUFFIX,kstar.com,DIRECT
  - MATCH,🚀 节点选择
`
	svc := &RuleSetService{}
	res, err := svc.ImportClashConfig([]byte(conf), "office", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rules != 4 || res.Skipped != 2 || !res.Dns || strings.Join(res.Sets, "|") != "office · DIRECT|office · Teams|office · REJECT" {
		t.Fatalf("result = %+v", res)
	}
	var sets []model.RuleSet
	database.GetDB().Where("category = ?", "office").Order("id").Find(&sets)
	if len(sets) != 3 || sets[0].Rules != "DOMAIN-SUFFIX,kstar.com\nIP-CIDR,10.0.0.0/8,no-resolve" || sets[1].DefaultPolicy != "PROXY" || sets[2].DefaultPolicy != "REJECT" {
		t.Fatalf("sets = %+v", sets)
	}
	dns := svc.GlobalDns()
	if !dns.Enable || dns.Mode != "redir-host" || dns.Company != "kstar.com, gm.kstar.grp = 192.168.8.10" || dns.Foreign[0] != "https://1.1.1.1/dns-query" {
		t.Fatalf("imported DNS = %+v", dns)
	}
	if _, err := svc.ImportClashConfig([]byte("proxies: []"), "x", false); err == nil {
		t.Fatal("a config without usable rules must say so")
	}
}
