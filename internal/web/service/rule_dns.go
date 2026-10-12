package service

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// DnsConfig is the DNS a Clash/Mihomo profile gets: domestic and foreign resolvers plus company
// rules that send a company's own domains to its internal DNS servers.
type DnsConfig struct {
	Enable bool   `json:"enable"`
	Mode   string `json:"mode"`
	IPv6   bool   `json:"ipv6"`
	// Domestic resolves everything not covered below; Foreign resolves non-mainland domains.
	Domestic        []string `json:"domestic"`
	Foreign         []string `json:"foreign"`
	ForeignViaProxy bool     `json:"foreignViaProxy"`
	// Company holds lines "corp.com, corp.local = 10.0.0.53, 10.0.0.54".
	Company       string `json:"company"`
	CompanyDirect bool   `json:"companyDirect"`
}

var (
	defaultDomesticDNS = []string{"https://223.5.5.5/dns-query", "https://doh.pub/dns-query"}
	defaultForeignDNS  = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}
	defaultBootstrap   = []string{"223.5.5.5", "119.29.29.29"}
	baseFakeIPFilter   = []string{"*.lan", "*.local", "*.localdomain", "+.msftconnecttest.com", "+.msftncsi.com", "time.*.com", "ntp.*.com", "+.stun.*.*"}
)

// CompanyDNSRule is one parsed company line: these domains are resolved by these servers.
type CompanyDNSRule struct {
	Domains []string
	Servers []string
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '，' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func validDNSServer(s string) bool {
	if strings.ContainsAny(s, " ,\r\n") || s == "" {
		return false
	}
	for _, prefix := range []string{"https://", "tls://", "quic://", "tcp://", "udp://", "dhcp://"} {
		if strings.HasPrefix(s, prefix) {
			return len(s) > len(prefix)
		}
	}
	if s == "system" {
		return true
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(host) != nil
	}
	return net.ParseIP(s) != nil
}

// ParseCompanyDNS reads the company lines; a domain covers its subdomains.
func ParseCompanyDNS(raw string) ([]CompanyDNSRule, error) {
	var rules []CompanyDNSRule
	for i, line := range ruleLines(raw) {
		left, right, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("company DNS line %d: write \"corp.com = 10.0.0.53\"", i+1)
		}
		rule := CompanyDNSRule{}
		for _, d := range splitList(left) {
			d = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(d, "+."), "*."))
			if !domainPattern.MatchString(d) {
				return nil, fmt.Errorf("company DNS line %d: not a domain: %q", i+1, d)
			}
			rule.Domains = append(rule.Domains, d)
		}
		for _, s := range splitList(right) {
			if !validDNSServer(s) {
				return nil, fmt.Errorf("company DNS line %d: not a DNS server: %q", i+1, s)
			}
			rule.Servers = append(rule.Servers, s)
		}
		if len(rule.Domains) == 0 || len(rule.Servers) == 0 {
			return nil, fmt.Errorf("company DNS line %d: needs domains and servers", i+1)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// NormalizeDns validates a DNS config and returns its storable JSON.
func NormalizeDns(cfg DnsConfig) (string, error) {
	switch cfg.Mode {
	case "", "fake-ip", "redir-host":
	default:
		return "", common.NewError("DNS mode must be fake-ip or redir-host")
	}
	for _, list := range [][]string{cfg.Domestic, cfg.Foreign} {
		for _, s := range list {
			if !validDNSServer(strings.TrimSpace(s)) {
				return "", common.NewError("not a DNS server:", s)
			}
		}
	}
	if _, err := ParseCompanyDNS(cfg.Company); err != nil {
		return "", common.NewError(err.Error())
	}
	b, err := json.Marshal(cfg)
	return string(b), err
}

func ParseDns(raw string) *DnsConfig {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var cfg DnsConfig
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return nil
	}
	return &cfg
}

func (s *RuleSetService) GlobalDns() DnsConfig {
	raw, _ := s.settingService.getString("subClashDns")
	if cfg := ParseDns(raw); cfg != nil {
		return *cfg
	}
	return DnsConfig{Mode: "fake-ip", CompanyDirect: true}
}

func (s *RuleSetService) SaveGlobalDns(cfg DnsConfig) error {
	raw, err := NormalizeDns(cfg)
	if err != nil {
		return err
	}
	return s.settingService.setString("subClashDns", raw)
}

// SetGroupDns gives a client group its own DNS; nil makes it follow the global DNS again.
func (s *ClientService) SetGroupDns(name string, cfg *DnsConfig) error {
	name = strings.TrimSpace(name)
	raw := ""
	if cfg != nil {
		var err error
		if raw, err = NormalizeDns(*cfg); err != nil {
			return err
		}
	}
	db := database.GetDB()
	var count int64
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		if err := db.Create(&model.ClientGroup{Name: name}).Error; err != nil {
			return err
		}
	}
	return db.Model(&model.ClientGroup{}).Where("name = ?", name).Update("clash_dns", raw).Error
}

// DnsForSub is the DNS of the subscriber's group, or the global one when the group has none.
func DnsForSub(subID string) DnsConfig {
	if g := RoutingBySubID(subID); g.Dns != nil {
		return *g.Dns
	}
	return (&RuleSetService{}).GlobalDns()
}

func orDefault(list, def []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), def...)
	}
	return out
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

// Servers returns the domestic and foreign resolvers with the defaults filled in, so every client
// format resolves the same way even when the admin left the lists empty.
func (cfg DnsConfig) Servers() (domestic, foreign []string) {
	return orDefault(cfg.Domestic, defaultDomesticDNS), orDefault(cfg.Foreign, defaultForeignDNS)
}

// BuildClashDNS turns the config into a Mihomo dns section plus DIRECT rules for company domains.
// proxyGroup is the profile's main proxy group, used when foreign DNS should go through the proxy.
func BuildClashDNS(cfg DnsConfig, proxyGroup string) (map[string]any, []string) {
	if !cfg.Enable {
		return nil, nil
	}
	company, _ := ParseCompanyDNS(cfg.Company)
	domestic := orDefault(cfg.Domestic, defaultDomesticDNS)
	foreign := orDefault(cfg.Foreign, defaultForeignDNS)
	bootstrap := []string{}
	for _, s := range domestic {
		if net.ParseIP(s) != nil {
			bootstrap = append(bootstrap, s)
		}
	}
	if len(bootstrap) == 0 {
		bootstrap = defaultBootstrap
	}
	policy := map[string]any{}
	filter := append([]string(nil), baseFakeIPFilter...)
	var direct []string
	for _, r := range company {
		for _, d := range r.Domains {
			policy["+."+d] = toAny(r.Servers)
			filter = append(filter, "+."+d)
			if cfg.CompanyDirect {
				direct = append(direct, "DOMAIN-SUFFIX,"+d+",DIRECT")
			}
		}
	}
	if cfg.ForeignViaProxy && proxyGroup != "" {
		for i, s := range foreign {
			if strings.Contains(s, "://") && !strings.Contains(s, "#") {
				foreign[i] = s + "#" + proxyGroup
			}
		}
	}
	policy["geosite:geolocation-!cn"] = toAny(foreign)
	mode := cfg.Mode
	if mode == "" {
		mode = "fake-ip"
	}
	dns := map[string]any{
		"enable":                  true,
		"ipv6":                    cfg.IPv6,
		"enhanced-mode":           mode,
		"default-nameserver":      toAny(bootstrap),
		"nameserver":              toAny(domestic),
		"proxy-server-nameserver": toAny(domestic),
		"nameserver-policy":       policy,
	}
	if mode == "fake-ip" {
		dns["fake-ip-range"] = "198.18.0.1/16"
		dns["fake-ip-filter"] = toAny(filter)
	}
	return dns, direct
}
