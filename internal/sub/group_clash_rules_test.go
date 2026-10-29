package sub

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const groupClashTCP = `{"network":"tcp","security":"none"}`

func TestGetClash_GroupRulesOverrideGlobal(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-vip", "vip-in", 20001, 0, groupClashTCP)
	seedSubInbound(t, "sub-plain", "plain-in", 20002, 0, groupClashTCP)
	db := database.GetDB()
	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", "sub-vip").Update("group_name", "vip").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroup{Name: "vip", ClashRules: "DOMAIN-SUFFIX,crm-group.test,DIRECT"}).Error; err != nil {
		t.Fatal(err)
	}
	clash := NewSubClashService(true, "DOMAIN-SUFFIX,global.test,DIRECT", false, NewSubService(""))

	vip, _, err := clash.GetClash("sub-vip", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash vip: %v", err)
	}
	if !strings.Contains(vip, "crm-group.test") || strings.Contains(vip, "global.test") {
		t.Fatalf("group member must get the group's rules only, got:\n%s", vip)
	}

	plain, _, err := clash.GetClash("sub-plain", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash plain: %v", err)
	}
	if !strings.Contains(plain, "global.test") || strings.Contains(plain, "crm-group.test") {
		t.Fatalf("ungrouped client must keep the global rules, got:\n%s", plain)
	}
}

func TestGetClash_GroupRulesApplyWhenGlobalRoutingDisabled(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-vip", "vip-in", 20003, 0, groupClashTCP)
	db := database.GetDB()
	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", "sub-vip").Update("group_name", "vip").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroup{Name: "vip", ClashRules: "DOMAIN-SUFFIX,crm-group.test,DIRECT"}).Error; err != nil {
		t.Fatal(err)
	}
	out, _, err := NewSubClashService(false, "", false, NewSubService("")).GetClash("sub-vip", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if !strings.Contains(out, "crm-group.test") {
		t.Fatalf("group rules must apply even with global routing off, got:\n%s", out)
	}
}

func TestGetClash_GroupWithoutRulesFollowsGlobal(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-g", "g-in", 20004, 0, groupClashTCP)
	db := database.GetDB()
	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", "sub-g").Update("group_name", "basic").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientGroup{Name: "basic"}).Error; err != nil {
		t.Fatal(err)
	}
	out, _, err := NewSubClashService(true, "DOMAIN-SUFFIX,global.test,DIRECT", false, NewSubService("")).GetClash("sub-g", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if !strings.Contains(out, "global.test") {
		t.Fatalf("group with empty rules must follow global, got:\n%s", out)
	}
}

func TestGetClash_RuleLibraryGoesAheadOfTemplate(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-lib", "lib-in", 20005, 0, groupClashTCP)
	seedSubInbound(t, "sub-own", "own-in", 20006, 0, groupClashTCP)
	db := database.GetDB()
	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", "sub-own").Update("group_name", "own").Error; err != nil {
		t.Fatal(err)
	}
	rs := &service.RuleSetService{}
	ships, err := rs.Save(model.RuleSet{Name: "ships", Rules: "maersk.com", DefaultPolicy: "PROXY"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.SaveGlobalRouting(service.RoutingChoice{
		RuleSets:   []service.RuleSetPick{{Id: ships.Id, Policy: "PROXY"}},
		ExtraRules: "bank.example,DIRECT\nDOMAIN-SUFFIX,lost.example,Missing Group",
	}); err != nil {
		t.Fatal(err)
	}
	if err := (&service.ClientService{}).SetGroupRouting("own", "", &service.RoutingChoice{ExtraRules: "own.example,REJECT"}); err != nil {
		t.Fatal(err)
	}
	clash := NewSubClashService(true, "DOMAIN-SUFFIX,template.test,DIRECT", false, NewSubService(""))

	out, _, err := clash.GetClash("sub-lib", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	bank := strings.Index(out, "DOMAIN-SUFFIX,bank.example,DIRECT")
	ship := strings.Index(out, "DOMAIN-SUFFIX,maersk.com,PROXY")
	tmpl := strings.Index(out, "DOMAIN-SUFFIX,template.test,DIRECT")
	if bank < 0 || ship < bank || tmpl < ship {
		t.Fatalf("want custom rules, then picked sets, then template rules:\n%s", out)
	}
	if !strings.Contains(out, "DOMAIN-SUFFIX,lost.example,PROXY") || strings.Contains(out, "Missing Group") {
		t.Fatalf("a rule naming an undefined policy group must fall back to the proxy group, not break the profile:\n%s", out)
	}

	own, _, err := clash.GetClash("sub-own", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash own: %v", err)
	}
	if !strings.Contains(own, "DOMAIN-SUFFIX,own.example,REJECT") || strings.Contains(own, "maersk.com") || strings.Contains(own, "template.test") {
		t.Fatalf("a group with its own routing gets only its own rules:\n%s", own)
	}
}

func TestGetClash_LibraryProxyFollowsTemplateMainGroup(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-tpl", "tpl-in", 20007, 0, groupClashTCP)
	template := "proxy-groups:\n" +
		"  - {name: \"🚀 手动选择\", type: select, proxies: [DIRECT]}\n" +
		"  - {name: \"🎯 全球直连\", type: select, proxies: [DIRECT]}\n" +
		"rules:\n  - MATCH,🚀 手动选择\n"
	if err := (&service.RuleSetService{}).SaveGlobalRouting(service.RoutingChoice{
		ExtraRules: "ships.example\nbank.example,🎯 全球直连\nlost.example,🇭🇰 香港节点",
	}); err != nil {
		t.Fatal(err)
	}
	out, _, err := NewSubClashService(true, template, false, NewSubService("")).GetClash("sub-tpl", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if !strings.Contains(out, "DOMAIN-SUFFIX,ships.example,🚀 手动选择") {
		t.Fatalf("\"proxy\" must mean the template's main group, or the rule bypasses the user's node choice:\n%s", out)
	}
	if !strings.Contains(out, "DOMAIN-SUFFIX,bank.example,🎯 全球直连") || !strings.Contains(out, "DOMAIN-SUFFIX,lost.example,🚀 手动选择") {
		t.Fatalf("template groups are valid destinations; groups it lacks fall back to its main group:\n%s", out)
	}
	groups, err := ClashTemplateGroups(template)
	if err != nil || strings.Join(groups, "|") != "🚀 手动选择|🎯 全球直连" {
		t.Fatalf("ClashTemplateGroups = %v, %v", groups, err)
	}
}

func TestGetClash_CompanyDNS(t *testing.T) {
	seedSubDB(t)
	seedSubInbound(t, "sub-dns", "dns-in", 20008, 0, groupClashTCP)
	seedSubInbound(t, "sub-corp", "corp-in", 20009, 0, groupClashTCP)
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("sub_id = ?", "sub-corp").Update("group_name", "corp").Error; err != nil {
		t.Fatal(err)
	}
	if err := (&service.RuleSetService{}).SaveGlobalDns(service.DnsConfig{Enable: true}); err != nil {
		t.Fatal(err)
	}
	if err := (&service.ClientService{}).SetGroupDns("corp", &service.DnsConfig{
		Enable: true, Company: "kstar.com = 192.168.8.10", CompanyDirect: true,
	}); err != nil {
		t.Fatal(err)
	}
	clash := NewSubClashService(false, "", false, NewSubService(""))
	plain, _, err := clash.GetClash("sub-dns", "sub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, "nameserver-policy") || strings.Contains(plain, "kstar.com") {
		t.Fatalf("the global DNS applies and carries no company rules:\n%s", plain)
	}
	corp, _, err := clash.GetClash("sub-corp", "sub.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corp, "192.168.8.10") || !strings.Contains(corp, "DOMAIN-SUFFIX,kstar.com,DIRECT") {
		t.Fatalf("the group's company DNS and its DIRECT rule must be in the profile:\n%s", corp)
	}
	if strings.Index(corp, "DOMAIN-SUFFIX,kstar.com,DIRECT") > strings.Index(corp, "MATCH,PROXY") {
		t.Fatalf("company domains must be matched before everything else:\n%s", corp)
	}
}
