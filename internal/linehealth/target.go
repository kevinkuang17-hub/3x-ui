package linehealth

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// ProbeHost mirrors the subscription's own address choice (resolveInboundAddress) and returns ""
// when clients reach the line some other way: fallback child, external proxy, or a private address.
func ProbeHost(ib *model.Inbound, node *model.Node) string {
	listen := strings.TrimSpace(ib.Listen)
	if strings.HasPrefix(listen, "@") || strings.HasPrefix(listen, "/") || hostIsInternal(listen) {
		return ""
	}
	var stream map[string]any
	_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	if proxies, ok := stream["externalProxy"].([]any); ok && len(proxies) > 0 {
		return ""
	}
	nodeAddr := strings.TrimSpace(node.Address)
	listenAddr := ""
	if listen != "" && !hostIsWildcard(listen) {
		listenAddr = listen
	}
	candidates := []string{nodeAddr, listenAddr}
	switch ib.ShareAddrStrategy {
	case "listen":
		candidates = []string{listenAddr, nodeAddr}
	case "custom":
		candidates = []string{strings.TrimSpace(ib.ShareAddr), nodeAddr, listenAddr}
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if hostIsInternal(c) || hostIsPrivate(c) {
			return ""
		}
		return c
	}
	return ""
}

func hostIsInternal(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func hostIsWildcard(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
}

// A private/CGNAT node address (e.g. a Tailscale IP) is how the master reaches the panel, not users.
func hostIsPrivate(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	cgnat := net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
	return cgnat.Contains(ip)
}

// IsUDP reports lines with no TCP handshake to test (QUIC/KCP based protocols and transports).
func IsUDP(ib *model.Inbound) bool {
	switch ib.Protocol {
	case model.Hysteria, model.TUIC, model.WireGuard, model.AmneziaWG:
		return true
	}
	var stream struct {
		Network string `json:"network"`
	}
	_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	return stream.Network == "kcp" || stream.Network == "quic"
}
