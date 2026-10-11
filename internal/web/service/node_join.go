package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/util/netsafe"
)

const nodeJoinTTL = 24 * time.Hour

var countryRe = regexp.MustCompile(`^[A-Za-z]{2}$`)

// joinMu serialises prepare/finish so concurrent joins cannot pick the same name or
// register one ticket twice.
var joinMu sync.Mutex

// NodeJoinService lets a fresh server register itself as a node with one command: the master
// mints a ticket, the server's script reserves a name ("HK A") and DNS record, installs the
// panel, then hands back its API token so the master can add and verify it.
type NodeJoinService struct {
	dns   NodeDnsService
	nodes NodeService
	// probe and fingerprint are swappable in tests.
	probe       func(ctx context.Context, n *model.Node) error
	fingerprint func(ctx context.Context, n *model.Node) (string, error)
}

func (s *NodeJoinService) certFingerprint(ctx context.Context, n *model.Node) (string, error) {
	if s.fingerprint != nil {
		return s.fingerprint(ctx, n)
	}
	return s.nodes.FetchCertFingerprint(ctx, n)
}

func (s *NodeJoinService) probeNode(ctx context.Context, n *model.Node) error {
	if s.probe != nil {
		return s.probe(ctx, n)
	}
	_, err := s.nodes.Probe(ctx, n)
	return err
}

func (s *NodeJoinService) MintTicket() (string, int64, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", 0, err
	}
	token := hex.EncodeToString(b)
	exp := time.Now().Add(nodeJoinTTL).Unix()
	db := database.GetDB()
	db.Where("expires_at < ?", time.Now().Unix()).Delete(&model.NodeJoinTicket{})
	if err := db.Create(&model.NodeJoinTicket{Token: token, ExpiresAt: exp}).Error; err != nil {
		return "", 0, err
	}
	return token, exp, nil
}

func (s *NodeJoinService) ticket(token string) (*model.NodeJoinTicket, error) {
	var t model.NodeJoinTicket
	err := database.GetDB().Where("token = ?", token).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && t.ExpiresAt < time.Now().Unix()) {
		return nil, common.NewError("join link is invalid or expired; create a new one in the master panel")
	}
	return &t, err
}

func letterName(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return fmt.Sprintf("%c%d", 'A'+i%26, i/26+1)
}

// nextNodeName follows the "HK A, HK B" convention: region code plus the first free letter.
func nextNodeName(region string, taken map[string]bool) string {
	for i := 0; ; i++ {
		name := region + " " + letterName(i)
		if !taken[strings.ToUpper(name)] {
			return name
		}
	}
}

type NodeJoinPrepared struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

// Prepare reserves the node's name and, when Cloudflare is configured, its DNS record, so the
// installer can request a certificate for that domain. Re-running returns the same reservation.
func (s *NodeJoinService) Prepare(ctx context.Context, token, ip, country, remoteIP string) (*NodeJoinPrepared, error) {
	joinMu.Lock()
	defer joinMu.Unlock()
	t, err := s.ticket(token)
	if err != nil {
		return nil, err
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() == nil || netsafe.IsBlockedIP(parsed) {
		return nil, common.NewError("a public IPv4 address is required, got:", ip)
	}
	ip = parsed.String()
	// The record must point at the machine running the script, not an address of the caller's choosing.
	if remote := net.ParseIP(remoteIP); remote != nil && remote.To4() != nil && remote.String() != ip {
		return nil, common.NewError("the reported IP", ip, "does not match the address the request came from:", remote.String())
	}
	if t.Name != "" {
		return &NodeJoinPrepared{Name: t.Name, Domain: t.Domain}, nil
	}
	region := "NODE"
	if countryRe.MatchString(country) {
		region = strings.ToUpper(country)
	}
	taken := map[string]bool{}
	var names []string
	db := database.GetDB()
	db.Model(&model.Node{}).Pluck("name", &names)
	var reserved []string
	db.Model(&model.NodeJoinTicket{}).Where("name <> '' AND token <> ?", token).Pluck("name", &reserved)
	for _, n := range append(names, reserved...) {
		taken[strings.ToUpper(strings.TrimSpace(n))] = true
	}
	var linked []string
	db.Model(&model.NodeDns{}).Pluck("name", &linked)
	linkedSet := make(map[string]bool, len(linked))
	for _, n := range linked {
		linkedSet[n] = true
	}
	useDNS := s.dns.Configured()
	for {
		name := nextNodeName(region, taken)
		if !useDNS {
			return s.reserve(token, name, "", ip)
		}
		fqdn, err := s.dns.FQDN(strings.ToLower(strings.ReplaceAll(name, " ", "-")))
		if err != nil {
			return nil, err
		}
		// Never repoint a name another node or an unrelated record already uses.
		current, exists, err := s.dns.LookupA(ctx, fqdn)
		if err != nil {
			return nil, err
		}
		if linkedSet[fqdn] || (exists && current != ip) {
			taken[strings.ToUpper(name)] = true
			continue
		}
		if err := s.dns.UpsertA(ctx, fqdn, ip); err != nil {
			return nil, err
		}
		return s.reserve(token, name, fqdn, ip)
	}
}

func (s *NodeJoinService) reserve(token, name, domain, ip string) (*NodeJoinPrepared, error) {
	db := database.GetDB()
	if err := db.Model(&model.NodeJoinTicket{}).Where("token = ?", token).
		Updates(map[string]any{"name": name, "domain": domain, "ip": ip}).Error; err != nil {
		return nil, err
	}
	return &NodeJoinPrepared{Name: name, Domain: domain}, nil
}

type NodeJoinFinish struct {
	Scheme   string `json:"scheme"`
	Port     int    `json:"port"`
	BasePath string `json:"basePath"`
	ApiToken string `json:"apiToken"`
	// AllowHttp must be set explicitly: over http the node's admin token travels in clear text.
	AllowHttp bool `json:"allowHttp"`
}

type NodeJoinResult struct {
	Node      *model.Node
	ReportKey string
}

// Finish adds the node using the first address/TLS combination the master can actually reach,
// preferring the DNS name with full certificate checks, and burns the ticket on success.
func (s *NodeJoinService) Finish(ctx context.Context, token string, req NodeJoinFinish) (*NodeJoinResult, error) {
	joinMu.Lock()
	defer joinMu.Unlock()
	t, err := s.ticket(token)
	if err != nil {
		return nil, err
	}
	if t.Name == "" || t.Ip == "" {
		return nil, common.NewError("run the prepare step first")
	}
	if strings.TrimSpace(req.ApiToken) == "" || req.Port <= 0 || req.Port > 65535 {
		return nil, common.NewError("node port and API token are required")
	}
	scheme := "http"
	if req.Scheme == "https" {
		scheme = "https"
	}
	if scheme == "http" && !req.AllowHttp {
		return nil, common.NewError("the node panel has no certificate, so its admin token would travel in clear text;",
			"open port 80 and re-run so a certificate can be issued, or re-run with JOIN_ALLOW_HTTP=1")
	}
	type attempt struct{ address, verify string }
	var attempts []attempt
	if t.Domain != "" {
		attempts = append(attempts, attempt{t.Domain, "verify"})
	}
	attempts = append(attempts, attempt{t.Ip, "verify"})
	if scheme == "https" {
		// Trust on first use: pin the certificate the node presents now.
		attempts = append(attempts, attempt{t.Ip, "pin"})
	}
	var lastErr error
	for _, a := range attempts {
		n := &model.Node{
			Name: t.Name, Scheme: scheme, Address: a.address, Port: req.Port, BasePath: req.BasePath,
			ApiToken: strings.TrimSpace(req.ApiToken), Enable: true, TlsVerifyMode: a.verify, InboundSyncMode: "all",
			Remark: "joined " + time.Now().Format("2006-01-02"),
		}
		if a.verify == "pin" {
			fctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			pin, err := s.certFingerprint(fctx, n)
			cancel()
			if err != nil {
				lastErr = err
				continue
			}
			n.PinnedCertSha256 = pin
		}
		if err := s.nodes.normalize(n); err != nil {
			return nil, err
		}
		pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		lastErr = s.probeNode(pctx, n)
		cancel()
		if lastErr != nil {
			continue
		}
		if err := s.nodes.Create(n); err != nil {
			return nil, err
		}
		res := &NodeJoinResult{Node: n}
		if t.Domain != "" {
			row := &model.NodeDns{NodeId: n.Id, Name: t.Domain, LastIp: t.Ip, SyncedAt: time.Now().Unix(), ReportKey: newReportKey()}
			if err := database.GetDB().Save(row).Error; err != nil {
				logger.Warning("node join: save DNS link failed:", err)
			} else {
				res.ReportKey = row.ReportKey
			}
		}
		database.GetDB().Where("token = ?", token).Delete(&model.NodeJoinTicket{})
		if a.verify == "pin" {
			logger.Warningf("node join: %s pinned to its current certificate (none valid for %s)", n.Name, a.address)
		}
		return res, nil
	}
	return nil, common.NewError("the master cannot reach the new node's panel:", lastErr)
}
