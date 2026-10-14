package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
)

const (
	CertStateOK         = "ok"
	CertStateMissing    = "missing"
	CertStateUnreadable = "unreadable"
	CertStateExpired    = "expired"
	CertStateMismatch   = "mismatch"
	// CertStateUnverified is a node whose certificate files exist but could not be inspected.
	CertStateUnverified = "unverified"

	RecommendTLS     = "tls"
	RecommendReality = "reality"
)

// InboundRecommendation is what the one-click inbound dialog knows about a server: whether a
// domain with a working certificate points at it (VLESS TLS) or not (VLESS Reality), and a free port.
type InboundRecommendation struct {
	NodeId     *int     `json:"nodeId,omitempty" example:"3"`
	Domain     string   `json:"domain" example:"hk.example.com"`
	DomainIPs  []string `json:"domainIps"`
	ServerIPs  []string `json:"serverIps"`
	PointsHere bool     `json:"pointsHere" example:"true"`
	CertFile   string   `json:"certFile" example:"/root/cert/hk.example.com/fullchain.pem"`
	KeyFile    string   `json:"keyFile" example:"/root/cert/hk.example.com/privkey.pem"`
	CertState  string   `json:"certState" example:"ok"`
	// CertNotAfter is the certificate expiry in Unix milliseconds, 0 when unknown.
	CertNotAfter int64 `json:"certNotAfter" example:"1767225600000"`
	// TcpPorts and UdpPorts are free ports, preferred ones (443 first) before random ones.
	TcpPorts    []int  `json:"tcpPorts"`
	UdpPorts    []int  `json:"udpPorts"`
	Recommended string `json:"recommended" example:"tls"`
	// Firewall is "ufw" when ufw filters this server, so new inbound ports must be opened in it.
	Firewall string `json:"firewall" example:"ufw"`
	// RealityTarget is the loopback website REALITY should front, when it runs behind REALITY.
	RealityTarget string `json:"realityTarget" example:"127.0.0.1:24873"`
	// DecoyReady says a decoy website is deployed here, so REALITY can be put in front of it.
	DecoyReady bool `json:"decoyReady" example:"true"`
}

type InboundRecommendService struct {
	settings SettingService
	nodes    NodeService
	inbounds InboundService
}

// recommendPorts are tried in order: 443 first, then the other HTTPS ports Cloudflare proxies.
var recommendPorts = []int{443, 8443, 2053, 2083, 2087, 2096}

const (
	recommendTCPPorts = 8
	recommendUDPPorts = 3
)

var (
	recommendLookupHost = net.DefaultResolver.LookupHost
	recommendPublicIPv4 = cachedPublicIPv4
	recommendFirewall   = func() FirewallStatus { return (&ProtectService{}).Firewall() }
	// recommendLocalPortFree binds the port briefly: the panel, nginx or a decoy may hold it.
	recommendLocalPortFree = func(port int, udp bool) bool {
		if udp {
			c, err := net.ListenPacket("udp", ":"+strconv.Itoa(port))
			if err != nil {
				return false
			}
			_ = c.Close()
			return true
		}
		l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
		if err != nil {
			return false
		}
		_ = l.Close()
		return true
	}
	// recommendRemotePortOpen reports whether something on a node already answers on the port.
	recommendRemotePortOpen = func(host string, port int) bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}
	recommendPeerCert = func(host string, port int, sni string) (*x509.Certificate, error) {
		d := &net.Dialer{Timeout: 5 * time.Second}
		// Only the presented certificate is read; whether it is trusted is judged by checkCert.
		c, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{ServerName: sni, InsecureSkipVerify: true}) //nolint:gosec
		if err != nil {
			return nil, err
		}
		defer c.Close()
		certs := c.ConnectionState().PeerCertificates
		if len(certs) == 0 {
			return nil, errors.New("no certificate")
		}
		return certs[0], nil
	}
)

var publicIPv4Cache struct {
	sync.Mutex
	ip string
	at time.Time
}

func cachedPublicIPv4() string {
	publicIPv4Cache.Lock()
	defer publicIPv4Cache.Unlock()
	if publicIPv4Cache.ip == "" || time.Since(publicIPv4Cache.at) > time.Hour {
		if ip := firstPublicIP(publicIPv4Services); ip != "N/A" {
			publicIPv4Cache.ip, publicIPv4Cache.at = ip, time.Now()
		}
	}
	return publicIPv4Cache.ip
}

type certCheck struct {
	state    string
	notAfter int64
	names    []string
}

func checkCert(leaf *x509.Certificate, domain string, now time.Time) certCheck {
	res := certCheck{state: CertStateOK, notAfter: leaf.NotAfter.UnixMilli(), names: leaf.DNSNames}
	switch {
	case now.After(leaf.NotAfter):
		res.state = CertStateExpired
	case domain != "" && leaf.VerifyHostname(domain) != nil:
		res.state = CertStateMismatch
	}
	return res
}

func checkCertFile(certFile, keyFile, domain string, now time.Time) certCheck {
	if certFile == "" || keyFile == "" {
		return certCheck{state: CertStateMissing}
	}
	if _, err := os.Stat(keyFile); err != nil {
		return certCheck{state: CertStateMissing}
	}
	raw, err := os.ReadFile(certFile)
	if err != nil {
		return certCheck{state: CertStateMissing}
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return certCheck{state: CertStateUnreadable}
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return certCheck{state: CertStateUnreadable}
	}
	return checkCert(leaf, domain, now)
}

// domainFromNames picks the first concrete name; a wildcard says which zone, not which host.
func domainFromNames(names []string) string {
	for _, n := range names {
		if n != "" && !strings.HasPrefix(n, "*") {
			return strings.ToLower(n)
		}
	}
	return ""
}

// Recommend inspects this panel's server, or the node nodeID, for the one-click inbound dialog.
func (s *InboundRecommendService) Recommend(ctx context.Context, nodeID *int) (*InboundRecommendation, error) {
	var rec *InboundRecommendation
	var err error
	if nodeID == nil {
		rec = s.recommendLocal(ctx)
	} else if rec, err = s.recommendNode(ctx, *nodeID); err != nil {
		return nil, err
	}
	if rec.DomainIPs == nil {
		rec.DomainIPs = []string{}
	}
	if rec.ServerIPs == nil {
		rec.ServerIPs = []string{}
	}
	if rec.Domain != "" && rec.CertState == CertStateOK && rec.PointsHere {
		rec.Recommended = RecommendTLS
	} else {
		rec.Recommended = RecommendReality
	}
	return rec, nil
}

func (s *InboundRecommendService) recommendLocal(ctx context.Context) *InboundRecommendation {
	webDomain, _ := s.settings.GetWebDomain()
	webCert, _ := s.settings.GetCertFile()
	webKey, _ := s.settings.GetKeyFile()
	subDomain, _ := s.settings.GetSubDomain()
	subCert, _ := s.settings.GetSubCertFile()
	subKey, _ := s.settings.GetSubKeyFile()
	now := time.Now()

	type candidate struct{ domain, cert, key string }
	rec := &InboundRecommendation{CertState: CertStateMissing, Domain: firstNonEmpty(webDomain, subDomain)}
	var fallback *InboundRecommendation
	for _, c := range []candidate{{webDomain, webCert, webKey}, {subDomain, subCert, subKey}} {
		if c.cert == "" {
			continue
		}
		check := checkCertFile(c.cert, c.key, c.domain, now)
		domain := c.domain
		if domain == "" {
			domain = domainFromNames(check.names)
		}
		got := &InboundRecommendation{
			Domain: domain, CertFile: c.cert, KeyFile: c.key, CertState: check.state, CertNotAfter: check.notAfter,
		}
		if check.state == CertStateOK && domain != "" {
			rec = got
			fallback = nil
			break
		}
		if fallback == nil {
			fallback = got
		}
	}
	if fallback != nil {
		rec = fallback
	}

	rec.ServerIPs = localServerIPs()
	if rec.Domain != "" {
		rec.DomainIPs, _ = recommendLookupHost(ctx, rec.Domain)
		rec.PointsHere = overlaps(rec.DomainIPs, rec.ServerIPs)
	}
	if fw := recommendFirewall(); fw.Active {
		rec.Firewall = fw.Tool
	}
	if mode, _ := s.settings.getString("decoyMode"); mode != "" && mode != decoy.ModeOff {
		rec.DecoyReady = true
	}
	if protect := (&ProtectService{}); protect.FrontLoopback() && CurrentFrontStatus().Listening {
		rec.RealityTarget = CurrentFrontStatus().Addr
	}
	rec.TcpPorts = s.freePorts(nil, false, recommendTCPPorts, func(p int) bool { return recommendLocalPortFree(p, false) })
	rec.UdpPorts = s.freePorts(nil, true, recommendUDPPorts, func(p int) bool { return recommendLocalPortFree(p, true) })
	return rec
}

func (s *InboundRecommendService) recommendNode(ctx context.Context, nodeID int) (*InboundRecommendation, error) {
	n, err := s.nodes.GetById(nodeID)
	if err != nil || n == nil {
		return nil, errors.New("node not found")
	}
	if !n.Enable {
		return nil, errors.New("node is disabled")
	}
	rec := &InboundRecommendation{NodeId: &nodeID, CertState: CertStateMissing}
	if net.ParseIP(n.Address) == nil {
		rec.Domain = strings.ToLower(n.Address)
	}
	if files, err := s.nodes.GetWebCertFiles(nodeID); err == nil && files.WebCertFile != "" && files.WebKeyFile != "" {
		rec.CertFile, rec.KeyFile, rec.CertState = files.WebCertFile, files.WebKeyFile, CertStateUnverified
	}
	// The node panel serves its own web certificate, which is what these files hold.
	if rec.CertState == CertStateUnverified {
		if leaf, err := recommendPeerCert(n.Address, n.Port, rec.Domain); err == nil {
			if rec.Domain == "" {
				rec.Domain = domainFromNames(leaf.DNSNames)
			}
			check := checkCert(leaf, rec.Domain, time.Now())
			rec.CertState, rec.CertNotAfter = check.state, check.notAfter
		}
	}
	rec.ServerIPs, _ = recommendLookupHost(ctx, n.Address)
	if rec.Domain != "" {
		rec.DomainIPs, _ = recommendLookupHost(ctx, rec.Domain)
		rec.PointsHere = strings.EqualFold(rec.Domain, n.Address) || overlaps(rec.DomainIPs, rec.ServerIPs)
	}
	open := remoteOpenPorts(n.Address)
	rec.TcpPorts = s.freePorts(&nodeID, false, recommendTCPPorts, func(p int) bool { return !open[p] })
	// UDP gives no answer to probe, so on a node only the panel's own inbounds can rule a port out.
	rec.UdpPorts = s.freePorts(&nodeID, true, recommendUDPPorts, func(int) bool { return true })
	return rec, nil
}

// remoteOpenPorts dials the preferred ports at once, so filtered ports cost one timeout, not six.
func remoteOpenPorts(host string) map[int]bool {
	open := make(map[int]bool, len(recommendPorts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range recommendPorts {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			if recommendRemotePortOpen(host, port) {
				mu.Lock()
				open[port] = true
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	return open
}

// freePorts lists up to n ports that no inbound on the server and no other program holds,
// preferred ports first; random ports are probed only for the local server.
func (s *InboundRecommendService) freePorts(nodeID *int, udp bool, n int, systemFree func(int) bool) []int {
	probe := func(port int) *model.Inbound {
		if udp {
			return &model.Inbound{Port: port, Protocol: model.Hysteria, NodeID: nodeID, Settings: `{"clients":[]}`, StreamSettings: `{"network":"hysteria","security":"tls"}`}
		}
		return &model.Inbound{Port: port, Protocol: model.VLESS, NodeID: nodeID, Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp","security":"tls"}`}
	}
	free := func(port int) bool {
		if conflict, err := s.inbounds.checkPortConflict(probe(port), 0); err != nil || conflict != nil {
			return false
		}
		return systemFree(port)
	}
	ports := []int{}
	for _, p := range recommendPorts {
		if len(ports) < n && free(p) {
			ports = append(ports, p)
		}
	}
	for tries := 0; len(ports) < n && tries < 200; tries++ {
		if p := 10000 + rand.IntN(50000); !slices.Contains(ports, p) && free(p) {
			ports = append(ports, p)
		}
	}
	return ports
}

func localServerIPs() []string {
	var ips []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() && !ipnet.IP.IsPrivate() {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	// Cloud servers often sit behind NAT, so the interface never carries the public address.
	if ip := recommendPublicIPv4(); ip != "" && !slices.Contains(ips, ip) {
		ips = append(ips, ip)
	}
	return ips
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		ipx := net.ParseIP(x)
		for _, y := range b {
			if ipx != nil && ipx.Equal(net.ParseIP(y)) {
				return true
			}
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
