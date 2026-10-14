package service

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// FirewallRule is one port opened in the server's firewall.
type FirewallRule struct {
	Port  int    `json:"port" example:"443"`
	Proto string `json:"proto" example:"udp"`
}

// FirewallStatus says whether ufw filters incoming traffic here; when it does, a port it was never
// told about is dropped, which is how a UDP inbound such as Hysteria2 silently stops working.
type FirewallStatus struct {
	Tool   string `json:"tool" example:"ufw"`
	Active bool   `json:"active" example:"true"`
}

// runFirewall runs a firewall command; tests replace it.
var runFirewall = func(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

var errNoFirewall = errors.New("ufw is not active on this server, so no port needs opening there")

func (s *ProtectService) Firewall() FirewallStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runFirewall(ctx, "ufw", "status")
	if err != nil {
		return FirewallStatus{}
	}
	return FirewallStatus{Tool: "ufw", Active: strings.Contains(strings.ToLower(out), "status: active")}
}

// AllowPorts opens each port for its protocol in ufw.
func (s *ProtectService) AllowPorts(rules []FirewallRule) ([]FirewallRule, error) {
	if !s.Firewall().Active {
		return nil, common.NewError(errNoFirewall.Error())
	}
	done := []FirewallRule{}
	for _, r := range rules {
		if r.Port < 1 || r.Port > 65535 || (r.Proto != "tcp" && r.Proto != "udp") {
			return done, common.NewErrorf("not a port rule: %d/%s", r.Port, r.Proto)
		}
		if slices.Contains(done, r) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, err := runFirewall(ctx, "ufw", "allow", strconv.Itoa(r.Port)+"/"+r.Proto)
		cancel()
		if err != nil {
			return done, common.NewErrorf("ufw allow %d/%s: %v %s", r.Port, r.Proto, err, strings.TrimSpace(out))
		}
		done = append(done, r)
	}
	return done, nil
}

// InboundFirewallRules lists the ports this server's enabled inbounds listen on, with the
// protocols they actually use: Hysteria2 and TUIC need UDP, Shadowsocks often both.
func InboundFirewallRules() ([]FirewallRule, error) {
	var inbounds []model.Inbound
	if err := database.GetDB().Where("enable = ? AND node_id IS NULL AND port > 0", true).Find(&inbounds).Error; err != nil {
		return nil, err
	}
	rules := []FirewallRule{}
	for i := range inbounds {
		mine, _ := FirewallRulesFor(&inbounds[i])
		for _, r := range mine {
			if !slices.Contains(rules, r) {
				rules = append(rules, r)
			}
		}
	}
	return rules, nil
}

// FirewallRulesFor lists the port rules one inbound needs; loopback and socket listeners need none.
func FirewallRulesFor(ib *model.Inbound) ([]FirewallRule, bool) {
	if ib.Port <= 0 || strings.HasPrefix(ib.Listen, "127.") || ib.Listen == "::1" || strings.HasPrefix(ib.Listen, "/") || strings.HasPrefix(ib.Listen, "@") {
		return nil, false
	}
	bits := inboundTransports(ib.Protocol, ib.StreamSettings, ib.Settings)
	var rules []FirewallRule
	if bits&transportTCP != 0 {
		rules = append(rules, FirewallRule{ib.Port, "tcp"})
	}
	if bits&transportUDP != 0 {
		rules = append(rules, FirewallRule{ib.Port, "udp"})
	}
	return rules, true
}

// PortCheck is what the server hosting an inbound sees of its port: whether something is bound to
// it (Xray, when the inbound is up) and whether ufw lets the traffic in.
type PortCheck struct {
	Port  int    `json:"port" example:"443"`
	Proto string `json:"proto" example:"udp"`
	// Bound is nil when binding failed for another reason than "in use" (e.g. no permission).
	Bound    *bool  `json:"bound"`
	Firewall string `json:"firewall" example:"ufw"`
	Allowed  bool   `json:"allowed" example:"true"`
}

// portBound tries to take the port: "address in use" means something already listens on it.
var portBound = func(port int, proto string) *bool {
	addr := ":" + strconv.Itoa(port)
	var err error
	if proto == "udp" {
		var c net.PacketConn
		if c, err = net.ListenPacket("udp", addr); err == nil {
			_ = c.Close()
		}
	} else {
		var l net.Listener
		if l, err = net.Listen("tcp", addr); err == nil {
			_ = l.Close()
		}
	}
	bound := err != nil
	if err != nil && !errors.Is(err, syscall.EADDRINUSE) {
		return nil
	}
	return &bound
}

func (s *ProtectService) CheckPort(port int, proto string) (*PortCheck, error) {
	if port < 1 || port > 65535 || (proto != "tcp" && proto != "udp") {
		return nil, common.NewErrorf("not a port rule: %d/%s", port, proto)
	}
	pc := &PortCheck{Port: port, Proto: proto, Bound: portBound(port, proto), Allowed: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runFirewall(ctx, "ufw", "status", "verbose")
	if err == nil && strings.Contains(strings.ToLower(out), "status: active") {
		pc.Firewall = "ufw"
		pc.Allowed = ufwAllows(out, port, proto)
	}
	return pc, nil
}

// ufwAllows reads `ufw status verbose`: an incoming default of allow lets everything in, otherwise a
// rule must cover the port for that protocol ("443", "443/udp", "10000:20000/udp").
func ufwAllows(status string, port int, proto string) bool {
	for _, line := range strings.Split(status, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "default:") {
			return ufwDefaultAllows(l) || ufwRuleAllows(status, port, proto)
		}
	}
	return ufwRuleAllows(status, port, proto)
}

func ufwDefaultAllows(defaultLine string) bool {
	for _, part := range strings.Split(strings.TrimPrefix(defaultLine, "default:"), ",") {
		part = strings.TrimSpace(part)
		if strings.HasSuffix(part, "(incoming)") {
			return strings.HasPrefix(part, "allow")
		}
	}
	return false
}

func ufwRuleAllows(status string, port int, proto string) bool {
	for _, line := range strings.Split(status, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[1], "ALLOW") {
			continue
		}
		spec, ruleProto, _ := strings.Cut(fields[0], "/")
		if ruleProto != "" && ruleProto != proto {
			continue
		}
		from, to, isRange := strings.Cut(spec, ":")
		lo, err := strconv.Atoi(from)
		if err != nil {
			continue
		}
		hi := lo
		if isRange {
			if hi, err = strconv.Atoi(to); err != nil {
				continue
			}
		}
		if port >= lo && port <= hi {
			return true
		}
	}
	return false
}
