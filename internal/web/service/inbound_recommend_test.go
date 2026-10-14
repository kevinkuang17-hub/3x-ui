package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func writeTestCert(t *testing.T, dir string, names []string, notAfter time.Time) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func TestCheckCertFile(t *testing.T) {
	now := time.Now()
	valid, validKey := writeTestCert(t, t.TempDir(), []string{"hk.example.com"}, now.Add(60*24*time.Hour))
	expired, expiredKey := writeTestCert(t, t.TempDir(), []string{"hk.example.com"}, now.Add(-time.Hour))
	wildcard, wildcardKey := writeTestCert(t, t.TempDir(), []string{"*.example.com"}, now.Add(60*24*time.Hour))
	garbage := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(garbage, []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, cert, key, domain, want string
	}{
		{"valid", valid, validKey, "hk.example.com", CertStateOK},
		{"expired", expired, expiredKey, "hk.example.com", CertStateExpired},
		{"other name", valid, validKey, "us.example.com", CertStateMismatch},
		{"wildcard covers", wildcard, wildcardKey, "us.example.com", CertStateOK},
		{"no cert set", "", "", "hk.example.com", CertStateMissing},
		{"cert file gone", filepath.Join(t.TempDir(), "x.pem"), validKey, "hk.example.com", CertStateMissing},
		{"key file gone", valid, filepath.Join(t.TempDir(), "x.key"), "hk.example.com", CertStateMissing},
		{"not pem", garbage, validKey, "hk.example.com", CertStateUnreadable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkCertFile(tc.cert, tc.key, tc.domain, now)
			if got.state != tc.want {
				t.Fatalf("state = %q, want %q", got.state, tc.want)
			}
		})
	}
}

func stubRecommendNet(t *testing.T, domainIPs map[string][]string, publicIP string, busyPorts ...int) {
	t.Helper()
	oldLookup, oldPublic, oldFree, oldFw := recommendLookupHost, recommendPublicIPv4, recommendLocalPortFree, recommendFirewall
	t.Cleanup(func() {
		recommendLookupHost, recommendPublicIPv4, recommendLocalPortFree, recommendFirewall = oldLookup, oldPublic, oldFree, oldFw
	})
	recommendFirewall = func() FirewallStatus { return FirewallStatus{} }
	recommendLookupHost = func(_ context.Context, host string) ([]string, error) {
		if ips, ok := domainIPs[host]; ok {
			return ips, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	recommendPublicIPv4 = func() string { return publicIP }
	recommendLocalPortFree = func(port int, udp bool) bool {
		if udp {
			return true
		}
		for _, p := range busyPorts {
			if p == port {
				return false
			}
		}
		return true
	}
}

// A panel with a certificate but no "panel domain" typed in still has a domain: the one on its
// certificate. Missing that sent every such install to Reality.
func TestRecommendLocalPicksTLSFromCertificate(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	settings := SettingService{}
	cert, key := writeTestCert(t, t.TempDir(), []string{"hk.example.com"}, time.Now().Add(60*24*time.Hour))
	for k, v := range map[string]string{"webCertFile": cert, "webKeyFile": key, "webDomain": ""} {
		if err := settings.setString(k, v); err != nil {
			t.Fatal(err)
		}
	}
	stubRecommendNet(t, map[string][]string{"hk.example.com": {"203.0.113.5"}}, "203.0.113.5")

	got, err := (&InboundRecommendService{}).Recommend(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Domain != "hk.example.com" || got.CertState != CertStateOK || !got.PointsHere {
		t.Fatalf("domain/cert/points = %q/%q/%v", got.Domain, got.CertState, got.PointsHere)
	}
	if got.Recommended != RecommendTLS || got.CertFile != cert || got.KeyFile != key || got.TcpPorts[0] != 443 {
		t.Fatalf("recommended %q cert %q key %q ports %v", got.Recommended, got.CertFile, got.KeyFile, got.TcpPorts)
	}
}

// A domain that resolves elsewhere (another box, or a Cloudflare-proxied record) cannot carry a
// direct TLS inbound on this server, however good the certificate is.
func TestRecommendLocalFallsBackToRealityWhenDomainPointsElsewhere(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	settings := SettingService{}
	cert, key := writeTestCert(t, t.TempDir(), []string{"hk.example.com"}, time.Now().Add(60*24*time.Hour))
	for k, v := range map[string]string{"webCertFile": cert, "webKeyFile": key, "webDomain": "hk.example.com"} {
		if err := settings.setString(k, v); err != nil {
			t.Fatal(err)
		}
	}
	stubRecommendNet(t, map[string][]string{"hk.example.com": {"104.21.3.4"}}, "203.0.113.5")

	got, err := (&InboundRecommendService{}).Recommend(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.PointsHere || got.Recommended != RecommendReality {
		t.Fatalf("points=%v recommended=%q, want false/reality", got.PointsHere, got.Recommended)
	}
	if len(got.DomainIPs) != 1 || got.DomainIPs[0] != "104.21.3.4" {
		t.Fatalf("domain IPs = %v", got.DomainIPs)
	}
}

func TestRecommendPortSkipsTakenPorts(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	stubRecommendNet(t, nil, "203.0.113.5", 8443)
	taken := &model.Inbound{
		Remark: "HK A", Port: 443, Protocol: model.VLESS, Tag: "inbound-443", Enable: true,
		Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp","security":"reality"}`,
	}
	if err := database.GetDB().Create(taken).Error; err != nil {
		t.Fatal(err)
	}

	got, err := (&InboundRecommendService{}).Recommend(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// 443 is held by a TCP inbound and 8443 by another program; neither may be offered for TCP.
	if len(got.TcpPorts) != recommendTCPPorts || got.TcpPorts[0] != 2053 || got.TcpPorts[1] != 2083 {
		t.Fatalf("tcp ports = %v, want %d starting 2053, 2083", got.TcpPorts, recommendTCPPorts)
	}
	for _, p := range got.TcpPorts {
		if p == 443 || p == 8443 {
			t.Fatalf("tcp ports %v offer taken port %d", got.TcpPorts, p)
		}
	}
	// The inbound on 443 only holds TCP, so UDP protocols such as Hysteria2 can still use 443.
	if len(got.UdpPorts) != recommendUDPPorts || got.UdpPorts[0] != 443 {
		t.Fatalf("udp ports = %v, want %d starting 443", got.UdpPorts, recommendUDPPorts)
	}
	if got.Recommended != RecommendReality || got.CertState != CertStateMissing {
		t.Fatalf("no domain: recommended %q cert %q", got.Recommended, got.CertState)
	}
}
