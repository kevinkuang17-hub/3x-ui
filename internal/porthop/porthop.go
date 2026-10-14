// Package porthop makes the server answer Hysteria2 port hopping: clients send to any port of the
// advertised range, and a nat REDIRECT hands those packets to the one port Xray listens on.
package porthop

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Chain is the nat chain the panel owns; nothing else is touched apart from one jump to it.
const Chain = "XUI_PORTHOP"

// Redirect sends UDP Lo..Hi to To.
type Redirect struct {
	Lo, Hi, To int
}

func (r Redirect) args() []string {
	dport := strconv.Itoa(r.Lo)
	if r.Hi != r.Lo {
		dport += ":" + strconv.Itoa(r.Hi)
	}
	return []string{"-A", Chain, "-p", "udp", "-m", "udp", "--dport", dport, "-j", "REDIRECT", "--to-ports", strconv.Itoa(r.To)}
}

// ParsePorts reads a hop spec such as "20000-30000" or "443,20000-30000".
func ParsePorts(spec string) ([][2]int, error) {
	var out [][2]int
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		from, to, isRange := strings.Cut(part, "-")
		lo, err := strconv.Atoi(strings.TrimSpace(from))
		if err != nil {
			return nil, errors.New("bad hop port: " + part)
		}
		hi := lo
		if isRange {
			if hi, err = strconv.Atoi(strings.TrimSpace(to)); err != nil {
				return nil, errors.New("bad hop port: " + part)
			}
		}
		if lo < 1 || hi > 65535 || lo > hi {
			return nil, errors.New("bad hop port: " + part)
		}
		out = append(out, [2]int{lo, hi})
	}
	return out, nil
}

// HopSpec returns the hop range an inbound's stream settings advertise, or "".
func HopSpec(streamSettings string) string {
	var stream struct {
		Finalmask struct {
			QuicParams struct {
				UdpHop struct {
					Ports string `json:"ports"`
				} `json:"udpHop"`
			} `json:"quicParams"`
		} `json:"finalmask"`
	}
	if json.Unmarshal([]byte(streamSettings), &stream) != nil {
		return ""
	}
	return strings.TrimSpace(stream.Finalmask.QuicParams.UdpHop.Ports)
}

// Inbound is the part of an inbound that decides its redirects.
type Inbound struct {
	Port           int
	StreamSettings string
}

// Redirects lists what the given Hysteria inbounds need; the listening port itself is left out, and
// a range already claimed by an earlier inbound is not handed to a second one.
func Redirects(inbounds []Inbound) []Redirect {
	var out []Redirect
	for _, ib := range inbounds {
		ranges, err := ParsePorts(HopSpec(ib.StreamSettings))
		if err != nil || ib.Port <= 0 {
			continue
		}
		for _, rg := range ranges {
			for _, piece := range without(rg, ib.Port) {
				if !overlaps(out, piece) {
					out = append(out, Redirect{piece[0], piece[1], ib.Port})
				}
			}
		}
	}
	return out
}

func without(rg [2]int, port int) [][2]int {
	if port < rg[0] || port > rg[1] {
		return [][2]int{rg}
	}
	var out [][2]int
	if port > rg[0] {
		out = append(out, [2]int{rg[0], port - 1})
	}
	if port < rg[1] {
		out = append(out, [2]int{port + 1, rg[1]})
	}
	return out
}

func overlaps(have []Redirect, rg [2]int) bool {
	return slices.ContainsFunc(have, func(r Redirect) bool { return rg[0] <= r.Hi && r.Lo <= rg[1] })
}

// Run executes a firewall command; tests replace it.
var Run = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// LookPath finds a firewall tool; tests replace it.
var LookPath = exec.LookPath

// ErrNoIptables means the redirects could not be installed, so hopping clients reach nothing.
var ErrNoIptables = errors.New("iptables is not available, so Hysteria2 port hopping cannot be served")

// Apply makes the chain hold exactly the redirects, for IPv4 and (when present) IPv6. It leaves
// the rules alone when they already match, and does nothing at all on a server that never hopped.
func Apply(want []Redirect) error {
	if err := applyWith("iptables", want); err != nil {
		return err
	}
	_ = applyWith("ip6tables", want)
	return nil
}

func applyWith(tool string, want []Redirect) error {
	if _, err := LookPath(tool); err != nil {
		if tool == "iptables" && len(want) > 0 {
			return ErrNoIptables
		}
		return nil
	}
	current, err := Run(tool, "-t", "nat", "-S", Chain)
	exists := err == nil
	if err != nil {
		if len(want) == 0 {
			return nil
		}
		if out, err := Run(tool, "-t", "nat", "-N", Chain); err != nil {
			return errors.New(tool + " -N " + Chain + ": " + strings.TrimSpace(out))
		}
	}
	if !exists || !sameRules(current, want) {
		if exists {
			if out, err := Run(tool, "-t", "nat", "-F", Chain); err != nil {
				return errors.New(tool + " -F " + Chain + ": " + strings.TrimSpace(out))
			}
		}
		for _, r := range want {
			if out, err := Run(tool, append([]string{"-t", "nat"}, r.args()...)...); err != nil {
				return errors.New(tool + " " + strings.Join(r.args(), " ") + ": " + strings.TrimSpace(out))
			}
		}
	}
	if _, err := Run(tool, "-t", "nat", "-C", "PREROUTING", "-j", Chain); err != nil && len(want) > 0 {
		if out, err := Run(tool, "-t", "nat", "-I", "PREROUTING", "-j", Chain); err != nil {
			return errors.New(tool + " jump to " + Chain + ": " + strings.TrimSpace(out))
		}
	}
	return nil
}

// sameRules compares `iptables -S CHAIN` output with the wanted redirects, in order.
func sameRules(listing string, want []Redirect) bool {
	var have []string
	for _, line := range strings.Split(listing, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "-A "+Chain+" ") {
			have = append(have, line)
		}
	}
	if len(have) != len(want) {
		return false
	}
	for i, r := range want {
		if have[i] != strings.Join(r.args(), " ") {
			return false
		}
	}
	return true
}
