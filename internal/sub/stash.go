package sub

import (
	"strings"

	yaml "github.com/goccy/go-yaml"
)

// Stash reads Clash profiles but stops on parts only Mihomo knows. These are the proxy types and
// transports it supports; anything else is left out so the rest of the profile still imports.
var stashProxyTypes = map[string]bool{
	"ss": true, "ssr": true, "vmess": true, "vless": true, "trojan": true, "socks5": true, "http": true,
	"snell": true, "hysteria": true, "hysteria2": true, "tuic": true, "wireguard": true,
}

var stashUnsupportedNetworks = map[string]bool{"xhttp": true, "splithttp": true, "httpupgrade": true}

// Mihomo-only top-level settings; Stash has its own switches for these.
var stashDroppedKeys = map[string]bool{
	"sniffer": true, "tun": true, "geox-url": true, "geodata-mode": true, "geodata-loader": true,
	"find-process-mode": true, "global-client-fingerprint": true, "unified-delay": true,
	"tcp-concurrent": true, "keep-alive-interval": true, "profile": true,
}

func stashKeepsProxy(p map[string]any) bool {
	typ, _ := p["type"].(string)
	if !stashProxyTypes[strings.ToLower(typ)] {
		return false
	}
	if network, _ := p["network"].(string); stashUnsupportedNetworks[strings.ToLower(network)] {
		return false
	}
	// VLESS Encryption (post-quantum handshake) has no Stash client.
	if enc, _ := p["encryption"].(string); enc != "" && enc != "none" {
		return false
	}
	return true
}

// renderStash filters a Clash/Mihomo profile down to what Stash imports.
func renderStash(clashYAML string) (string, error) {
	var doc yaml.MapSlice
	if err := yaml.Unmarshal([]byte(clashYAML), &doc); err != nil {
		return "", err
	}
	dropped := map[string]bool{}
	droppedProviders := map[string]bool{}
	out := make(yaml.MapSlice, 0, len(doc))
	for _, item := range doc {
		key, _ := item.Key.(string)
		if stashDroppedKeys[key] {
			continue
		}
		switch key {
		case "proxies":
			item.Value = stashProxies(item.Value, dropped)
		case "rule-providers":
			item.Value = stashProviders(item.Value, droppedProviders)
		}
		out = append(out, item)
	}
	for i, item := range out {
		switch item.Key {
		case "proxy-groups":
			out[i].Value = stashGroups(item.Value, dropped)
		case "rules":
			out[i].Value = stashRules(item.Value, droppedProviders)
		}
	}
	b, err := yaml.Marshal(out)
	return string(b), err
}

func stashProxies(v any, dropped map[string]bool) []any {
	list, _ := v.([]any)
	kept := make([]any, 0, len(list))
	for _, raw := range list {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !stashKeepsProxy(p) {
			if name, _ := p["name"].(string); name != "" {
				dropped[name] = true
			}
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func stashGroups(v any, dropped map[string]bool) []any {
	list, _ := v.([]any)
	for _, raw := range list {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		members, _ := g["proxies"].([]any)
		kept := make([]any, 0, len(members))
		for _, m := range members {
			if name, _ := m.(string); !dropped[name] {
				kept = append(kept, m)
			}
		}
		// A group left with nothing to pick fails the whole import; DIRECT keeps it valid.
		if len(kept) == 0 && g["use"] == nil {
			kept = append(kept, "DIRECT")
		}
		g["proxies"] = kept
	}
	return list
}

// stashProviders drops Mihomo's binary .mrs rule sets, which Stash cannot read.
func stashProviders(v any, dropped map[string]bool) any {
	providers, ok := v.(map[string]any)
	if !ok {
		return v
	}
	for name, raw := range providers {
		p, _ := raw.(map[string]any)
		if format, _ := p["format"].(string); strings.EqualFold(format, "mrs") {
			dropped[name] = true
			delete(providers, name)
		}
	}
	return providers
}

func stashRules(v any, droppedProviders map[string]bool) []any {
	list, _ := v.([]any)
	kept := make([]any, 0, len(list))
	for _, raw := range list {
		rule, _ := raw.(string)
		parts := strings.Split(rule, ",")
		if len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "RULE-SET") && droppedProviders[strings.TrimSpace(parts[1])] {
			continue
		}
		kept = append(kept, raw)
	}
	return kept
}
