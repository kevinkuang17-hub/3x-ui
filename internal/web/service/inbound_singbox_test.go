package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/singbox"
)

const sidecarTestStream = `{"network":"tcp","security":"tls","tlsSettings":{"serverName":"hk.example.com",
	"certificates":[{"certificateFile":"/root/cert/hk/fullchain.pem","keyFile":"/root/cert/hk/privkey.pem"}]}}`

func addSidecarInbound(t *testing.T, protocol model.Protocol, port int, clients string) *model.Inbound {
	t.Helper()
	in := &model.Inbound{
		Tag: "in-" + string(protocol), Enable: true, Port: port, Protocol: protocol,
		Settings: `{"clients":` + clients + `}`, StreamSettings: sidecarTestStream,
	}
	if _, _, err := (&InboundService{}).AddInbound(in); err != nil {
		t.Fatalf("AddInbound: %v", err)
	}
	return in
}

// Xray carries an AnyTLS inbound only as the loopback SOCKS5 relay its traffic leaves through: a
// disabled client must lose both its relay account and its sing-box user, or it keeps working.
func TestSidecarInboundServedBySingboxAndRelayedThroughXray(t *testing.T) {
	setupConflictDB(t)
	in := addSidecarInbound(t, model.AnyTLS, 8443, `[
		{"email":"alice@hk","password":"pa","enable":true},
		{"email":"bob@hk","password":"pb","enable":false}]`)

	cfg, err := (&XrayService{}).GetXrayConfig()
	if err != nil {
		t.Fatalf("GetXrayConfig: %v", err)
	}
	var relay map[string]any
	for i := range cfg.InboundConfigs {
		if cfg.InboundConfigs[i].Tag == in.Tag {
			raw, _ := json.Marshal(cfg.InboundConfigs[i])
			_ = json.Unmarshal(raw, &relay)
		}
	}
	if relay == nil {
		t.Fatal("no Xray relay inbound for the AnyTLS inbound")
	}
	if relay["protocol"] != "socks" || relay["listen"] != "127.0.0.1" || int(relay["port"].(float64)) != singbox.RelayPortForInbound(in.Id) {
		t.Fatalf("relay = %v", relay)
	}
	accounts, _ := json.Marshal(relay["settings"].(map[string]any)["accounts"])
	if want := `[{"pass":"` + singbox.RelayPassword() + `","user":"alice@hk"}]`; string(accounts) != want {
		t.Fatalf("relay accounts %s, want %s", accounts, want)
	}
	assertXrayAccepts(t, "the AnyTLS relay inbound", buildGoldenInbound(t, relay))

	insts, err := (&InboundService{}).DesiredSingboxInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(insts) != 1 || insts[0].Protocol != "anytls" || insts[0].CertFile != "/root/cert/hk/fullchain.pem" ||
		insts[0].ServerName != "hk.example.com" || len(insts[0].Users) != 1 || insts[0].Users[0] != (singbox.User{Name: "alice@hk", Password: "pa"}) {
		t.Fatalf("sing-box instances %+v", insts)
	}
}

// NaiveProxy logs in with the email over Basic auth, so sing-box refuses the inbound over a ":".
func TestCreateClient_RefusesColonEmailOnNaive(t *testing.T) {
	setupConflictDB(t)
	in := addSidecarInbound(t, model.Naive, 8444, `[{"email":"ivy","password":"pi","enable":true}]`)
	_, err := (&ClientService{}).Create(&InboundService{}, &ClientCreatePayload{
		Client:     model.Client{Email: "tg:42", Enable: true},
		InboundIds: []int{in.Id},
	})
	if want := `NaiveProxy: client email "tg:42" cannot contain ":"`; err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Create err = %v, want %q", err, want)
	}
}

// The relay port is no row's port, so only this check keeps a loopback inbound off it.
func TestPortConflict_SidecarRelayPort(t *testing.T) {
	setupConflictDB(t)
	in := addSidecarInbound(t, model.AnyTLS, 8443, `[{"email":"alice@hk","password":"pa","enable":true}]`)
	clash := &model.Inbound{
		Tag: "in-socks", Enable: true, Listen: "127.0.0.1", Port: singbox.RelayPortForInbound(in.Id),
		Protocol: model.Mixed, Settings: `{"auth":"noauth"}`,
	}
	conflict, err := (&InboundService{}).checkPortConflict(clash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if conflict == nil || conflict.InboundID != in.Id || !conflict.Relay {
		t.Fatalf("conflict = %+v, want the AnyTLS inbound's relay", conflict)
	}
}
