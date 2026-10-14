package service

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"
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
		ib := &inbounds[i]
		if strings.HasPrefix(ib.Listen, "127.") || ib.Listen == "::1" || strings.HasPrefix(ib.Listen, "/") || strings.HasPrefix(ib.Listen, "@") {
			continue
		}
		bits := inboundTransports(ib.Protocol, ib.StreamSettings, ib.Settings)
		for _, r := range []FirewallRule{{ib.Port, "tcp"}, {ib.Port, "udp"}} {
			want := (r.Proto == "tcp" && bits&transportTCP != 0) || (r.Proto == "udp" && bits&transportUDP != 0)
			if want && !slices.Contains(rules, r) {
				rules = append(rules, r)
			}
		}
	}
	return rules, nil
}
