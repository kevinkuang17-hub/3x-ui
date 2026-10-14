package service

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// REALITY "using its own website": the inbound takes TCP 443 with the server's own domain as SNI,
// and the decoy website moves to a loopback port as its target. Everything a censor or browser sees
// on 443 is then a real site with a valid certificate for that domain on that IP.

// RealityPublicPort is where a REALITY inbound fronting its own website listens.
const RealityPublicPort = 443

// RealitySelfPlan is what the first step decided; the panel restarts before the second step.
type RealitySelfPlan struct {
	Domain       string `json:"domain" example:"us.example.com"`
	InternalPort int    `json:"internalPort" example:"24873"`
}

func (s *ProtectService) realitySelfInbound(id int) (*model.Inbound, map[string]any, error) {
	var ib model.Inbound
	if err := database.GetDB().First(&ib, id).Error; err != nil {
		return nil, nil, common.NewError("inbound not found")
	}
	var stream map[string]any
	_ = json.Unmarshal([]byte(ib.StreamSettings), &stream)
	security, _ := stream["security"].(string)
	if ib.NodeID != nil || ib.Protocol != model.VLESS || security != "reality" {
		return nil, nil, common.NewError("pick a VLESS + REALITY inbound on this server")
	}
	return &ib, stream, nil
}

// realitySelfDomain is the server's domain with a valid certificate pointing here: the SNI.
func realitySelfDomain() (string, error) {
	rec, err := (&InboundRecommendService{}).Recommend(context.Background(), nil)
	if err != nil {
		return "", err
	}
	if rec.Domain == "" || rec.CertState != CertStateOK {
		return "", common.NewError("the panel needs a domain with a valid certificate (x-ui menu → SSL) first")
	}
	return rec.Domain, nil
}

// PrepareRealitySelf moves the website to a loopback port; the caller restarts the panel, then
// calls ApplyRealitySelf once the website runs there.
func (s *ProtectService) PrepareRealitySelf(inboundID int) (*RealitySelfPlan, error) {
	ib, _, err := s.realitySelfInbound(inboundID)
	if err != nil {
		return nil, err
	}
	if mode, _ := s.settingService.getString("decoyMode"); mode == "" || mode == decoy.ModeOff {
		return nil, common.NewError("deploy a decoy website first: it is what visitors of your domain will see")
	}
	if _, frontOnly := s.FrontSettings(); frontOnly {
		return nil, common.NewError("open the panel port again first; the website is about to move")
	}
	domain, err := realitySelfDomain()
	if err != nil {
		return nil, err
	}
	if conflict := s.publicPortConflict(ib.Id); conflict != "" {
		return nil, common.NewErrorf("TCP %d is used by %q; move it first", RealityPublicPort, conflict)
	}
	port := s.FrontPort()
	if !s.FrontLoopback() || port == RealityPublicPort {
		if port, err = s.pickInternalPort(); err != nil {
			return nil, err
		}
	}
	for key, value := range map[string]string{
		"decoyFront": "true", "decoyFrontPort": strconv.Itoa(port), "decoyFrontLoopback": "true",
	} {
		if err := s.settingService.setString(key, value); err != nil {
			return nil, err
		}
	}
	return &RealitySelfPlan{Domain: domain, InternalPort: port}, nil
}

// publicPortConflict names what else holds TCP 443 here, apart from the inbound being moved and the
// website (which moves to loopback in the same step).
func (s *ProtectService) publicPortConflict(inboundID int) string {
	var inbounds []model.Inbound
	database.GetDB().Where("port = ? AND enable = ? AND node_id IS NULL AND id <> ?", RealityPublicPort, true, inboundID).Find(&inbounds)
	for i := range inbounds {
		if rules, _ := FirewallRulesFor(&inbounds[i]); hasTCP(rules) {
			if inbounds[i].Remark != "" {
				return inbounds[i].Remark
			}
			return inbounds[i].Tag
		}
	}
	if s.panelPort() == RealityPublicPort {
		return "panel"
	}
	if on, _ := s.settingService.GetSubEnable(); on {
		if subPort, _ := s.settingService.GetSubPort(); subPort == RealityPublicPort {
			return "subscription"
		}
	}
	return ""
}

func hasTCP(rules []FirewallRule) bool {
	for _, r := range rules {
		if r.Proto == "tcp" {
			return true
		}
	}
	return false
}

// pickInternalPort finds a high port no inbound, panel or other program uses.
func (s *ProtectService) pickInternalPort() (int, error) {
	inbounds := InboundService{}
	for range 200 {
		port := 20000 + rand.IntN(40000)
		if s.frontConflict(port) != "" {
			continue
		}
		probe := &model.Inbound{Port: port, Protocol: model.VLESS, Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp"}`}
		if conflict, err := inbounds.checkPortConflict(probe, 0); err != nil || conflict != nil {
			continue
		}
		if !recommendLocalPortFree(port, false) {
			continue
		}
		return port, nil
	}
	return 0, common.NewError("no free port for the website")
}

// RealitySelfStream points the inbound at the loopback website with the server's domain as SNI;
// xver 1 hands the visitor's address to the website so the panel allowlist still applies.
func RealitySelfStream(stream map[string]any, domain string, internalPort int) map[string]any {
	reality, _ := stream["realitySettings"].(map[string]any)
	if reality == nil {
		reality = map[string]any{}
	}
	reality["target"] = net.JoinHostPort("127.0.0.1", strconv.Itoa(internalPort))
	delete(reality, "dest")
	reality["serverNames"] = []any{domain}
	reality["xver"] = 1
	if client, ok := reality["settings"].(map[string]any); ok {
		client["serverName"] = domain
	}
	stream["realitySettings"] = reality
	return stream
}

// ApplyRealitySelf moves the inbound to 443 in front of the website, which must already listen on
// its loopback port (after the restart PrepareRealitySelf asked for).
func (s *ProtectService) ApplyRealitySelf(inboundID int) (*model.Inbound, bool, error) {
	ib, stream, err := s.realitySelfInbound(inboundID)
	if err != nil {
		return nil, false, err
	}
	port := s.FrontPort()
	st := CurrentFrontStatus()
	if !s.FrontLoopback() || !st.Listening || !strings.HasSuffix(st.Addr, ":"+strconv.Itoa(port)) {
		return nil, false, common.NewError("the website is not running on its internal port yet; wait for the panel restart and retry")
	}
	domain, err := realitySelfDomain()
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(RealitySelfStream(stream, domain, port))
	if err != nil {
		return nil, false, err
	}
	ib.StreamSettings = string(raw)
	ib.Port = RealityPublicPort
	return (&InboundService{}).UpdateInbound(ib)
}
