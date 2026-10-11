package sub

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// libraryRule is one rule-library line split into its parts, the common input of every client format.
type libraryRule struct {
	kind, value, policy string
}

// parseLibraryRule reads a built Clash rule ("TYPE,value,POLICY[,no-resolve]").
func parseLibraryRule(rule string) (libraryRule, bool) {
	parts := strings.Split(rule, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	i := len(parts) - 1
	for i >= 2 && (strings.EqualFold(parts[i], "no-resolve") || strings.EqualFold(parts[i], "src")) {
		i--
	}
	if i != 2 || parts[1] == "" || parts[2] == "" {
		return libraryRule{}, false
	}
	return libraryRule{kind: strings.ToUpper(parts[0]), value: parts[1], policy: parts[2]}, true
}

// libraryRulesForSub is everything a non-Clash format applies for a subscriber: company DIRECT
// rules first, then the custom and rule-library rules, in match order.
func libraryRulesForSub(subID string) ([]libraryRule, service.DnsConfig) {
	dns := service.DnsForSub(subID)
	rules, _ := (&service.RuleSetService{}).BuildRules(libraryChoiceForSub(subID))
	_, direct := service.BuildClashDNS(dns, "")
	var out []libraryRule
	for _, r := range append(direct, rules...) {
		if lr, ok := parseLibraryRule(r); ok {
			out = append(out, lr)
		}
	}
	return out, dns
}

// basicTarget folds every proxy group into "proxy": formats other than Clash have one proxy choice.
func basicTarget(policy string) string {
	switch strings.ToUpper(policy) {
	case "DIRECT":
		return "direct"
	case "REJECT", "REJECT-DROP":
		return "block"
	}
	return "proxy"
}

// xrayMatch maps a rule onto an Xray routing field and entry (domain/ip/port).
func xrayMatch(r libraryRule) (field, entry string, ok bool) {
	switch r.kind {
	case "DOMAIN-SUFFIX":
		return "domain", "domain:" + r.value, true
	case "DOMAIN":
		return "domain", "full:" + r.value, true
	case "DOMAIN-KEYWORD":
		return "domain", r.value, true
	case "DOMAIN-REGEX":
		return "domain", "regexp:" + r.value, true
	case "GEOSITE":
		return "domain", "geosite:" + strings.ToLower(r.value), true
	case "IP-CIDR", "IP-CIDR6":
		return "ip", r.value, true
	case "GEOIP":
		return "ip", "geoip:" + geoipCode(r.value), true
	case "DST-PORT":
		return "port", strings.ReplaceAll(r.value, "/", ","), true
	}
	return "", "", false
}

// geoipCode maps Mihomo's LAN/PRIVATE pseudo-codes onto the "private" list the other cores ship.
func geoipCode(v string) string {
	v = strings.ToLower(v)
	if v == "lan" {
		return "private"
	}
	return v
}

// dnsServerHost returns the IP and port of a plain DNS server entry ("10.0.0.53", "10.0.0.53:5353",
// "udp://10.0.0.53"); ok is false for DoH/DoT and names.
func dnsServerHost(s string) (string, int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "udp://")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), 53, true
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || net.ParseIP(host) == nil {
		return "", 0, false
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 || p > 65535 {
		return "", 0, false
	}
	return host, p, true
}

// xrayDNSServer turns a resolver entry into Xray's form; DoT has no Xray client and is skipped.
func xrayDNSServer(s string) (any, bool) {
	if ip, port, ok := dnsServerHost(s); ok {
		if port == 53 {
			return ip, true
		}
		return map[string]any{"address": ip, "port": port}, true
	}
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "tcp://") || strings.HasPrefix(s, "quic+local://") || s == "localhost" {
		return s, true
	}
	if s == "system" {
		return "localhost", true
	}
	return nil, false
}

// resolverIPs lists the literal IPs of resolvers (plain or DoH by IP); queries to them must not
// ride the proxy, or mainland lookups would leave the country.
func resolverIPs(list []string) []string {
	var ips []string
	for _, s := range list {
		if ip, _, ok := dnsServerHost(s); ok {
			ips = append(ips, ip)
			continue
		}
		if u, err := url.Parse(s); err == nil && net.ParseIP(u.Hostname()) != nil {
			ips = append(ips, u.Hostname())
		}
	}
	return ips
}

func xrayDNS(cfg service.DnsConfig) map[string]any {
	domestic, foreign := cfg.Servers()
	var servers []any
	company, _ := service.ParseCompanyDNS(cfg.Company)
	for _, r := range company {
		domains := make([]string, 0, len(r.Domains))
		for _, d := range r.Domains {
			domains = append(domains, "domain:"+d)
		}
		for _, s := range r.Servers {
			if ip, port, ok := dnsServerHost(s); ok {
				servers = append(servers, map[string]any{"address": ip, "port": port, "domains": domains, "skipFallback": true})
			}
		}
	}
	for _, s := range foreign {
		if v, ok := xrayDNSServer(s); ok {
			entry := map[string]any{"domains": []string{"geosite:geolocation-!cn"}, "skipFallback": true}
			if m, isMap := v.(map[string]any); isMap {
				entry["address"], entry["port"] = m["address"], m["port"]
			} else {
				entry["address"] = v
			}
			servers = append(servers, entry)
		}
	}
	for _, s := range domestic {
		if v, ok := xrayDNSServer(s); ok {
			servers = append(servers, v)
		}
	}
	strategy := "UseIPv4"
	if cfg.IPv6 {
		strategy = "UseIP"
	}
	return map[string]any{"tag": "dns_out", "queryStrategy": strategy, "servers": servers}
}

// companyDNSIPs lists the company resolvers, which must be reached directly, not through the proxy.
func companyDNSIPs(cfg service.DnsConfig) []string {
	if !cfg.Enable {
		return nil
	}
	company, _ := service.ParseCompanyDNS(cfg.Company)
	var ips []string
	for _, r := range company {
		for _, s := range r.Servers {
			if ip, _, ok := dnsServerHost(s); ok {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

// xrayRoutingRules groups consecutive rules with the same field and target into one Xray rule, which
// keeps the first-match order of the library.
func xrayRoutingRules(rules []libraryRule, cfg service.DnsConfig, balancer bool) []any {
	type key struct{ field, target string }
	var out []any
	var cur key
	var entries []string
	flush := func() {
		if len(entries) == 0 {
			return
		}
		rule := map[string]any{"type": "field"}
		if cur.field == "port" {
			rule["port"] = strings.Join(entries, ",")
		} else {
			rule[cur.field] = entries
		}
		if cur.target == "proxy" && balancer {
			rule["balancerTag"] = subBalancerTag
		} else {
			rule["outboundTag"] = cur.target
		}
		out = append(out, rule)
		entries = nil
	}
	add := func(k key, entry string) {
		if k != cur {
			flush()
			cur = k
		}
		entries = append(entries, entry)
	}
	direct := companyDNSIPs(cfg)
	if cfg.Enable {
		domestic, _ := cfg.Servers()
		direct = append(direct, resolverIPs(domestic)...)
	}
	for _, ip := range direct {
		add(key{"ip", "direct"}, ip)
	}
	for _, r := range rules {
		if field, entry, ok := xrayMatch(r); ok {
			add(key{field, basicTarget(r.policy)}, entry)
		}
	}
	flush()
	return out
}

// applyLibraryToXrayConfigs puts the library rules in front of every config's routing and, when
// DNS is managed, replaces its dns block.
func applyLibraryToXrayConfigs(configs []json_util.RawMessage, rules []libraryRule, cfg service.DnsConfig, keepDNS bool) []json_util.RawMessage {
	if len(rules) == 0 && !cfg.Enable {
		return configs
	}
	out := make([]json_util.RawMessage, 0, len(configs))
	for _, raw := range configs {
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil || doc == nil {
			out = append(out, raw)
			continue
		}
		routing, _ := doc["routing"].(map[string]any)
		if routing == nil {
			routing = map[string]any{}
		}
		balancers, _ := routing["balancers"].([]any)
		added := xrayRoutingRules(rules, cfg, len(balancers) > 0)
		existing, _ := routing["rules"].([]any)
		routing["rules"] = append(added, existing...)
		doc["routing"] = routing
		// A DNS block set for the JSON subscription itself is an explicit choice and stays.
		if cfg.Enable && !keepDNS {
			doc["dns"] = xrayDNS(cfg)
		}
		patched, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			out = append(out, raw)
			continue
		}
		out = append(out, json_util.RawMessage(patched))
	}
	return out
}

// happRoutingFromLibrary builds a Happ/INCY routing profile from the library; mainland sites go direct
// as in the one-click templates, since the profile replaces the app's own routing.
func happRoutingFromLibrary(rules []libraryRule, cfg service.DnsConfig) string {
	spec := map[string]any{
		"Name": "3x-ui", "GlobalProxy": "true", "DomainStrategy": "IPIfNonMatch", "RouteOrder": "block-direct-proxy",
	}
	lists := map[string][]string{}
	put := func(name, entry string) {
		lists[name] = append(lists[name], entry)
	}
	for _, ip := range companyDNSIPs(cfg) {
		put("DirectIp", ip)
	}
	for _, r := range rules {
		field, entry, ok := xrayMatch(r)
		if !ok || field == "port" {
			continue
		}
		prefix := map[string]string{"direct": "Direct", "block": "Block", "proxy": "Proxy"}[basicTarget(r.policy)]
		if field == "domain" {
			put(prefix+"Sites", entry)
		} else {
			put(prefix+"Ip", entry)
		}
	}
	put("DirectSites", "geosite:private")
	put("DirectSites", "geosite:cn")
	put("DirectIp", "geoip:private")
	put("DirectIp", "geoip:cn")
	for k, v := range lists {
		spec[k] = v
	}
	domestic, foreign := cfg.Servers()
	setDNS := func(list []string, prefix string) {
		for _, s := range list {
			if ip, port, ok := dnsServerHost(s); ok && port == 53 {
				spec[prefix+"DNSType"], spec[prefix+"DNSIP"] = "DoU", ip
				return
			}
			if u, err := url.Parse(s); err == nil && u.Scheme == "https" && u.Hostname() != "" {
				spec[prefix+"DNSType"], spec[prefix+"DNSDomain"] = "DoH", s
				if net.ParseIP(u.Hostname()) != nil {
					spec[prefix+"DNSIP"] = u.Hostname()
				}
				return
			}
		}
	}
	setDNS(domestic, "Domestic")
	setDNS(foreign, "Remote")
	body, err := json.Marshal(spec)
	if err != nil {
		return ""
	}
	link := "happ://routing/onadd/" + base64.StdEncoding.EncodeToString(body)
	// The profile travels in an HTTP header; one too large for it is left out rather than cut.
	if len(link) > remoteRoutingHappMaxValue {
		return ""
	}
	return link
}
