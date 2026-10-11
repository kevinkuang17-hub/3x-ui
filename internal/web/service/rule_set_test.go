package service

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestExtraRuleForms(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"maersk.com", "DOMAIN-SUFFIX,maersk.com,PROXY"},
		{"https://Www.MSC.com/", "DOMAIN-SUFFIX,www.msc.com,PROXY"},
		{"*.icbc.com.cn, direct", "DOMAIN-SUFFIX,icbc.com.cn,DIRECT"},
		{"10.0.0.0/8,DIRECT", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve"},
		{"203.0.113.7,reject", "IP-CIDR,203.0.113.7/32,REJECT,no-resolve"},
		{"domain-keyword,teams,🚀 节点选择", "DOMAIN-KEYWORD,teams,🚀 节点选择"},
		{"IP-CIDR,192.0.2.0/24,PROXY,no-resolve", "IP-CIDR,192.0.2.0/24,PROXY,no-resolve"},
	}
	for _, c := range cases {
		got, err := extraRule(c.in)
		if err != nil || got != c.want {
			t.Errorf("extraRule(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"MATCH,DIRECT", "not a domain", "DOMAIN-SUFFIX,x.com", "RULE-SET,ads,REJECT", "IP-CIDR,192.0.2.0/24,no-resolve"} {
		if _, err := extraRule(bad); err == nil {
			t.Errorf("extraRule(%q) must be rejected", bad)
		}
	}
}

func TestRuleSetLinesRejectPolicies(t *testing.T) {
	if _, err := validateRuleSetText("maersk.com\nDOMAIN-SUFFIX,msc.com,PROXY"); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("a library line carrying a policy must be rejected with its line number, got %v", err)
	}
	got, err := validateRuleSetText("# shipping\n maersk.com \n\nIP-CIDR,198.51.100.0/24,no-resolve\n")
	if err != nil || got != "maersk.com\nIP-CIDR,198.51.100.0/24,no-resolve" {
		t.Fatalf("validateRuleSetText = %q, %v", got, err)
	}
}

func TestBuildRulesOrderAndPolicies(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	svc := &RuleSetService{}
	a, err := svc.Save(model.RuleSet{Name: "A", Rules: "a.example\nshared.example", DefaultPolicy: "proxy"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Save(model.RuleSet{Name: "B", Rules: "shared.example\nIP-CIDR,192.0.2.0/24,no-resolve", DefaultPolicy: "DIRECT"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := svc.Save(model.RuleSet{Name: "gone", Rules: "gone.example", DefaultPolicy: "DIRECT"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(gone.Id); err != nil {
		t.Fatal(err)
	}
	rules, err := svc.BuildRules(RoutingChoice{
		RuleSets:   []RuleSetPick{{Id: b.Id, Policy: "DIRECT"}, {Id: gone.Id, Policy: "PROXY"}, {Id: a.Id, Policy: "REJECT"}},
		ExtraRules: "a.example,PROXY",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DOMAIN-SUFFIX,a.example,PROXY",
		"DOMAIN-SUFFIX,shared.example,DIRECT",
		"IP-CIDR,192.0.2.0/24,DIRECT,no-resolve",
		"DOMAIN-SUFFIX,a.example,REJECT",
	}
	if !slices.Equal(rules, want) {
		t.Fatalf("custom lines first, then sets in pick order, first set wins a shared domain:\n got %q\nwant %q", rules, want)
	}
}

func TestBuiltinRuleSetsUpgradeWithoutOverwritingEdits(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	saved := builtinRuleSets
	t.Cleanup(func() { builtinRuleSets = saved })
	builtinRuleSets = []builtinRuleSet{
		{Key: "kept", Name: "Kept", Policy: "PROXY", Version: 1, Rules: []string{"old.example"}},
		{Key: "edited", Name: "Edited", Policy: "PROXY", Version: 1, Rules: []string{"old.example"}},
	}
	svc := &RuleSetService{}
	rows, err := svc.List()
	if err != nil || len(rows) != 2 {
		t.Fatalf("List = %d rows, %v", len(rows), err)
	}
	edited := rows[1]
	edited.Rules = "mine.example"
	if _, err := svc.Save(edited); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(edited.Id); err == nil {
		t.Fatal("built-in sets must not be deletable, they would be re-seeded")
	}

	builtinRuleSets = []builtinRuleSet{
		{Key: "kept", Name: "Kept", Policy: "PROXY", Version: 2, Rules: []string{"new.example"}},
		{Key: "edited", Name: "Edited", Policy: "PROXY", Version: 2, Rules: []string{"new.example"}},
		{Key: "added", Name: "Added", Policy: "DIRECT", Version: 1, Rules: []string{"added.example"}},
	}
	rows, err = svc.List()
	if err != nil || len(rows) != 3 {
		t.Fatalf("after upgrade List = %d rows, %v", len(rows), err)
	}
	if rows[0].Rules != "new.example" {
		t.Errorf("an untouched built-in set must follow the new list, got %q", rows[0].Rules)
	}
	if rows[1].Rules != "mine.example" {
		t.Errorf("an edited built-in set must keep the admin's list, got %q", rows[1].Rules)
	}
	reset, err := svc.ResetBuiltin(rows[1].Id)
	if err != nil || reset.Rules != "new.example" || reset.Edited {
		t.Fatalf("ResetBuiltin = %+v, %v", reset, err)
	}
}

func TestGroupRoutingFollowsGlobalUntilSet(t *testing.T) {
	initTrafficTestDB(t)
	seedGroupedClient(t, "alice", "vip", 0, 0)
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", "alice").Update("sub_id", "sub-alice").Error; err != nil {
		t.Fatal(err)
	}
	svc := &ClientService{}
	if err := svc.SetGroupClashRules("vip", ""); err != nil {
		t.Fatal(err)
	}
	if g := GroupRoutingBySubID("sub-alice"); g.Override {
		t.Fatalf("a group with nothing set follows the global routing, got %+v", g)
	}
	choice := &RoutingChoice{RuleSets: []RuleSetPick{{Id: 7, Policy: "direct"}}, ExtraRules: "x.example"}
	if err := svc.SetGroupRouting("vip", "", choice); err != nil {
		t.Fatal(err)
	}
	g := GroupRoutingBySubID("sub-alice")
	if !g.Override || g.Template != "" || len(g.Choice.RuleSets) != 1 || g.Choice.RuleSets[0].Policy != "DIRECT" {
		t.Fatalf("group picks must override the global routing, got %+v", g)
	}
	if err := svc.SetGroupClashRules("vip", ""); err != nil {
		t.Fatal(err)
	}
	if g := GroupRoutingBySubID("sub-alice"); !g.Override || g.Choice.ExtraRules != "x.example" {
		t.Fatalf("saving only the template must keep the group's picks, got %+v", g)
	}
	if err := svc.SetGroupRouting("vip", "", &RoutingChoice{RuleSets: []RuleSetPick{{Id: 1, Policy: "PROXY-ALL"}}}); err == nil {
		t.Fatal("an unknown policy must be rejected")
	}
}
