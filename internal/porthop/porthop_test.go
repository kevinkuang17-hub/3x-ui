package porthop

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func hop(ports string) string {
	return `{"network":"hysteria","finalmask":{"quicParams":{"udpHop":{"ports":"` + ports + `","interval":"5-10"}}}}`
}

func TestRedirects(t *testing.T) {
	cases := []struct {
		name string
		in   []Inbound
		want []Redirect
	}{
		{"range", []Inbound{{443, hop("20000-21000")}}, []Redirect{{20000, 21000, 443}}},
		{
			"list with the listening port left out",
			[]Inbound{{20500, hop("443,20000-21000")}},
			[]Redirect{{443, 443, 20500}, {20000, 20499, 20500}, {20501, 21000, 20500}},
		},
		{"no hop", []Inbound{{443, `{"network":"hysteria"}`}}, nil},
		{"bad spec ignored", []Inbound{{443, hop("30000-20000")}}, nil},
		{
			"second inbound cannot claim a taken range",
			[]Inbound{{443, hop("20000-21000")}, {8443, hop("20500-22000")}},
			[]Redirect{{20000, 21000, 443}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Redirects(c.in); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Redirects = %v, want %v", got, c.want)
			}
		})
	}
}

type fakeTables struct {
	chain  []string // nil: chain missing
	jumped bool
	calls  []string
}

func (f *fakeTables) install(t *testing.T, tools ...string) {
	t.Helper()
	oldRun, oldLook := Run, LookPath
	t.Cleanup(func() { Run, LookPath = oldRun, oldLook })
	LookPath = func(name string) (string, error) {
		for _, tool := range tools {
			if tool == name {
				return "/sbin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	Run = func(name string, args ...string) (string, error) {
		call := strings.Join(args, " ")
		if name == "iptables" && args[2] != "-S" {
			f.calls = append(f.calls, call)
		}
		if name != "iptables" {
			return "", nil
		}
		switch args[2] {
		case "-S":
			if f.chain == nil {
				return "iptables: No chain/target/match by that name.", errors.New("exit 1")
			}
			return strings.Join(append([]string{"-N " + Chain}, f.chain...), "\n"), nil
		case "-N":
			f.chain = []string{}
		case "-F":
			f.chain = []string{}
		case "-A":
			f.chain = append(f.chain, strings.Join(args[2:], " "))
		case "-C":
			if !f.jumped {
				return "", errors.New("exit 1")
			}
		case "-I":
			f.jumped = true
		}
		return "", nil
	}
}

func TestApply(t *testing.T) {
	want := []Redirect{{20000, 21000, 443}}
	rule := "-A XUI_PORTHOP -p udp -m udp --dport 20000:21000 -j REDIRECT --to-ports 443"

	t.Run("creates chain, rule and jump", func(t *testing.T) {
		f := &fakeTables{}
		f.install(t, "iptables")
		if err := Apply(want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.chain, []string{rule}) || !f.jumped {
			t.Fatalf("chain %v jumped %v", f.chain, f.jumped)
		}
	})
	t.Run("leaves matching rules alone", func(t *testing.T) {
		f := &fakeTables{chain: []string{rule}, jumped: true}
		f.install(t, "iptables")
		if err := Apply(want); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 1 || !strings.Contains(f.calls[0], "-C PREROUTING") {
			t.Fatalf("expected only the jump check, got %v", f.calls)
		}
	})
	t.Run("replaces stale rules", func(t *testing.T) {
		f := &fakeTables{chain: []string{"-A XUI_PORTHOP -p udp -m udp --dport 30000:31000 -j REDIRECT --to-ports 8443"}, jumped: true}
		f.install(t, "iptables")
		if err := Apply(want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(f.chain, []string{rule}) {
			t.Fatalf("chain %v", f.chain)
		}
	})
	t.Run("removing the last hop empties the chain", func(t *testing.T) {
		f := &fakeTables{chain: []string{rule}, jumped: true}
		f.install(t, "iptables")
		if err := Apply(nil); err != nil {
			t.Fatal(err)
		}
		if len(f.chain) != 0 {
			t.Fatalf("chain %v", f.chain)
		}
	})
	t.Run("never-hopped server is untouched", func(t *testing.T) {
		f := &fakeTables{}
		f.install(t, "iptables")
		if err := Apply(nil); err != nil {
			t.Fatal(err)
		}
		if f.chain != nil || len(f.calls) != 0 {
			t.Fatalf("touched: chain %v calls %v", f.chain, f.calls)
		}
	})
	t.Run("missing iptables is reported", func(t *testing.T) {
		f := &fakeTables{}
		f.install(t)
		if err := Apply(want); !errors.Is(err, ErrNoIptables) {
			t.Fatalf("err = %v", err)
		}
	})
}
