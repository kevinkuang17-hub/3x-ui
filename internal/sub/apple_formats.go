package sub

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	yaml "github.com/goccy/go-yaml"
)

// Surge, Loon and Quantumult X profiles are rendered from the Clash/Mihomo proxy list, so every
// subscription feature (hosts, fallbacks, line health, group routing) applies to them unchanged.
const (
	formatSurge = "surge"
	formatLoon  = "loon"
	formatQX    = "qx"
)

var applePaths = map[string]string{formatSurge: "/surge/", formatLoon: "/loon/", formatQX: "/qx/"}

func (a *SUBController) initAppleFormats(g *gin.RouterGroup) {
	// They serve the Clash proxy list, so they exist only while the Clash subscription is on.
	if !a.clashEnabled {
		return
	}
	for _, format := range []string{formatSurge, formatLoon, formatQX} {
		path := applePaths[format]
		if owner := a.configuredSubscriptionPathOwner(path); owner != "" {
			continue
		}
		f := format
		grp := g.Group(path)
		grp.GET(":subid", func(c *gin.Context) { a.subApple(c, f) })
		grp.HEAD(":subid", func(c *gin.Context) { a.subApple(c, f) })
	}
}

// appleURLs derives the Surge/Loon/QX links from the raw link when it uses the panel's own path;
// a custom reverse-proxy URI gives no safe way to build them, so they are left out.
func (a *SUBController) appleURLs(subURL, subID string) map[string]string {
	suffix := strings.TrimSuffix(a.subPath, "/") + "/" + subID
	if !a.clashEnabled || subURL == "" || subID == "" || !strings.HasSuffix(subURL, suffix) {
		return nil
	}
	base := strings.TrimSuffix(subURL, suffix)
	out := map[string]string{}
	for format, key := range map[string]string{formatSurge: "Surge", formatLoon: "Loon", formatQX: "Qx"} {
		if a.configuredSubscriptionPathOwner(applePaths[format]) == "" {
			out[key] = base + applePaths[format] + subID
		}
	}
	return out
}

func (a *SUBController) subApple(c *gin.Context, format string) {
	if !a.enforceHwid(c) {
		return
	}
	subID := c.Param("subid")
	scheme, host, hostWithPort, _ := a.subService.ResolveRequest(c)
	clashSub, header, err := a.subClashService.GetClash(subID, host)
	if err != nil {
		writeSubError(c, err)
		return
	}
	if clashSub == "" && header == "" {
		writeSubError(c, nil)
		return
	}
	var doc struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashSub), &doc); err != nil {
		writeSubError(c, err)
		return
	}
	self := fmt.Sprintf("%s://%s%s", scheme, hostWithPort, c.Request.URL.EscapedPath())
	hours, _ := strconv.Atoi(a.updateInterval)
	body := renderAppleProfile(format, doc.Proxies, self, hours)
	a.ApplyCommonHeaders(c, header, a.updateInterval, "", "", "", "", false, "", false)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
	a.recordSubscriptionFetch(c)
}

func appleStr(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func appleBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func appleSub(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

// appleName strips characters that break the comma/equals separated formats.
func appleName(name string) string {
	return strings.NewReplacer(",", " ", "=", "-", "\n", " ", "\"", "'").Replace(strings.TrimSpace(name))
}

func wsOpts(p map[string]any) (path, hostHeader string) {
	ws := appleSub(p, "ws-opts")
	path = appleStr(ws, "path")
	if path == "" {
		path = "/"
	}
	hostHeader = appleStr(appleSub(ws, "headers"), "Host")
	return path, hostHeader
}

func sniOf(p map[string]any) string {
	if v := appleStr(p, "servername"); v != "" {
		return v
	}
	return appleStr(p, "sni")
}

// vlessEncrypted reports VLESS post-quantum encryption, which these apps cannot speak.
func vlessEncrypted(p map[string]any) bool {
	enc := appleStr(p, "encryption")
	return appleStr(p, "type") == "vless" && enc != "" && enc != "none"
}

// transportOK reports whether the proxy's transport is one these apps understand (tcp or ws).
func transportOK(p map[string]any) bool {
	n := appleStr(p, "network")
	return n == "" || n == "tcp" || n == "ws"
}

func surgeLine(p map[string]any) string {
	name, server, port := appleName(appleStr(p, "name")), appleStr(p, "server"), appleStr(p, "port")
	if !transportOK(p) || len(appleSub(p, "reality-opts")) > 0 {
		return ""
	}
	var parts []string
	switch appleStr(p, "type") {
	case "ss":
		if appleStr(p, "plugin") != "" {
			return ""
		}
		parts = []string{"ss", server, port, "encrypt-method=" + appleStr(p, "cipher"), "password=" + appleStr(p, "password"), "udp-relay=true"}
	case "vmess":
		parts = []string{"vmess", server, port, "username=" + appleStr(p, "uuid"), "vmess-aead=true"}
	case "trojan":
		parts = []string{"trojan", server, port, "password=" + appleStr(p, "password")}
	case "hysteria2":
		if appleStr(p, "obfs") != "" {
			return ""
		}
		parts = []string{"hysteria2", server, port, "password=" + appleStr(p, "password")}
	case "tuic":
		parts = []string{"tuic-v5", server, port, "uuid=" + appleStr(p, "uuid"), "password=" + appleStr(p, "password"), "alpn=h3"}
	default:
		return ""
	}
	if appleStr(p, "network") == "ws" {
		path, h := wsOpts(p)
		parts = append(parts, "ws=true", "ws-path="+path)
		if h != "" {
			parts = append(parts, `ws-headers=Host:"`+h+`"`)
		}
	}
	t := appleStr(p, "type")
	if appleBool(p, "tls") || t == "trojan" || t == "hysteria2" || t == "tuic" {
		if t == "vmess" {
			parts = append(parts, "tls=true")
		}
		if sni := sniOf(p); sni != "" {
			parts = append(parts, "sni="+sni)
		}
		parts = append(parts, "skip-cert-verify="+strconv.FormatBool(appleBool(p, "skip-cert-verify")))
		if fp := appleStr(p, "fingerprint"); fp != "" {
			parts = append(parts, "server-cert-fingerprint-sha256="+fp)
		}
	}
	return name + " = " + strings.Join(parts, ", ")
}

func loonLine(p map[string]any) string {
	name, server, port := appleName(appleStr(p, "name")), appleStr(p, "server"), appleStr(p, "port")
	if !transportOK(p) || vlessEncrypted(p) || appleStr(p, "fingerprint") != "" {
		return ""
	}
	t := appleStr(p, "type")
	var parts []string
	switch t {
	case "ss":
		if appleStr(p, "plugin") != "" {
			return ""
		}
		parts = []string{"Shadowsocks", server, port, appleStr(p, "cipher"), `"` + appleStr(p, "password") + `"`, "udp=true"}
	case "vmess":
		parts = []string{"vmess", server, port, "auto", `"` + appleStr(p, "uuid") + `"`}
	case "vless":
		parts = []string{"VLESS", server, port, `"` + appleStr(p, "uuid") + `"`}
	case "trojan":
		parts = []string{"trojan", server, port, `"` + appleStr(p, "password") + `"`}
	case "hysteria2":
		parts = []string{"Hysteria2", server, port, `"` + appleStr(p, "password") + `"`}
		switch appleStr(p, "obfs") {
		case "":
		case "salamander":
			parts = append(parts, "salamander-password="+appleStr(p, "obfs-password"))
		default:
			return ""
		}
	default:
		return ""
	}
	if t == "vmess" || t == "vless" || t == "trojan" {
		if appleStr(p, "network") == "ws" {
			path, h := wsOpts(p)
			parts = append(parts, "transport=ws", "path="+path)
			if h != "" {
				parts = append(parts, "host="+h)
			}
		} else {
			parts = append(parts, "transport=tcp")
		}
	}
	tls := appleBool(p, "tls") || t == "trojan" || t == "hysteria2"
	if tls {
		if t == "vmess" || t == "vless" {
			parts = append(parts, "over-tls=true")
		}
		if sni := sniOf(p); sni != "" {
			parts = append(parts, "tls-name="+sni)
		}
		parts = append(parts, "skip-cert-verify="+strconv.FormatBool(appleBool(p, "skip-cert-verify")))
	}
	if flow := appleStr(p, "flow"); flow != "" && t == "vless" {
		parts = append(parts, "flow="+flow)
	}
	if r := appleSub(p, "reality-opts"); len(r) > 0 {
		if t != "vless" {
			return ""
		}
		parts = append(parts, "public-key="+appleStr(r, "public-key"), "short-id="+appleStr(r, "short-id"))
	}
	return name + " = " + strings.Join(parts, ",")
}

func qxLine(p map[string]any) string {
	name, server, port := appleName(appleStr(p, "name")), appleStr(p, "server"), appleStr(p, "port")
	if !transportOK(p) || vlessEncrypted(p) || appleStr(p, "fingerprint") != "" {
		return ""
	}
	if strings.Contains(server, ":") && !strings.HasPrefix(server, "[") {
		server = "[" + server + "]"
	}
	t := appleStr(p, "type")
	var parts []string
	switch t {
	case "ss":
		if appleStr(p, "plugin") != "" {
			return ""
		}
		parts = []string{"shadowsocks=" + server + ":" + port, "method=" + appleStr(p, "cipher"), "password=" + appleStr(p, "password"), "udp-relay=true"}
	case "vmess":
		parts = []string{"vmess=" + server + ":" + port, "method=chacha20-poly1305", "password=" + appleStr(p, "uuid")}
	case "vless":
		parts = []string{"vless=" + server + ":" + port, "method=none", "password=" + appleStr(p, "uuid")}
	case "trojan":
		parts = []string{"trojan=" + server + ":" + port, "password=" + appleStr(p, "password")}
	default:
		return ""
	}
	tls := appleBool(p, "tls") || t == "trojan"
	if t != "ss" {
		path, h := wsOpts(p)
		switch {
		case appleStr(p, "network") == "ws" && tls:
			parts = append(parts, "obfs=wss", "obfs-uri="+path)
		case appleStr(p, "network") == "ws":
			parts = append(parts, "obfs=ws", "obfs-uri="+path)
		case tls && t != "trojan":
			parts = append(parts, "obfs=over-tls")
		case tls:
			parts = append(parts, "over-tls=true")
		}
		host := h
		if sni := sniOf(p); sni != "" {
			host = sni
		}
		if host != "" {
			if tls && t == "trojan" && appleStr(p, "network") != "ws" {
				parts = append(parts, "tls-host="+host)
			} else {
				parts = append(parts, "obfs-host="+host)
			}
		}
		if tls {
			parts = append(parts, "tls-verification="+strconv.FormatBool(!appleBool(p, "skip-cert-verify")))
		}
	}
	if r := appleSub(p, "reality-opts"); len(r) > 0 {
		if t != "vless" {
			return ""
		}
		parts = append(parts, "reality-base64-pubkey="+appleStr(r, "public-key"), "reality-hex-shortid="+appleStr(r, "short-id"))
	}
	if flow := appleStr(p, "flow"); flow != "" && t == "vless" {
		parts = append(parts, "vless-flow="+flow)
	}
	return strings.Join(append(parts, "tag="+name), ", ")
}

// renderAppleProfile builds a Surge/Loon managed profile or a Quantumult X server list.
// Proxies the target app cannot express (grpc, xhttp, wireguard, ...) are skipped.
func renderAppleProfile(format string, proxies []map[string]any, selfURL string, updateHours int) string {
	var lines, names []string
	seen := map[string]bool{}
	for _, p := range proxies {
		var line string
		switch format {
		case formatSurge:
			line = surgeLine(p)
		case formatLoon:
			line = loonLine(p)
		case formatQX:
			line = qxLine(p)
		}
		if line == "" {
			continue
		}
		name := appleName(appleStr(p, "name"))
		if seen[name] {
			continue
		}
		seen[name] = true
		lines = append(lines, line)
		names = append(names, name)
	}
	if format == formatQX {
		return strings.Join(lines, "\n") + "\n"
	}
	if updateHours <= 0 {
		updateHours = 12
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var b strings.Builder
	if format == formatSurge {
		fmt.Fprintf(&b, "#!MANAGED-CONFIG %s interval=%d strict=false\n\n", selfURL, updateHours*3600)
		b.WriteString("[General]\nloglevel = notify\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local\n")
		b.WriteString("dns-server = 223.5.5.5, 119.29.29.29, system\n\n[Proxy]\n")
	} else {
		b.WriteString("[General]\nskip-proxy = 127.0.0.1, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, 100.64.0.0/10, localhost, *.local\n")
		b.WriteString("dns-server = 223.5.5.5, 119.29.29.29, system\n\n[Proxy]\n")
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n[Proxy Group]\n")
	group := "Proxy = select"
	if len(names) > 0 {
		group += ", " + strings.Join(names, ", ")
	} else {
		group += ", DIRECT"
	}
	b.WriteString(group + "\n")
	if len(sorted) > 1 {
		auto := "Auto = url-test, " + strings.Join(sorted, ", ") + ", url = http://www.gstatic.com/generate_204, interval = 600"
		if format == formatLoon {
			auto = "Auto = url-test," + strings.Join(sorted, ",") + ",url = http://www.gstatic.com/generate_204,interval = 600"
		}
		b.WriteString(auto + "\n")
	}
	b.WriteString("\n[Rule]\nGEOIP,CN,DIRECT\nFINAL,Proxy\n")
	return b.String()
}
