// Package panelaccess limits which source addresses may open the panel, so a node's panel answers
// only its master and a scanner sees nothing there.
package panelaccess

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

var allowed atomic.Pointer[[]*net.IPNet]

// Parse reads a comma/space/newline separated list of IPs and CIDRs.
func Parse(raw string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' }) {
		if _, n, err := net.ParseCIDR(item); err == nil {
			out = append(out, n)
			continue
		}
		ip := net.ParseIP(item)
		if ip == nil {
			return nil, fmt.Errorf("not an IP address or range: %q", item)
		}
		bits := 128
		if ip.To4() != nil {
			ip, bits = ip.To4(), 32
		}
		out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
	}
	return out, nil
}

// Configure installs the allowlist; an empty list lets everyone through.
func Configure(raw string) error {
	nets, err := Parse(raw)
	if err != nil {
		return err
	}
	allowed.Store(&nets)
	return nil
}

func Active() bool {
	nets := allowed.Load()
	return nets != nil && len(*nets) > 0
}

// Allows reports whether a source address may use the panel; loopback always may.
func Allows(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	nets := allowed.Load()
	if nets == nil || len(*nets) == 0 {
		return true
	}
	for _, n := range *nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// RemoteIP is the TCP peer, never a forwarded header a client could forge.
func RemoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// Middleware turns away blocked addresses with deny; public prefixes (node join, ISP probes, SSO)
// stay reachable because outside systems call them.
func Middleware(publicPrefixes []string, deny gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Active() || Allows(RemoteIP(c.Request)) {
			c.Next()
			return
		}
		for _, p := range publicPrefixes {
			if strings.HasPrefix(c.Request.URL.Path, p) {
				c.Next()
				return
			}
		}
		deny(c)
	}
}
