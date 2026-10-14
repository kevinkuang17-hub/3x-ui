package sub

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

// sidecarSNI is the name the client must send: the inbound's TLS server name, else the address
// itself when it is a domain (a bare IP never matches the certificate).
func sidecarSNI(stream map[string]any, address string) string {
	tls, _ := stream["tlsSettings"].(map[string]any)
	sni, _ := tls["serverName"].(string)
	if sni = strings.TrimSpace(sni); sni == "" && net.ParseIP(strings.Trim(address, "[]")) == nil {
		sni = address
	}
	return sni
}

// genAnyTLSLink follows the anytls:// URI the AnyTLS reference client defines.
func (s *SubService) genAnyTLSLink(inbound *model.Inbound, email string) string {
	client, ok := s.clientForLink(inbound, email)
	if !ok || client.Password == "" {
		return ""
	}
	address := s.resolveInboundAddress(inbound)
	params := map[string]string{}
	if sni := sidecarSNI(unmarshalStreamSettings(inbound.StreamSettings), address); sni != "" {
		params["sni"] = sni
	}
	link := fmt.Sprintf("anytls://%s@%s/", encodeUserinfo(client.Password), joinHostPort(address, inbound.Port))
	return buildLinkWithParams(link, params, s.genRemark(inbound, email, "", "anytls"))
}

// genNaiveLink emits naive+https://user:pass@host:port, the form NaiveProxy clients import.
// The address must be the certificate's domain, since naive has no separate SNI field.
func (s *SubService) genNaiveLink(inbound *model.Inbound, email string) string {
	client, ok := s.clientForLink(inbound, email)
	if !ok || client.Password == "" || client.Email == "" {
		return ""
	}
	address := s.resolveInboundAddress(inbound)
	if sni := sidecarSNI(unmarshalStreamSettings(inbound.StreamSettings), address); sni != "" {
		address = sni
	}
	user := url.UserPassword(client.Email, client.Password).String()
	link := fmt.Sprintf("naive+https://%s@%s", user, joinHostPort(address, inbound.Port))
	return buildLinkWithParams(link, nil, s.genRemark(inbound, email, "", "naive"))
}
