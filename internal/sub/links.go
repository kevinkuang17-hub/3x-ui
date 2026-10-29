package sub

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type LinkProvider struct {
	settingService service.SettingService
}

func NewLinkProvider() *LinkProvider {
	return &LinkProvider{}
}

func (p *LinkProvider) build(host string) *SubService {
	remarkTemplate, _ := p.settingService.GetRemarkTemplate()
	svc := NewSubService(remarkTemplate)
	svc.PrepareForRequest(host)
	return svc
}

func (p *LinkProvider) SubLinksForSubId(host, subId string) ([]string, error) {
	svc := p.build(host)
	links, _, _, _, err := svc.GetSubs(subId, host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(links))
	for _, l := range links {
		out = append(out, splitLinkLines(l)...)
	}
	return out, nil
}

func (p *LinkProvider) LinksForClient(host string, inbound *model.Inbound, email string) []string {
	svc := p.build(host)
	svc.projectThroughFallbackMaster(inbound)
	baseEps := svc.hostEndpoints(inbound, "raw")
	if client, ok := svc.clientForLink(inbound, email); ok {
		effectiveEps, bound := svc.clientHostEndpoints(inbound, client, "raw", baseEps)
		if bound && len(effectiveEps) == 0 {
			return nil
		}
		if len(effectiveEps) > 0 {
			if link := svc.linkFromHosts(inbound, client, effectiveEps); link != "" {
				return splitLinkLines(link)
			}
		}
	}
	return splitLinkLines(svc.GetLink(inbound, email))
}

func (p *LinkProvider) LinksForInbounds(host string, inbounds []*model.Inbound) []string {
	svc := p.build(host)
	var out []string
	for _, inbound := range inbounds {
		out = append(out, svc.inboundLinks(inbound)...)
	}
	return out
}

func splitLinkLines(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ErrNoEnabledClient means the inbound has no client a subscriber could connect with.
var ErrNoEnabledClient = errors.New("no enabled client on this inbound")

// ClientOutbound is the Xray outbound the inbound's first enabled client gets in a JSON
// subscription, so a test through it uses the inbound exactly the way a client app does.
func (p *LinkProvider) ClientOutbound(host string, inbound *model.Inbound) (map[string]any, string, error) {
	svc := p.build(host)
	svc.projectThroughFallbackMaster(inbound)
	clients, err := svc.clientsForLinkExport(inbound)
	if err != nil {
		return nil, "", err
	}
	idx := slices.IndexFunc(clients, func(c model.Client) bool { return c.Enable })
	if idx < 0 {
		return nil, "", ErrNoEnabledClient
	}
	client := clients[idx]
	mux, _ := p.settingService.GetSubJsonMux()
	mask, _ := p.settingService.GetSubJsonFinalMask()
	js := NewSubJsonService(mux, "", mask, "", svc)
	for _, cfg := range js.getConfig(svc, inbound, client, host) {
		if ob := extractProxyOutbound(cfg); ob != nil {
			return ob, client.Email, nil
		}
	}
	return nil, client.Email, fmt.Errorf("%s inbounds have no Xray client config to test with", inbound.Protocol)
}

// SidecarEndpoint is where clients of an AnyTLS or NaiveProxy inbound connect and the name they
// verify; those protocols have no Xray client config to test through.
func (p *LinkProvider) SidecarEndpoint(host string, inbound *model.Inbound) (address, serverName string) {
	svc := p.build(host)
	address = svc.resolveInboundAddress(inbound)
	return address, sidecarSNI(unmarshalStreamSettings(inbound.StreamSettings), address)
}
