package service

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/singbox"
	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

var singboxProtocols = []model.Protocol{model.AnyTLS, model.Naive}

func isSingboxProtocol(p model.Protocol) bool {
	return p == model.AnyTLS || p == model.Naive
}

// singboxSyncHook applies sidecar changes right away; the web server sets it to the sing-box job.
var singboxSyncHook func()

func SetSingboxSyncHook(f func()) { singboxSyncHook = f }

// TriggerSingboxSync asks for an immediate sidecar update instead of waiting for the next tick.
func TriggerSingboxSync() {
	if f := singboxSyncHook; f != nil {
		go f()
	}
}

// sidecarUsers is who may use a sidecar inbound now: enabled, within traffic and time limits, and
// holding a password. Both sing-box's user list and the Xray relay's accounts come from it.
func (s *InboundService) sidecarUsers(db *gorm.DB, inbound *model.Inbound) ([]singbox.User, error) {
	clients, err := s.clientService.ListForInbound(db, inbound.Id)
	if err != nil {
		return nil, err
	}
	emails := make([]string, 0, len(clients))
	for _, c := range clients {
		emails = append(emails, c.Email)
	}
	disabled, err := trafficDisabledEmails(db, emails)
	if err != nil {
		return nil, err
	}
	users := make([]singbox.User, 0, len(clients))
	for _, c := range clients {
		if _, off := disabled[c.Email]; off || !c.Enable || c.Password == "" || c.Email == "" {
			continue
		}
		users = append(users, singbox.User{Name: c.Email, Password: c.Password})
	}
	return users, nil
}

// sidecarTLS reads the certificate the inbound's TLS settings name; sing-box needs files.
func sidecarTLS(streamSettings string) (serverName, certFile, keyFile string) {
	var stream struct {
		TLSSettings struct {
			ServerName   string `json:"serverName"`
			Certificates []struct {
				CertificateFile string `json:"certificateFile"`
				KeyFile         string `json:"keyFile"`
			} `json:"certificates"`
		} `json:"tlsSettings"`
	}
	_ = json.Unmarshal([]byte(streamSettings), &stream)
	tls := stream.TLSSettings
	if len(tls.Certificates) > 0 {
		certFile, keyFile = strings.TrimSpace(tls.Certificates[0].CertificateFile), strings.TrimSpace(tls.Certificates[0].KeyFile)
	}
	return strings.TrimSpace(tls.ServerName), certFile, keyFile
}

// DesiredSingboxInstances lists the AnyTLS and NaiveProxy inbounds this server must serve.
func (s *InboundService) DesiredSingboxInstances() ([]singbox.Instance, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	if err := db.Where("protocol IN ? AND enable = ? AND node_id IS NULL", singboxProtocols, true).
		Order("id").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	out := make([]singbox.Instance, 0, len(inbounds))
	for _, ib := range inbounds {
		serverName, certFile, keyFile := sidecarTLS(ib.StreamSettings)
		if certFile == "" || keyFile == "" {
			logger.Warning("sing-box: inbound", ib.Tag, "has no certificate file, skipping it")
			continue
		}
		users, err := s.sidecarUsers(db, ib)
		if err != nil {
			return nil, err
		}
		if len(users) == 0 {
			continue
		}
		out = append(out, singbox.Instance{
			Id: ib.Id, Tag: ib.Tag, Protocol: string(ib.Protocol), Listen: ib.Listen, Port: ib.Port,
			ServerName: serverName, CertFile: certFile, KeyFile: keyFile, Users: users,
		})
	}
	return out, nil
}

const singboxRelaySniffing = `{"enabled":true,"destOverride":["http","tls","quic"],"routeOnly":true}`

// singboxRelayInbound is the Xray side of a sidecar inbound: SOCKS5 on loopback with one account
// per active client, so Xray meters, limits and routes each one by email under the inbound's tag.
func (s *InboundService) singboxRelayInbound(inbound *model.Inbound) (*xray.InboundConfig, error) {
	users, err := s.sidecarUsers(database.GetDB(), inbound)
	if err != nil || len(users) == 0 {
		return nil, err
	}
	type account struct {
		User string `json:"user"`
		Pass string `json:"pass"`
	}
	settings := struct {
		Auth     string    `json:"auth"`
		UDP      bool      `json:"udp"`
		Accounts []account `json:"accounts"`
	}{Auth: "password", UDP: true}
	for _, u := range users {
		settings.Accounts = append(settings.Accounts, account{User: u.Name, Pass: singbox.RelayPassword()})
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	return &xray.InboundConfig{
		Listen:   json_util.RawMessage(`"127.0.0.1"`),
		Port:     singbox.RelayPortForInbound(inbound.Id),
		Protocol: "socks",
		Settings: json_util.RawMessage(raw),
		Sniffing: json_util.RawMessage(singboxRelaySniffing),
		Tag:      inbound.Tag,
	}, nil
}
