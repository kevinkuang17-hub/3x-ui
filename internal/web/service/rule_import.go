package service

import (
	"fmt"
	"net"
	"sort"
	"strings"

	yaml "github.com/goccy/go-yaml"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// ImportResult says what an imported Clash config turned into.
type ImportResult struct {
	Sets    []string `json:"sets"`
	Rules   int      `json:"rules"`
	Skipped int      `json:"skipped"`
	Dns     bool     `json:"dns"`
}

// importPolicy maps an imported rule's target onto a rule-set default: its built-in policies stay,
// any proxy group becomes "proxy".
func importPolicy(target string) string {
	switch up := strings.ToUpper(target); {
	case up == "DIRECT":
		return "DIRECT"
	case strings.HasPrefix(up, "REJECT"):
		return "REJECT"
	}
	return "PROXY"
}

// importRule splits "TYPE,value,TARGET[,flags]" into the library line and its target; rules the
// library cannot hold (MATCH, RULE-SET, logic rules) report ok=false.
func importRule(rule string) (line, target string, ok bool) {
	parts := strings.Split(rule, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) < 3 {
		return "", "", false
	}
	kind := strings.ToUpper(parts[0])
	if !ruleSetMatchTypes[kind] || parts[1] == "" {
		return "", "", false
	}
	i := len(parts) - 1
	flag := ""
	for i >= 2 && (strings.EqualFold(parts[i], "no-resolve") || strings.EqualFold(parts[i], "src")) {
		if strings.EqualFold(parts[i], "no-resolve") {
			flag = ",no-resolve"
		}
		i--
	}
	if i != 2 || parts[2] == "" {
		return "", "", false
	}
	return kind + "," + parts[1] + flag, parts[2], true
}

// ImportClashConfig turns a Clash/Mihomo config (or subscription) into rule sets, one per target
// group, and optionally takes over its DNS servers and company DNS rules.
func (s *RuleSetService) ImportClashConfig(content []byte, label string, importDNS bool) (*ImportResult, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(content, &doc); err != nil || doc == nil {
		return nil, common.NewError("not a Clash/Mihomo YAML config")
	}
	rawRules, _ := doc["rules"].([]any)
	byTarget := map[string][]string{}
	var order []string
	res := &ImportResult{}
	seen := map[string]bool{}
	for _, r := range rawRules {
		text, _ := r.(string)
		line, target, ok := importRule(text)
		if !ok {
			res.Skipped++
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		if _, exists := byTarget[target]; !exists {
			order = append(order, target)
		}
		byTarget[target] = append(byTarget[target], line)
		res.Rules++
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "import"
	}
	for _, target := range order {
		lines := byTarget[target]
		for start := 0; start < len(lines); start += maxRuleSetLines {
			end := min(start+maxRuleSetLines, len(lines))
			name := fmt.Sprintf("%s · %s", label, target)
			if start > 0 {
				name = fmt.Sprintf("%s (%d)", name, start/maxRuleSetLines+1)
			}
			if _, err := s.Save(model.RuleSet{
				Name: name, Category: label, Rules: strings.Join(lines[start:end], "\n"), DefaultPolicy: importPolicy(target),
			}); err != nil {
				return res, err
			}
			res.Sets = append(res.Sets, name)
		}
	}
	if importDNS {
		if dnsDoc, ok := doc["dns"].(map[string]any); ok {
			cfg := importedDNS(dnsDoc, s.GlobalDns())
			if err := s.SaveGlobalDns(cfg); err != nil {
				return res, err
			}
			res.Dns = true
		}
	}
	if len(res.Sets) == 0 && !res.Dns {
		return res, common.NewError("nothing to import: the config has no rules the library can hold")
	}
	return res, nil
}

func stringList(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, it := range items {
		if s, ok := it.(string); ok && validDNSServer(strings.TrimSpace(s)) {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// importedDNS keeps the existing switches and takes servers and company rules from the config.
func importedDNS(dns map[string]any, base DnsConfig) DnsConfig {
	cfg := base
	cfg.Enable = true
	if mode, _ := dns["enhanced-mode"].(string); mode == "fake-ip" || mode == "redir-host" {
		cfg.Mode = mode
	}
	if v := stringList(dns["nameserver"]); len(v) > 0 {
		cfg.Domestic = v
	}
	if v := stringList(dns["fallback"]); len(v) > 0 {
		cfg.Foreign = v
	}
	policy, _ := dns["nameserver-policy"].(map[string]any)
	keys := make([]string, 0, len(policy))
	for k := range policy {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		var servers []string
		switch v := policy[k].(type) {
		case string:
			servers = splitList(v)
		case []any:
			servers = stringList(v)
		}
		var domains []string
		for _, d := range splitList(k) {
			if strings.Contains(d, ":") {
				continue
			}
			d = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(d, "+."), "*."))
			if domainPattern.MatchString(d) {
				domains = append(domains, d)
			}
		}
		var valid []string
		for _, sv := range servers {
			if validDNSServer(sv) && !strings.Contains(sv, "#") {
				valid = append(valid, sv)
			}
		}
		if len(domains) > 0 && len(valid) > 0 && isPrivateServerList(valid) {
			lines = append(lines, strings.Join(domains, ", ")+" = "+strings.Join(valid, ", "))
		}
	}
	if len(lines) > 0 {
		cfg.Company = strings.Join(lines, "\n")
	}
	return cfg
}

// isPrivateServerList keeps policies that point at internal resolvers, which is what makes them
// company DNS; public resolvers for single sites are left to the general DNS.
func isPrivateServerList(servers []string) bool {
	for _, s := range servers {
		host := s
		if h, _, err := net.SplitHostPort(s); err == nil {
			host = h
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback()) {
			return true
		}
	}
	return false
}
