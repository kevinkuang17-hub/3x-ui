package service

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPasskeyRelyingParty(t *testing.T) {
	ip := httptest.NewRequest(http.MethodGet, "https://203.0.113.5:2053/", nil)
	ip.TLS = &tls.ConnectionState{}
	if _, _, err := RelyingParty(ip, false); err == nil {
		t.Fatal("IP hosts cannot be a WebAuthn relying party")
	}
	plain := httptest.NewRequest(http.MethodGet, "http://panel.example.com:2053/", nil)
	if _, _, err := RelyingParty(plain, false); err == nil {
		t.Fatal("plain http must be refused")
	}
	if _, rp, err := RelyingParty(plain, true); err != nil || rp != "panel.example.com" {
		t.Fatalf("https via a trusted proxy: rp=%q err=%v", rp, err)
	}
	secure := httptest.NewRequest(http.MethodGet, "https://panel.example.com:2053/", nil)
	secure.TLS = &tls.ConnectionState{}
	if w, rp, err := RelyingParty(secure, false); err != nil || w == nil || rp != "panel.example.com" {
		t.Fatalf("rp=%q err=%v", rp, err)
	}
}

func TestPasskeyFlowsAreSingleUse(t *testing.T) {
	id, err := storeFlow(passkeyFlow{userID: 7, rpID: "panel.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	f, ok := takeFlow(id)
	if !ok || f.userID != 7 {
		t.Fatal("stored flow must be returned once")
	}
	if _, ok := takeFlow(id); ok {
		t.Fatal("a flow must not be usable twice")
	}
	if _, ok := takeFlow("unknown"); ok {
		t.Fatal("unknown flows must fail")
	}
}
