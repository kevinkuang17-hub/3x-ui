package service

import "testing"

func TestSetGroupClashRules_LabelOnlyGroupGetsRow(t *testing.T) {
	initTrafficTestDB(t)
	svc := &ClientService{}
	seedGroupedClient(t, "alice", "vip", 0, 0)

	if err := svc.SetGroupClashRules("vip", "  DOMAIN-SUFFIX,crm.test,DIRECT  "); err != nil {
		t.Fatalf("SetGroupClashRules: %v", err)
	}
	if got := groupByName(t, svc, "vip").ClashRules; got != "DOMAIN-SUFFIX,crm.test,DIRECT" {
		t.Fatalf("clashRules = %q", got)
	}
	sources, err := svc.GroupClashRoutingSources()
	if err != nil || len(sources) != 1 || sources[0] != "DOMAIN-SUFFIX,crm.test,DIRECT" {
		t.Fatalf("GroupClashRoutingSources = %v, %v", sources, err)
	}

	if err := svc.SetGroupClashRules("vip", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := groupByName(t, svc, "vip").ClashRules; got != "" {
		t.Fatalf("cleared clashRules = %q", got)
	}
	if sources, _ := svc.GroupClashRoutingSources(); len(sources) != 0 {
		t.Fatalf("cleared group still listed: %v", sources)
	}
}

func TestSetGroupClashRules_RejectsBadInput(t *testing.T) {
	initTrafficTestDB(t)
	svc := &ClientService{}
	if err := svc.SetGroupClashRules(" ", "x"); err == nil {
		t.Fatal("empty group name must be rejected")
	}
	if err := svc.SetGroupClashRules("vip", "https://user:pw@rules.example/r.yaml"); err == nil {
		t.Fatal("remote source with credentials must be rejected")
	}
}
