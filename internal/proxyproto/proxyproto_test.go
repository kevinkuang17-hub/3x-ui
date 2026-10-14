package proxyproto

import (
	"io"
	"net"
	"testing"
	"time"
)

func serve(t *testing.T, payload string) (net.Addr, string) {
	t.Helper()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := &Listener{Listener: raw, HeaderTimeout: time.Second}
	defer ln.Close()
	go func() {
		c, err := net.Dial("tcp", raw.Addr().String())
		if err != nil {
			return
		}
		_, _ = c.Write([]byte(payload))
		_ = c.Close()
	}()
	c, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	body, _ := io.ReadAll(c)
	return c.RemoteAddr(), string(body)
}

// REALITY forwards visitors from loopback; without the header the panel would treat them all as local.
func TestListenerUsesProxyAddress(t *testing.T) {
	addr, body := serve(t, "PROXY TCP4 203.0.113.9 127.0.0.1 51234 443\r\n\x16\x03\x01hello")
	if addr.String() != "203.0.113.9:51234" {
		t.Fatalf("remote = %s, want the visitor's address", addr)
	}
	if body != "\x16\x03\x01hello" {
		t.Fatalf("body = %q, the TLS bytes after the header must pass through", body)
	}
}

func TestListenerPassesPlainConnections(t *testing.T) {
	addr, body := serve(t, "\x16\x03\x01hello")
	if host, _, _ := net.SplitHostPort(addr.String()); host != "127.0.0.1" {
		t.Fatalf("remote = %s, want the real peer", addr)
	}
	if body != "\x16\x03\x01hello" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseV1(t *testing.T) {
	if a, err := parseV1("PROXY TCP6 2001:db8::1 ::1 4000 443\r\n"); err != nil || a.String() != "[2001:db8::1]:4000" {
		t.Fatalf("tcp6 = %v, %v", a, err)
	}
	for _, bad := range []string{"PROXY UNKNOWN\r\n", "PROXY TCP4 x 127.0.0.1 1 2\r\n", "PROXY TCP4 1.2.3.4 127.0.0.1 99999 2\r\n", "GET / HTTP/1.1\r\n"} {
		if _, err := parseV1(bad); err == nil {
			t.Errorf("parseV1(%q) accepted a bad header", bad)
		}
	}
}
