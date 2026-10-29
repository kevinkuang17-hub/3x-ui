package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// CfOnlyStatus is which TCP ports of this server answer Cloudflare's edge only, and whether ufw
// is in a state where that holds (active, dropping what no rule allows).
type CfOnlyStatus struct {
	Ports       []int  `json:"ports" example:"[2053]"`
	Firewall    string `json:"firewall" example:"ufw"`
	Active      bool   `json:"active" example:"true"`
	DefaultDeny bool   `json:"defaultDeny" example:"true"`
	Ranges      int    `json:"ranges" example:"22"`
	UpdatedAt   int64  `json:"updatedAt" example:"1767225600000"`
}

type cfOnlyState struct {
	Ports     []int    `json:"ports"`
	Cidrs     []string `json:"cidrs"`
	UpdatedAt int64    `json:"updatedAt"`
}

var (
	errCfOnlyNoUfw            = errors.New("ufw is not active on this server; turn it on first (x-ui menu → Firewall Management, which keeps SSH open)")
	errCfOnlyDefaultAllow     = errors.New("ufw lets every incoming connection in by default, so limiting one port to Cloudflare would change nothing; set its incoming default to deny first")
	errCfOnlyNotViaCloudflare = errors.New("open the panel through its Cloudflare domain first: limited to Cloudflare, the address you use now would lock you out")
	errCfOnlyRanges           = errors.New("could not get Cloudflare's IP ranges")
)

const cloudflareIPsURL = "https://api.cloudflare.com/client/v4/ips"

// fetchCloudflareRanges downloads Cloudflare's published edge ranges; tests replace it.
var fetchCloudflareRanges = func(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloudflareIPsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&SettingService{}).NewProxiedHTTPClient(20 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", cloudflareIPsURL, resp.StatusCode)
	}
	var body struct {
		Success bool `json:"success"`
		Result  struct {
			IPv4 []string `json:"ipv4_cidrs"`
			IPv6 []string `json:"ipv6_cidrs"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if !body.Success {
		return nil, fmt.Errorf("%s: success=false", cloudflareIPsURL)
	}
	return append(body.Result.IPv4, body.Result.IPv6...), nil
}

// cloudflareRanges refuses anything but a plausible list of CIDRs: each one becomes a ufw
// argument, and a truncated list would cut off part of Cloudflare's edge.
func cloudflareRanges(ctx context.Context) ([]string, error) {
	list, err := fetchCloudflareRanges(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCfOnlyRanges, err)
	}
	out, v4 := []string{}, 0
	for _, raw := range list {
		_, ipNet, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not a CIDR", errCfOnlyRanges, raw)
		}
		cidr := ipNet.String()
		if slices.Contains(out, cidr) {
			continue
		}
		if ipNet.IP.To4() != nil {
			v4++
		}
		out = append(out, cidr)
	}
	if v4 < 5 {
		return nil, fmt.Errorf("%w: only %d IPv4 ranges", errCfOnlyRanges, v4)
	}
	return out, nil
}

// cfOnlyMu serialises changes: the daily refresh and a click in the panel both rewrite ufw.
var cfOnlyMu sync.Mutex

func loadCfOnlyState() cfOnlyState {
	var st cfOnlyState
	if raw, _ := (&SettingService{}).GetCfOnlyState(); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	return st
}

func saveCfOnlyState(st cfOnlyState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return (&SettingService{}).SetCfOnlyState(string(raw))
}

// cfOnlyPort reports whether TCP on port is limited to Cloudflare here.
func cfOnlyPort(port int) bool {
	return slices.Contains(loadCfOnlyState().Ports, port)
}

type ufwState struct {
	active, defaultAllow bool
	status               string
}

func readUfw(ctx context.Context) ufwState {
	out, err := runFirewall(ctx, "ufw", "status", "verbose")
	if err != nil {
		return ufwState{}
	}
	st := ufwState{status: out, active: strings.Contains(strings.ToLower(out), "status: active")}
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(l, "default:") {
			st.defaultAllow = ufwDefaultAllows(l)
		}
	}
	return st
}

// ufwOpenRule finds a rule letting anyone reach port over TCP: "2053/tcp", or "2053" for both protocols.
func ufwOpenRule(status string, port int) (tcpOnly, anyProto bool) {
	p := strconv.Itoa(port)
	for _, line := range strings.Split(status, "\n") {
		var f []string
		for _, w := range strings.Fields(line) {
			if w != "(v6)" && w != "IN" {
				f = append(f, w)
			}
		}
		if len(f) < 3 || f[1] != "ALLOW" || f[2] != "Anywhere" {
			continue
		}
		switch f[0] {
		case p + "/tcp":
			tcpOnly = true
		case p:
			anyProto = true
		}
	}
	return tcpOnly, anyProto
}

func ufw(ctx context.Context, args ...string) error {
	out, err := runFirewall(ctx, "ufw", args...)
	if err != nil {
		return common.NewErrorf("ufw %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return nil
}

func allowFromCloudflare(ctx context.Context, cidr string, port int) error {
	return skipWithoutIPv6(cidr, ufw(ctx, "allow", "proto", "tcp", "from", cidr, "to", "any", "port", strconv.Itoa(port)))
}

func deleteFromCloudflare(ctx context.Context, cidr string, port int) error {
	return skipWithoutIPv6(cidr, ufw(ctx, "delete", "allow", "proto", "tcp", "from", cidr, "to", "any", "port", strconv.Itoa(port)))
}

// skipWithoutIPv6 drops the error of an IPv6 rule on a ufw with IPV6=no, which filters no IPv6 at all.
func skipWithoutIPv6(cidr string, err error) error {
	if err != nil && strings.Contains(cidr, ":") && strings.Contains(err.Error(), "IPv6") {
		return nil
	}
	return err
}

func (s *ProtectService) CfOnly() CfOnlyStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st := loadCfOnlyState()
	fw := readUfw(ctx)
	out := CfOnlyStatus{Ports: st.Ports, Active: fw.active, DefaultDeny: fw.active && !fw.defaultAllow, Ranges: len(st.Cidrs), UpdatedAt: st.UpdatedAt}
	if out.Ports == nil {
		out.Ports = []int{}
	}
	if fw.status != "" {
		out.Firewall = "ufw"
	}
	return out
}

// EnableCfOnly limits TCP on port to Cloudflare's edge. Cloudflare is let in before the
// open-to-everyone rule goes, so the port never drops out in between.
func (s *ProtectService) EnableCfOnly(port int, viaCloudflare, panel bool) error {
	if port < 1 || port > 65535 {
		return common.NewErrorf("not a port: %d", port)
	}
	if panel && !viaCloudflare {
		return errCfOnlyNotViaCloudflare
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fw := readUfw(ctx)
	if !fw.active {
		return errCfOnlyNoUfw
	}
	if fw.defaultAllow {
		return errCfOnlyDefaultAllow
	}
	ranges, err := cloudflareRanges(ctx)
	if err != nil {
		return err
	}
	cfOnlyMu.Lock()
	defer cfOnlyMu.Unlock()
	st := loadCfOnlyState()
	if !slices.Contains(st.Ports, port) {
		st.Ports = append(st.Ports, port)
	}
	if err := applyCloudflareRanges(ctx, &st, ranges, port); err != nil {
		return err
	}
	tcpOnly, anyProto := ufwOpenRule(fw.status, port)
	if tcpOnly {
		if err := ufw(ctx, "delete", "allow", strconv.Itoa(port)+"/tcp"); err != nil {
			return err
		}
	}
	if anyProto {
		if err := ufw(ctx, "delete", "allow", strconv.Itoa(port)); err != nil {
			return err
		}
		if err := ufw(ctx, "allow", strconv.Itoa(port)+"/udp"); err != nil {
			return err
		}
	}
	return nil
}

// applyCloudflareRanges brings every protected port to ranges: newPort gets all of them, the
// others only the ones Cloudflare added; ranges Cloudflare retired are removed everywhere.
func applyCloudflareRanges(ctx context.Context, st *cfOnlyState, ranges []string, newPort int) error {
	old := st.Cidrs
	defer func() {
		st.UpdatedAt = time.Now().UnixMilli()
		_ = saveCfOnlyState(*st)
	}()
	for i, cidr := range ranges {
		for _, p := range st.Ports {
			if p != newPort && slices.Contains(old, cidr) {
				continue
			}
			if err := allowFromCloudflare(ctx, cidr, p); err != nil {
				st.Cidrs = mergeCidrs(old, ranges[:i+1])
				return err
			}
		}
	}
	st.Cidrs = slices.Clone(ranges)
	for _, cidr := range old {
		if slices.Contains(ranges, cidr) {
			continue
		}
		for _, p := range st.Ports {
			if p == newPort {
				continue
			}
			if err := deleteFromCloudflare(ctx, cidr, p); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeCidrs keeps every range that may still hold rules, so turning it off can find them.
func mergeCidrs(a, b []string) []string {
	out := slices.Clone(a)
	for _, c := range b {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// DisableCfOnly reopens port to everyone before removing the Cloudflare-only rules.
func (s *ProtectService) DisableCfOnly(port int) error {
	cfOnlyMu.Lock()
	defer cfOnlyMu.Unlock()
	st := loadCfOnlyState()
	if !slices.Contains(st.Ports, port) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if readUfw(ctx).active {
		if err := ufw(ctx, "allow", strconv.Itoa(port)+"/tcp"); err != nil {
			return err
		}
		for _, cidr := range st.Cidrs {
			if err := deleteFromCloudflare(ctx, cidr, port); err != nil {
				return err
			}
		}
	}
	st.Ports = slices.DeleteFunc(st.Ports, func(p int) bool { return p == port })
	if len(st.Ports) == 0 {
		st.Cidrs = nil
	}
	return saveCfOnlyState(st)
}

// RefreshCfOnly follows Cloudflare's range list on the protected ports; a failed download leaves
// the rules as they are rather than shutting Cloudflare out.
func (s *ProtectService) RefreshCfOnly() error {
	if len(loadCfOnlyState().Ports) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ranges, err := cloudflareRanges(ctx)
	if err != nil {
		return err
	}
	if !readUfw(ctx).active {
		return errCfOnlyNoUfw
	}
	cfOnlyMu.Lock()
	defer cfOnlyMu.Unlock()
	st := loadCfOnlyState()
	return applyCloudflareRanges(ctx, &st, ranges, 0)
}
