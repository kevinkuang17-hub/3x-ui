package sub

import (
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
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
	clash := NewSubClashService(true, "DOMAIN-SUFFIX,global.test,DIRECT", NewSubService(""))

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
	out, _, err := NewSubClashService(false, "", NewSubService("")).GetClash("sub-vip", "sub.example.com")
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
	out, _, err := NewSubClashService(true, "DOMAIN-SUFFIX,global.test,DIRECT", NewSubService("")).GetClash("sub-g", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash: %v", err)
	}
	if !strings.Contains(out, "global.test") {
		t.Fatalf("group with empty rules must follow global, got:\n%s", out)
	}
}
