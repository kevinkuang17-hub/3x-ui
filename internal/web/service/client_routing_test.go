package service

import (
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func seedRoutedClient(t *testing.T, email, group, subID string) {
	t.Helper()
	seedGroupedClient(t, email, group, 0, 0)
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("email = ?", email).Update("sub_id", subID).Error; err != nil {
		t.Fatal(err)
	}
}

// A client's own routing must win over its group's, and clearing it must hand the subscription
// back to the group, not to the global profile.
func TestClientRoutingOverridesGroupThenFallsBack(t *testing.T) {
	initTrafficTestDB(t)
	seedRoutedClient(t, "alice", "vip", "sub-alice")
	seedRoutedClient(t, "bob", "vip", "sub-bob")
	svc := &ClientService{}
	if err := svc.SetGroupRouting("vip", "", &RoutingChoice{ExtraRules: "group.example"}); err != nil {
		t.Fatal(err)
	}
	own := &RoutingChoice{RuleSets: []RuleSetPick{{Id: 3, Policy: "direct"}}, ExtraRules: "alice.example"}
	if err := svc.SetClientRouting("alice", "", own, nil); err != nil {
		t.Fatal(err)
	}

	g := RoutingBySubID("sub-alice")
	if g.Source != RoutingSourceClient || g.Choice.ExtraRules != "alice.example" || g.Choice.RuleSets[0].Policy != "DIRECT" {
		t.Fatalf("alice routing = %+v, want her own", g)
	}
	if g := RoutingBySubID("sub-bob"); g.Source != RoutingSourceGroup || g.Choice.ExtraRules != "group.example" {
		t.Fatalf("bob routing = %+v, want the group's", g)
	}
	view, err := svc.GetClientRouting("alice")
	if err != nil || !view.Own || view.Group != "vip" || view.Source != RoutingSourceClient || view.ExtraRules != "alice.example" {
		t.Fatalf("view = %+v, %v", view, err)
	}

	if err := svc.SetClientRouting("alice", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if g := RoutingBySubID("sub-alice"); g.Source != RoutingSourceGroup || g.Choice.ExtraRules != "group.example" {
		t.Fatalf("cleared alice routing = %+v, want the group's", g)
	}
	var rows int64
	database.GetDB().Model(&model.ClientRouting{}).Count(&rows)
	if rows != 0 {
		t.Fatalf("clearing both rules and DNS must remove the row, %d left", rows)
	}
}

// A company client often keeps the shared rules but needs its own internal DNS.
func TestClientDnsAloneKeepsGroupRules(t *testing.T) {
	initTrafficTestDB(t)
	seedRoutedClient(t, "carol", "vip", "sub-carol")
	svc := &ClientService{}
	if err := svc.SetGroupRouting("vip", "", &RoutingChoice{ExtraRules: "group.example"}); err != nil {
		t.Fatal(err)
	}
	dns := &DnsConfig{Enable: true, Mode: "fake-ip", Company: "corp.example = 10.0.0.53"}
	if err := svc.SetClientRouting("carol", "", nil, dns); err != nil {
		t.Fatal(err)
	}
	g := RoutingBySubID("sub-carol")
	if g.Source != RoutingSourceGroup || g.Choice.ExtraRules != "group.example" {
		t.Fatalf("rules = %+v, want the group's", g)
	}
	if g.Dns == nil || g.Dns.Company != "corp.example = 10.0.0.53" {
		t.Fatalf("dns = %+v, want carol's own", g.Dns)
	}
	if got := DnsForSub("sub-carol"); got.Company != "corp.example = 10.0.0.53" {
		t.Fatalf("DnsForSub = %+v", got)
	}
}

func TestClientRoutingRejectsEmptyOwnRules(t *testing.T) {
	initTrafficTestDB(t)
	seedRoutedClient(t, "dave", "", "sub-dave")
	if err := (&ClientService{}).SetClientRouting("dave", "", &RoutingChoice{}, nil); err == nil {
		t.Fatal("own routing with nothing in it must be refused, not saved as a silent no-op")
	}
	if g := RoutingBySubID("sub-dave"); g.Source != RoutingSourceGlobal {
		t.Fatalf("source = %q, want global", g.Source)
	}
}
