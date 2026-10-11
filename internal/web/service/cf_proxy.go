package service

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// cloudflareHTTPSPorts are the only HTTPS ports Cloudflare's proxy forwards.
var cloudflareHTTPSPorts = []int{443, 2053, 2083, 2087, 2096, 8443}

type CfProxyStatus struct {
	Domain  string `json:"domain"`
	Found   bool   `json:"found"`
	IP      string `json:"ip"`
	Proxied bool   `json:"proxied"`
	Port    int    `json:"port"`
	PortOK  bool   `json:"portOk"`
	SslMode string `json:"sslMode"`
}

type cfRecord struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// zoneFor finds the Cloudflare zone holding fqdn among the zones the token can see, so a
// subscription domain in another zone than the node domains works too.
func (s *NodeDnsService) zoneFor(ctx context.Context, fqdn string) (string, error) {
	labels := strings.Split(strings.Trim(strings.ToLower(fqdn), "."), ".")
	for i := 0; i+1 < len(labels); i++ {
		name := strings.Join(labels[i:], ".")
		if v, ok := cfZoneIDCache.Load(name); ok {
			return v.(string), nil
		}
		var zones []struct {
			ID string `json:"id"`
		}
		if err := s.call(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &zones); err != nil {
			return "", err
		}
		if len(zones) > 0 {
			cfZoneIDCache.Store(name, zones[0].ID)
			return zones[0].ID, nil
		}
	}
	return "", common.NewError("no Cloudflare zone of this API token contains", fqdn)
}

func (s *NodeDnsService) ProxyStatus(ctx context.Context, fqdn string, port int) (*CfProxyStatus, error) {
	fqdn = strings.Trim(strings.ToLower(strings.TrimSpace(fqdn)), ".")
	if fqdn == "" {
		return nil, common.NewError("domain is required")
	}
	if token, _ := s.settingService.GetCfApiToken(); strings.TrimSpace(token) == "" {
		return nil, common.NewError("set the Cloudflare API token in Settings first")
	}
	zid, err := s.zoneFor(ctx, fqdn)
	if err != nil {
		return nil, err
	}
	st := &CfProxyStatus{Domain: fqdn, Port: port, PortOK: slices.Contains(cloudflareHTTPSPorts, port)}
	var records []cfRecord
	if err := s.call(ctx, http.MethodGet, "/zones/"+zid+"/dns_records?type=A&name="+url.QueryEscape(fqdn), nil, &records); err != nil {
		return nil, err
	}
	if len(records) > 0 {
		st.Found, st.IP, st.Proxied = true, records[0].Content, records[0].Proxied
	}
	// Reading the SSL mode needs a zone-settings permission the token may lack; it is advisory.
	var ssl struct {
		Value string `json:"value"`
	}
	if s.call(ctx, http.MethodGet, "/zones/"+zid+"/settings/ssl", nil, &ssl) == nil {
		st.SslMode = ssl.Value
	}
	return st, nil
}

// SetProxied turns Cloudflare's proxy on or off for the domain's A record. Turning it on is refused
// when Cloudflare could not reach the panel: a port it does not forward, or SSL mode off/flexible.
func (s *NodeDnsService) SetProxied(ctx context.Context, fqdn string, port int, on bool) (*CfProxyStatus, error) {
	st, err := s.ProxyStatus(ctx, fqdn, port)
	if err != nil {
		return nil, err
	}
	if !st.Found {
		return nil, common.NewError("no A record for", st.Domain, "in Cloudflare")
	}
	if on {
		if !st.PortOK {
			return nil, common.NewErrorf("Cloudflare only proxies HTTPS on ports %v; move this service to one of them first", cloudflareHTTPSPorts)
		}
		if st.SslMode == "off" || st.SslMode == "flexible" {
			return nil, common.NewError("set Cloudflare SSL/TLS mode to Full or Full (strict) first; it is", st.SslMode)
		}
	}
	if st.Proxied == on {
		return st, nil
	}
	zid, err := s.zoneFor(ctx, st.Domain)
	if err != nil {
		return nil, err
	}
	var records []cfRecord
	if err := s.call(ctx, http.MethodGet, "/zones/"+zid+"/dns_records?type=A&name="+url.QueryEscape(st.Domain), nil, &records); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, common.NewError("no A record for", st.Domain, "in Cloudflare")
	}
	if err := s.call(ctx, http.MethodPatch, "/zones/"+zid+"/dns_records/"+records[0].ID, map[string]any{"proxied": on}, nil); err != nil {
		return nil, err
	}
	st.Proxied = on
	return st, nil
}
