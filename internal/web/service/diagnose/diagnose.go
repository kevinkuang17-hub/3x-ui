// Package diagnose checks why an inbound cannot be used: it gathers what the panel, the hosting
// server and a real client connection see, and turns that into findings with a cause and a fix.
package diagnose

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/linehealth"
	"github.com/mhsanaei/3x-ui/v3/internal/singbox"
	"github.com/mhsanaei/3x-ui/v3/internal/sub"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/outbound"
)

const (
	LevelOK    = "ok"
	LevelWarn  = "warn"
	LevelError = "error"

	FixEnable   = "enable"
	FixFirewall = "firewall"
	FixClients  = "clients"
)

// Finding is one check result; Key names its text (pages.inbounds.diagnose.find.<key>).
type Finding struct {
	Key    string         `json:"key" example:"firewallBlocked"`
	Level  string         `json:"level" example:"error"`
	Params map[string]any `json:"params,omitempty"`
	// Fix is the one-click repair the panel offers: enable, firewall or clients.
	Fix string `json:"fix,omitempty" example:"firewall"`
}

// Report is the diagnosis of one inbound; Ok means a real client connection through it worked.
type Report struct {
	InboundId int       `json:"inboundId" example:"7"`
	Remark    string    `json:"remark" example:"US A Hy2"`
	Protocol  string    `json:"protocol" example:"hysteria"`
	Port      int       `json:"port" example:"443"`
	NodeId    *int      `json:"nodeId,omitempty" example:"2"`
	Address   string    `json:"address" example:"us.example.com"`
	Ok        bool      `json:"ok" example:"false"`
	DelayMs   int64     `json:"delayMs" example:"0"`
	Findings  []Finding `json:"findings"`
}

// portFact is what the hosting server reported about one protocol of the inbound's port.
type portFact struct {
	proto string
	check *service.PortCheck
	err   string
}

// facts is everything gathered about one inbound; assess turns it into findings.
type facts struct {
	enabled     bool
	nodeName    string
	nodeOffline bool
	xrayDown    bool
	xrayErr     string
	noClient    bool
	noClientCfg string
	ports       []portFact
	address     string
	addrPort    int
	resolves    *bool
	tcpReach    *bool
	tcpErr      string
	certState   string
	certDate    string
	e2e         *outbound.TestOutboundResult
	// sidecar marks AnyTLS/NaiveProxy, served by sing-box; sidecarErr is why it is not running.
	sidecar    bool
	sidecarErr string
}

func errorFinding(key string, params map[string]any, fix string) Finding {
	return Finding{Key: key, Level: LevelError, Params: params, Fix: fix}
}

// assess explains the facts in the order a fault blocks the next check: a disabled inbound or an
// offline node makes every later check meaningless.
func assess(f facts) ([]Finding, bool) {
	if !f.enabled {
		return []Finding{errorFinding("disabled", nil, FixEnable)}, false
	}
	if f.nodeOffline {
		return []Finding{errorFinding("nodeOffline", map[string]any{"node": f.nodeName}, "")}, false
	}
	var out []Finding
	if f.xrayDown {
		out = append(out, errorFinding("xrayDown", map[string]any{"error": f.xrayErr}, ""))
	}
	if f.sidecar && f.sidecarErr != "" {
		out = append(out, errorFinding("sidecarDown", map[string]any{"error": f.sidecarErr}, ""))
	} else if f.sidecar {
		out = append(out, Finding{Key: "sidecarRunning", Level: LevelOK})
	}
	if f.noClient {
		out = append(out, errorFinding("noClient", nil, FixClients))
	}
	if f.noClientCfg != "" {
		out = append(out, Finding{Key: "noClientConfig", Level: LevelWarn, Params: map[string]any{"error": f.noClientCfg}})
	}
	serverSide := false
	for _, p := range f.ports {
		params := map[string]any{"port": portOf(p), "proto": strings.ToUpper(p.proto)}
		switch {
		case p.err != "":
			params["error"] = p.err
			out = append(out, Finding{Key: "portCheckFailed", Level: LevelWarn, Params: params})
		case p.check.Bound != nil && !*p.check.Bound:
			out = append(out, errorFinding("notListening", params, ""))
			serverSide = true
		case !p.check.Allowed:
			out = append(out, errorFinding("firewallBlocked", params, FixFirewall))
			serverSide = true
		default:
			out = append(out, Finding{Key: "portOpen", Level: LevelOK, Params: params})
		}
	}
	if f.resolves != nil && !*f.resolves {
		out = append(out, errorFinding("dnsNone", map[string]any{"address": f.address}, ""))
	}
	if f.tcpReach != nil {
		params := map[string]any{"address": f.address, "port": f.addrPort}
		if *f.tcpReach {
			out = append(out, Finding{Key: "reachable", Level: LevelOK, Params: params})
		} else if !serverSide {
			params["error"] = f.tcpErr
			// The server listens and its firewall lets traffic in, so something in between drops it.
			out = append(out, errorFinding("unreachable", params, ""))
		}
	}
	switch f.certState {
	case service.CertStateExpired:
		out = append(out, errorFinding("certExpired", map[string]any{"date": f.certDate}, ""))
	case service.CertStateMismatch:
		out = append(out, errorFinding("certMismatch", map[string]any{"address": f.address}, ""))
	}
	// No Xray client exists for a sidecar protocol, so a clean run of the other checks is the verdict.
	ok := f.sidecar && f.e2e == nil && !hasError(out) && f.tcpReach != nil && *f.tcpReach
	switch {
	case f.e2e == nil:
	case f.e2e.Success:
		ok = true
		out = append(out, Finding{Key: "e2eOk", Level: LevelOK, Params: map[string]any{"delay": f.e2e.Delay}})
	default:
		params := map[string]any{"error": f.e2e.Error}
		key := "e2eFailed"
		if !hasError(out) {
			// Everything around the inbound is fine, so the connection itself is refused.
			key = "e2eFailedOnly"
		}
		out = append(out, errorFinding(key, params, ""))
	}
	return out, ok
}

func portOf(p portFact) int {
	if p.check != nil {
		return p.check.Port
	}
	return 0
}

func hasError(fs []Finding) bool {
	for _, f := range fs {
		if f.Level == LevelError {
			return true
		}
	}
	return false
}

// Service runs diagnoses; host is the panel address used when an inbound shares its listen address.
type Service struct {
	nodes     service.NodeService
	xray      service.XrayService
	protect   service.ProtectService
	outbounds outbound.OutboundService
	links     *sub.LinkProvider
}

func NewService() *Service {
	return &Service{links: sub.NewLinkProvider()}
}

const diagnoseConcurrency = 8

// Diagnose checks the given inbounds, or every inbound when ids is empty.
func (s *Service) Diagnose(ctx context.Context, host string, ids []int) ([]*Report, error) {
	var inbounds []*model.Inbound
	q := database.GetDB().Order("id ASC")
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	if err := q.Find(&inbounds).Error; err != nil {
		return nil, err
	}
	nodes := map[int]*model.Node{}
	if all, err := s.nodes.GetAll(); err == nil {
		for _, n := range all {
			nodes[n.Id] = n
		}
	}
	reports := make([]*Report, len(inbounds))
	allFacts := make([]facts, len(inbounds))
	outbounds := make([]map[string]any, len(inbounds))
	var wg sync.WaitGroup
	sem := make(chan struct{}, diagnoseConcurrency)
	for i, ib := range inbounds {
		wg.Add(1)
		go func(i int, ib *model.Inbound) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var node *model.Node
			if ib.NodeID != nil {
				node = nodes[*ib.NodeID]
			}
			reports[i] = &Report{InboundId: ib.Id, Remark: ib.Remark, Protocol: string(ib.Protocol), Port: ib.Port, NodeId: ib.NodeID}
			allFacts[i], outbounds[i] = s.gather(ctx, host, ib, node)
		}(i, ib)
	}
	wg.Wait()

	s.runClientTests(inbounds, allFacts, outbounds)
	for i := range inbounds {
		reports[i].Address = allFacts[i].address
		reports[i].Findings, reports[i].Ok = assess(allFacts[i])
		if allFacts[i].e2e != nil {
			reports[i].DelayMs = allFacts[i].e2e.Delay
		}
	}
	return reports, nil
}

// gather collects the facts of one inbound and the client outbound to test it with.
func (s *Service) gather(ctx context.Context, host string, ib *model.Inbound, node *model.Node) (facts, map[string]any) {
	f := facts{enabled: ib.Enable}
	if !ib.Enable {
		return f, nil
	}
	if ib.NodeID != nil {
		if node == nil || !node.Enable || node.Status != "online" {
			f.nodeOffline = true
			if node != nil {
				f.nodeName = node.Name
			}
			return f, nil
		}
		f.nodeName = node.Name
		if node.XrayState != "" && node.XrayState != "running" {
			f.xrayDown, f.xrayErr = true, node.XrayError
		}
	} else if !s.xray.IsXrayRunning() {
		f.xrayDown = true
		if err := s.xray.GetXrayErr(); err != nil {
			f.xrayErr = err.Error()
		}
	}

	for _, proto := range protocolsOf(ib) {
		f.ports = append(f.ports, s.checkPort(ctx, ib, node, proto))
	}

	ob, _, err := s.links.ClientOutbound(host, ib)
	f.sidecar = ib.Protocol == model.AnyTLS || ib.Protocol == model.Naive
	switch {
	case errors.Is(err, sub.ErrNoEnabledClient):
		f.noClient = true
	case err != nil && !f.sidecar:
		f.noClientCfg = err.Error()
	}
	if ob != nil {
		f.address, f.addrPort = outboundEndpoint(ob)
	}
	sidecarSNI := ""
	if f.sidecar {
		if node == nil {
			if st := singbox.GetManager().Status(); !st.Running {
				f.sidecarErr = st.Error
				if f.sidecarErr == "" {
					f.sidecarErr = "sing-box is not running"
				}
			}
		}
		f.address, sidecarSNI = s.links.SidecarEndpoint(host, ib)
		f.addrPort = ib.Port
	}
	if f.address != "" {
		if net.ParseIP(f.address) == nil {
			lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, lerr := net.DefaultResolver.LookupHost(lctx, f.address)
			cancel()
			resolves := lerr == nil
			f.resolves = &resolves
		}
		if !linehealth.IsUDP(ib) && f.addrPort > 0 && (f.resolves == nil || *f.resolves) {
			reach, rerr := dialTCP(f.address, f.addrPort)
			f.tcpReach = &reach
			if rerr != nil {
				f.tcpErr = rerr.Error()
			}
			if reach && f.sidecar {
				f.certState, f.certDate = checkTLS(f.address, f.addrPort, cmp.Or(sidecarSNI, f.address))
			} else if reach && securityOf(ob) == "tls" {
				f.certState, f.certDate = checkTLS(f.address, f.addrPort, tlsServerName(ob, f.address))
			}
		}
	}
	return f, ob
}

// protocolsOf lists which transports the inbound's port must be open for.
func protocolsOf(ib *model.Inbound) []string {
	rules, _ := service.FirewallRulesFor(ib)
	protos := make([]string, 0, len(rules))
	for _, r := range rules {
		protos = append(protos, r.Proto)
	}
	return protos
}

func (s *Service) checkPort(ctx context.Context, ib *model.Inbound, node *model.Node, proto string) portFact {
	pf := portFact{proto: proto}
	if node == nil {
		pc, err := s.protect.CheckPort(ib.Port, proto)
		if err != nil {
			pf.err = err.Error()
		}
		pf.check = pc
		return pf
	}
	mgr := runtime.GetManager()
	if mgr == nil {
		pf.err = "runtime manager unavailable"
		return pf
	}
	remote, err := mgr.RemoteFor(node)
	if err != nil {
		pf.err = err.Error()
		return pf
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, err := remote.ProtectCall(cctx, "POST", "portcheck", map[string]any{"port": ib.Port, "proto": proto})
	if err != nil {
		// Nodes older than this feature have no portcheck endpoint.
		pf.err = err.Error()
		return pf
	}
	var pc service.PortCheck
	if err := json.Unmarshal(raw, &pc); err != nil {
		pf.err = err.Error()
		return pf
	}
	pf.check = &pc
	return pf
}

// runClientTests connects through every testable inbound at once, the way a client app would.
func (s *Service) runClientTests(inbounds []*model.Inbound, all []facts, outbounds []map[string]any) {
	var batch []any
	var idx []int
	for i, ob := range outbounds {
		if ob == nil || !all[i].enabled || all[i].nodeOffline {
			continue
		}
		ob["tag"] = "diag-" + strconv.Itoa(inbounds[i].Id)
		batch = append(batch, ob)
		idx = append(idx, i)
	}
	if len(batch) == 0 {
		return
	}
	for start := 0; start < len(batch); start += clientTestBatch {
		end := min(start+clientTestBatch, len(batch))
		results, err := s.testBatch(batch[start:end])
		for k, i := range idx[start:end] {
			switch {
			case err != nil:
				all[i].e2e = &outbound.TestOutboundResult{Error: err.Error()}
			case k < len(results):
				all[i].e2e = results[k]
			}
		}
	}
}

// clientTestBatch matches the outbound tester's per-request limit; each batch is one temporary Xray.
const clientTestBatch = 50

func (s *Service) testBatch(batch []any) ([]*outbound.TestOutboundResult, error) {
	raw, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	return s.outbounds.TestOutbounds(string(raw), "", "", "http")
}

// outboundEndpoint finds the server address and port a client outbound connects to.
func outboundEndpoint(ob map[string]any) (string, int) {
	settings, _ := ob["settings"].(map[string]any)
	if settings == nil {
		return "", 0
	}
	for _, key := range []string{"vnext", "servers", "peers"} {
		if list, ok := settings[key].([]any); ok && len(list) > 0 {
			if first, ok := list[0].(map[string]any); ok {
				if addr, port := addressPort(first); addr != "" {
					return addr, port
				}
			}
		}
	}
	return addressPort(settings)
}

func addressPort(m map[string]any) (string, int) {
	addr, _ := m["address"].(string)
	if addr == "" {
		if ep, ok := m["endpoint"].(string); ok {
			if h, p, err := net.SplitHostPort(ep); err == nil {
				port, _ := strconv.Atoi(p)
				return h, port
			}
		}
	}
	port := 0
	switch v := m["port"].(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	}
	return addr, port
}

func securityOf(ob map[string]any) string {
	stream, _ := ob["streamSettings"].(map[string]any)
	sec, _ := stream["security"].(string)
	return sec
}

func tlsServerName(ob map[string]any, fallback string) string {
	stream, _ := ob["streamSettings"].(map[string]any)
	tlsSettings, _ := stream["tlsSettings"].(map[string]any)
	if sn, _ := tlsSettings["serverName"].(string); sn != "" {
		return sn
	}
	return fallback
}

var dialTCP = func(host string, port int) (bool, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		return false, err
	}
	_ = c.Close()
	return true, nil
}

// checkTLS reads the certificate the inbound presents; only expiry and name are judged.
var checkTLS = func(host string, port int, serverName string) (string, string) {
	d := &net.Dialer{Timeout: 5 * time.Second}
	c, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(host, strconv.Itoa(port)), &tls.Config{ServerName: serverName, InsecureSkipVerify: true}) //nolint:gosec
	if err != nil {
		return "", ""
	}
	defer c.Close()
	certs := c.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", ""
	}
	leaf := certs[0]
	date := leaf.NotAfter.Format("2006-01-02")
	if time.Now().After(leaf.NotAfter) {
		return service.CertStateExpired, date
	}
	if leaf.VerifyHostname(serverName) != nil {
		return service.CertStateMismatch, date
	}
	return service.CertStateOK, date
}
