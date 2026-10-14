package sub

import (
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// sing-box profiles target the 1.12 schema (typed DNS servers, rule actions, remote rule sets).
const (
	singboxGeositeURL = "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-%s.srs"
	singboxGeoipURL   = "https://raw.githubusercontent.com/SagerNet/sing-geoip/rule-set/geoip-%s.srs"
)

func strOf(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

func intOf(m map[string]any, key string) int {
	if s := strOf(m, key); s != "" {
		n, _ := strconv.Atoi(s)
		return n
	}
	return 0
}

func boolOf(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

func strList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	case string:
		return splitComma(t)
	}
	return nil
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// singboxTLS maps Clash TLS/REALITY fields; REALITY needs uTLS in sing-box.
func singboxTLS(p map[string]any, defaultSNI string) map[string]any {
	tls := map[string]any{"enabled": true}
	sni := strOf(p, "servername")
	if sni == "" {
		sni = strOf(p, "sni")
	}
	if sni == "" {
		sni = defaultSNI
	}
	if sni != "" {
		tls["server_name"] = sni
	}
	if boolOf(p, "skip-cert-verify") {
		tls["insecure"] = true
	}
	if alpn := strList(p["alpn"]); len(alpn) > 0 {
		tls["alpn"] = alpn
	}
	fp := strOf(p, "client-fingerprint")
	if ro, ok := p["reality-opts"].(map[string]any); ok {
		tls["reality"] = map[string]any{"enabled": true, "public_key": strOf(ro, "public-key"), "short_id": strOf(ro, "short-id")}
		if fp == "" {
			fp = "chrome"
		}
	}
	if fp != "" {
		tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	return tls
}

// singboxTransport maps the Clash network options; xhttp has no sing-box client and is refused.
func singboxTransport(p map[string]any) (map[string]any, bool) {
	switch strOf(p, "network") {
	case "", "tcp":
		return nil, true
	case "ws":
		t := map[string]any{"type": "ws"}
		if opts, ok := p["ws-opts"].(map[string]any); ok {
			if path := strOf(opts, "path"); path != "" {
				t["path"] = path
			}
			if headers, ok := opts["headers"].(map[string]any); ok && strOf(headers, "Host") != "" {
				t["headers"] = map[string]any{"Host": strOf(headers, "Host")}
			}
		}
		return t, true
	case "grpc":
		t := map[string]any{"type": "grpc"}
		if opts, ok := p["grpc-opts"].(map[string]any); ok && strOf(opts, "grpc-service-name") != "" {
			t["service_name"] = strOf(opts, "grpc-service-name")
		}
		return t, true
	case "httpupgrade":
		t := map[string]any{"type": "httpupgrade"}
		if opts, ok := p["http-upgrade-opts"].(map[string]any); ok {
			if path := strOf(opts, "path"); path != "" {
				t["path"] = path
			}
			if headers, ok := opts["headers"].(map[string]any); ok && strOf(headers, "Host") != "" {
				t["host"] = strOf(headers, "Host")
			}
		}
		return t, true
	}
	return nil, false
}

// singboxOutbound converts one Clash proxy; types sing-box cannot dial are skipped (nil).
func singboxOutbound(p map[string]any) map[string]any {
	name, server, port := strOf(p, "name"), strOf(p, "server"), intOf(p, "port")
	if name == "" || server == "" || port <= 0 {
		return nil
	}
	out := map[string]any{"tag": name, "server": server, "server_port": port}
	stream := true
	switch strOf(p, "type") {
	case "vless":
		if strOf(p, "encryption") != "" {
			return nil
		}
		out["type"], out["uuid"], out["packet_encoding"] = "vless", strOf(p, "uuid"), "xudp"
		if flow := strOf(p, "flow"); flow != "" {
			out["flow"] = flow
		}
	case "vmess":
		out["type"], out["uuid"], out["alter_id"] = "vmess", strOf(p, "uuid"), 0
		if c := strOf(p, "cipher"); c != "" {
			out["security"] = c
		}
	case "trojan":
		out["type"], out["password"] = "trojan", strOf(p, "password")
	case "ss":
		out["type"], out["method"], out["password"] = "shadowsocks", strOf(p, "cipher"), strOf(p, "password")
		stream = false
	case "hysteria2":
		out["type"], out["password"] = "hysteria2", strOf(p, "password")
		out["tls"] = singboxTLS(p, server)
		if strOf(p, "obfs") == "salamander" {
			out["obfs"] = map[string]any{"type": "salamander", "password": strOf(p, "obfs-password")}
		}
		if ports := strOf(p, "ports"); ports != "" {
			var ranges []string
			for _, r := range splitComma(ports) {
				if !strings.Contains(r, "-") {
					r += "-" + r
				}
				ranges = append(ranges, strings.ReplaceAll(r, "-", ":"))
			}
			out["server_ports"] = ranges
		}
		return out
	case "tuic":
		out["type"], out["uuid"], out["password"] = "tuic", strOf(p, "uuid"), strOf(p, "password")
		if cc := strOf(p, "congestion-controller"); cc != "" {
			out["congestion_control"] = cc
		}
		if mode := strOf(p, "udp-relay-mode"); mode != "" {
			out["udp_relay_mode"] = mode
		}
		if boolOf(p, "reduce-rtt") {
			out["zero_rtt_handshake"] = true
		}
		out["tls"] = singboxTLS(p, server)
		return out
	case "anytls":
		out["type"], out["password"] = "anytls", strOf(p, "password")
		out["tls"] = singboxTLS(p, server)
		return out
	case "socks5":
		out["type"] = "socks"
		if u := strOf(p, "username"); u != "" {
			out["username"], out["password"] = u, strOf(p, "password")
		}
		return out
	default:
		return nil
	}
	if !stream {
		return out
	}
	transport, ok := singboxTransport(p)
	if !ok {
		return nil
	}
	if transport != nil {
		out["transport"] = transport
	}
	if boolOf(p, "tls") {
		out["tls"] = singboxTLS(p, "")
	}
	return out
}

// singboxDNSServer turns a resolver entry into a typed 1.12 DNS server; names need the bootstrap.
func singboxDNSServer(tag, s string) map[string]any {
	if ip, port, ok := dnsServerHost(s); ok {
		srv := map[string]any{"type": "udp", "tag": tag, "server": ip}
		if port != 53 {
			srv["server_port"] = port
		}
		return srv
	}
	if s == "system" {
		return map[string]any{"type": "local", "tag": tag}
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	kind := map[string]string{"https": "https", "tls": "tls", "quic": "quic", "h3": "h3", "tcp": "tcp"}[u.Scheme]
	if kind == "" {
		return nil
	}
	srv := map[string]any{"type": kind, "tag": tag, "server": u.Hostname()}
	if p, err := strconv.Atoi(u.Port()); err == nil {
		srv["server_port"] = p
	}
	if kind == "https" && u.Path != "" && u.Path != "/dns-query" {
		srv["path"] = u.Path
	}
	if net.ParseIP(u.Hostname()) == nil {
		srv["domain_resolver"] = "dns-bootstrap"
	}
	return srv
}

type singboxRuleSets struct {
	tags []string
	sets []any
}

func newSingboxRuleSets() *singboxRuleSets {
	return &singboxRuleSets{sets: []any{}}
}

func (r *singboxRuleSets) use(kind, name string) string {
	name = strings.ToLower(name)
	tag := kind + "-" + name
	for _, t := range r.tags {
		if t == tag {
			return tag
		}
	}
	format := singboxGeositeURL
	if kind == "geoip" {
		format = singboxGeoipURL
	}
	r.tags = append(r.tags, tag)
	r.sets = append(r.sets, map[string]any{
		"type": "remote", "tag": tag, "format": "binary",
		"url": strings.Replace(format, "%s", name, 1), "download_detour": "proxy",
	})
	return tag
}

// singboxRouteRules groups consecutive rules with the same match field and action.
func singboxRouteRules(rules []libraryRule, sets *singboxRuleSets) []any {
	type key struct{ field, target string }
	var out []any
	var cur key
	var values []any
	flush := func() {
		if len(values) == 0 {
			return
		}
		out = append(out, singboxAction(map[string]any{cur.field: values}, cur.target))
		values = nil
	}
	push := func(k key, v any) {
		if k != cur {
			flush()
			cur = k
		}
		values = append(values, v)
	}
	for _, r := range rules {
		var field string
		var value any = r.value
		switch r.kind {
		case "DOMAIN-SUFFIX":
			field = "domain_suffix"
		case "DOMAIN":
			field = "domain"
		case "DOMAIN-KEYWORD":
			field = "domain_keyword"
		case "DOMAIN-REGEX":
			field = "domain_regex"
		case "IP-CIDR", "IP-CIDR6":
			field = "ip_cidr"
		case "GEOSITE":
			field, value = "rule_set", sets.use("geosite", r.value)
		case "GEOIP":
			if geoipCode(r.value) == "private" {
				flush()
				cur = key{}
				out = append(out, singboxAction(map[string]any{"ip_is_private": true}, basicTarget(r.policy)))
				continue
			}
			field, value = "rule_set", sets.use("geoip", r.value)
		case "PROCESS-NAME":
			field = "process_name"
		case "DST-PORT":
			// Mihomo writes port lists as 80/443 and ranges as 8000-9000.
			for _, token := range strings.FieldsFunc(r.value, func(c rune) bool { return c == '/' || c == ',' }) {
				pf, pv := "port", any(nil)
				if lo, hi, isRange := strings.Cut(token, "-"); isRange {
					pf, pv = "port_range", strings.TrimSpace(lo)+":"+strings.TrimSpace(hi)
				} else if n, err := strconv.Atoi(strings.TrimSpace(token)); err == nil {
					pv = n
				} else {
					continue
				}
				push(key{pf, basicTarget(r.policy)}, pv)
			}
			continue
		default:
			continue
		}
		push(key{field, basicTarget(r.policy)}, value)
	}
	flush()
	return out
}

func singboxAction(rule map[string]any, target string) map[string]any {
	if target == "block" {
		rule["action"] = "reject"
	} else {
		rule["outbound"] = target
	}
	return rule
}

// renderSingbox builds a complete sing-box profile: TUN and mixed inbounds, the nodes under a
// manual "proxy" selector with an "auto" url-test, the library rules, mainland direct and DNS.
func renderSingbox(proxies []map[string]any, rules []libraryRule, cfg service.DnsConfig) string {
	var nodes []any
	var names []string
	used := map[string]bool{"proxy": true, "auto": true, "direct": true}
	hasNodes := false
	for _, p := range proxies {
		ob := singboxOutbound(p)
		if ob == nil {
			continue
		}
		// Node names are free text; one equal to a built-in tag would stop sing-box from starting.
		tag := ob["tag"].(string)
		for i := 2; used[tag]; i++ {
			tag = ob["tag"].(string) + " (" + strconv.Itoa(i) + ")"
		}
		used[tag], ob["tag"] = true, tag
		nodes = append(nodes, ob)
		names = append(names, tag)
		if ob["type"] != "socks" || ob["server"] != "127.0.0.1" {
			hasNodes = true
		}
	}
	if !hasNodes {
		// Only the info node (expired or out of traffic): rule sets could not be downloaded through
		// it, so the profile stays minimal and just shows the node.
		rules, cfg = nil, service.DnsConfig{}
	}
	outbounds := []any{}
	if len(names) > 0 {
		outbounds = append(outbounds,
			map[string]any{"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, names...), "default": "auto"},
			map[string]any{
				"type": "urltest", "tag": "auto", "outbounds": names,
				"url": "https://www.gstatic.com/generate_204", "interval": "3m",
			})
	} else {
		outbounds = append(outbounds, map[string]any{"type": "selector", "tag": "proxy", "outbounds": []string{"direct"}})
	}
	outbounds = append(outbounds, nodes...)
	outbounds = append(outbounds, map[string]any{"type": "direct", "tag": "direct"})

	sets := newSingboxRuleSets()
	routeRules := []any{
		map[string]any{"action": "sniff"},
		map[string]any{"protocol": "dns", "action": "hijack-dns"},
		map[string]any{"ip_is_private": true, "outbound": "direct"},
	}
	if ips := companyDNSIPs(cfg); len(ips) > 0 {
		routeRules = append(routeRules, map[string]any{"ip_cidr": ips, "outbound": "direct"})
	}
	routeRules = append(routeRules, singboxRouteRules(rules, sets)...)
	if hasNodes {
		routeRules = append(routeRules,
			map[string]any{"rule_set": []string{sets.use("geosite", "cn")}, "outbound": "direct"},
			map[string]any{"rule_set": []string{sets.use("geoip", "cn")}, "outbound": "direct"})
	}

	domestic, foreign := cfg.Servers()
	servers := []any{map[string]any{"type": "udp", "tag": "dns-bootstrap", "server": "223.5.5.5"}}
	addFirst := func(tag string, list []string, detour bool) bool {
		for _, s := range list {
			if srv := singboxDNSServer(tag, s); srv != nil {
				if detour {
					srv["detour"] = "proxy"
				}
				servers = append(servers, srv)
				return true
			}
		}
		return false
	}
	final := "dns-bootstrap"
	if addFirst("dns-domestic", domestic, false) {
		final = "dns-domestic"
	}
	dnsRules := []any{}
	if cfg.Enable {
		company, _ := service.ParseCompanyDNS(cfg.Company)
		for i, r := range company {
			tag := "dns-company-" + strconv.Itoa(i+1)
			if addFirst(tag, r.Servers, false) {
				dnsRules = append(dnsRules, map[string]any{"domain_suffix": r.Domains, "server": tag})
			}
		}
	}
	if hasNodes && addFirst("dns-foreign", foreign, cfg.ForeignViaProxy) {
		dnsRules = append(dnsRules, map[string]any{"rule_set": []string{sets.use("geosite", "geolocation-!cn")}, "server": "dns-foreign"})
	}
	strategy := "ipv4_only"
	if cfg.IPv6 {
		strategy = "prefer_ipv4"
	}
	profile := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{"servers": servers, "rules": dnsRules, "final": final, "strategy": strategy},
		"inbounds": []any{
			map[string]any{
				"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route": true, "strict_route": true, "stack": "mixed",
			},
			map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"rules": routeRules, "rule_set": sets.sets, "final": "proxy",
			"auto_detect_interface": true, "default_domain_resolver": final,
		},
		"experimental": map[string]any{"cache_file": map[string]any{"enabled": true}},
	}
	body, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(body)
}
