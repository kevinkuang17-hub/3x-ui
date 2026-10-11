package service

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// RuleSetService manages the routing rule library and turns routing choices into Clash rules.
type RuleSetService struct {
	settingService SettingService
}

// RuleSetPick is one rule set chosen by a routing profile, with the policy its matches go to.
type RuleSetPick struct {
	Id     int    `json:"id"`
	Policy string `json:"policy"`
}

// RoutingChoice is what a profile (global or one client group) adds in front of its template rules.
type RoutingChoice struct {
	RuleSets   []RuleSetPick `json:"ruleSets"`
	ExtraRules string        `json:"extraRules"`
}

func (c RoutingChoice) Empty() bool {
	return len(c.RuleSets) == 0 && strings.TrimSpace(c.ExtraRules) == ""
}

const (
	maxRuleSetLines  = 5000
	maxExtraRuleLine = 2000
)

// Policies every generated Clash profile defines (PROXY is the base select group).
var routingPolicies = map[string]bool{"DIRECT": true, "PROXY": true, "REJECT": true}

// Match types a rule-set line may use; each becomes TYPE,value,POLICY in the profile.
var ruleSetMatchTypes = map[string]bool{
	"DOMAIN": true, "DOMAIN-SUFFIX": true, "DOMAIN-KEYWORD": true, "DOMAIN-REGEX": true,
	"IP-CIDR": true, "IP-CIDR6": true, "GEOSITE": true, "GEOIP": true, "PROCESS-NAME": true,
	"DST-PORT": true, "SRC-IP-CIDR": true, "IP-ASN": true,
}

var domainPattern = regexp.MustCompile(`^(\*\.|\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z0-9-]{2,}$`)

var ruleSetSeedMu sync.Mutex

func NormalizePolicy(p string) (string, bool) {
	p = strings.ToUpper(strings.TrimSpace(p))
	return p, routingPolicies[p]
}

// ruleSetMatch turns one library line into "TYPE,value" (plus a trailing no-resolve when given).
func ruleSetMatch(line string) (string, error) {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 1 {
		return bareMatch(parts[0])
	}
	kind := strings.ToUpper(parts[0])
	if !ruleSetMatchTypes[kind] {
		return "", fmt.Errorf("unknown match type %q", parts[0])
	}
	if parts[1] == "" {
		return "", fmt.Errorf("%s needs a value", kind)
	}
	switch {
	case len(parts) == 2:
		return kind + "," + parts[1], nil
	case len(parts) == 3 && strings.EqualFold(parts[2], "no-resolve"):
		return kind + "," + parts[1] + ",no-resolve", nil
	}
	return "", fmt.Errorf("leave the policy out of library lines: %q", line)
}

// bareMatch reads a lone domain or IP range the way people paste them.
func bareMatch(v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimPrefix(strings.TrimPrefix(v, "https://"), "http://")
	v = strings.TrimSuffix(v, "/")
	if _, ipnet, err := net.ParseCIDR(v); err == nil {
		if ipnet.IP.To4() != nil {
			return "IP-CIDR," + ipnet.String() + ",no-resolve", nil
		}
		return "IP-CIDR6," + ipnet.String() + ",no-resolve", nil
	}
	if ip := net.ParseIP(v); ip != nil {
		if ip.To4() != nil {
			return "IP-CIDR," + ip.String() + "/32,no-resolve", nil
		}
		return "IP-CIDR6," + ip.String() + "/128,no-resolve", nil
	}
	if !domainPattern.MatchString(v) {
		return "", fmt.Errorf("not a domain or IP: %q", v)
	}
	return "DOMAIN-SUFFIX," + strings.TrimPrefix(strings.TrimPrefix(v, "*."), "."), nil
}

// withPolicy places the policy before an optional no-resolve flag, as Clash expects.
func withPolicy(match, policy string) string {
	if base, ok := strings.CutSuffix(match, ",no-resolve"); ok {
		return base + "," + policy + ",no-resolve"
	}
	return match + "," + policy
}

func ruleLines(raw string) []string {
	var out []string
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// extraRule reads one hand-written line: "domain", "domain,POLICY" or a full Clash rule.
func extraRule(line string) (string, error) {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 1 {
		m, err := bareMatch(parts[0])
		if err != nil {
			return "", err
		}
		return withPolicy(m, "PROXY"), nil
	}
	if len(parts) == 2 {
		if policy, ok := NormalizePolicy(parts[1]); ok {
			m, err := bareMatch(parts[0])
			if err != nil {
				return "", err
			}
			return withPolicy(m, policy), nil
		}
	}
	kind := strings.ToUpper(parts[0])
	if kind == "MATCH" {
		return "", fmt.Errorf("MATCH would hide every rule after it; set the default in the template instead")
	}
	if !ruleSetMatchTypes[kind] || len(parts) < 3 || parts[1] == "" || parts[2] == "" {
		return "", fmt.Errorf("write a domain, \"domain,DIRECT|PROXY|REJECT\" or TYPE,value,POLICY: %q", line)
	}
	parts[0] = kind
	return strings.Join(parts, ","), nil
}

func validateRuleSetText(raw string) (string, error) {
	lines := ruleLines(raw)
	if len(lines) == 0 {
		return "", common.NewError("the rule set has no rules")
	}
	if len(lines) > maxRuleSetLines {
		return "", common.NewErrorf("a rule set holds at most %d lines", maxRuleSetLines)
	}
	for i, line := range lines {
		if _, err := ruleSetMatch(line); err != nil {
			return "", common.NewErrorf("line %d: %v", i+1, err)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func validateExtraRules(raw string) (string, error) {
	lines := ruleLines(raw)
	if len(lines) > maxExtraRuleLine {
		return "", common.NewErrorf("at most %d custom rules", maxExtraRuleLine)
	}
	for i, line := range lines {
		if _, err := extraRule(line); err != nil {
			return "", common.NewErrorf("custom rule line %d: %v", i+1, err)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// EnsureBuiltins adds missing built-in sets and refreshes the ones the admin left untouched.
func (s *RuleSetService) EnsureBuiltins() error {
	ruleSetSeedMu.Lock()
	defer ruleSetSeedMu.Unlock()
	db := database.GetDB()
	for i, b := range builtinRuleSets {
		var row model.RuleSet
		err := db.Where("builtin_key = ?", b.Key).First(&row).Error
		if database.IsNotFound(err) {
			row = model.RuleSet{
				Name: b.Name, Category: b.Category, Rules: strings.Join(b.Rules, "\n"), DefaultPolicy: b.Policy,
				BuiltinKey: b.Key, BuiltinVersion: b.Version, SortOrder: i + 1,
			}
			if err := db.Create(&row).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !row.Edited && row.BuiltinVersion < b.Version {
			if err := db.Model(&row).Updates(map[string]any{
				"name": b.Name, "category": b.Category, "rules": strings.Join(b.Rules, "\n"),
				"default_policy": b.Policy, "builtin_version": b.Version,
			}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *RuleSetService) List() ([]model.RuleSet, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return nil, err
	}
	var rows []model.RuleSet
	err := database.GetDB().Order("sort_order asc, id asc").Find(&rows).Error
	return rows, err
}

// Save creates a set (Id 0) or updates one; editing a built-in set keeps it from later refreshes.
func (s *RuleSetService) Save(in model.RuleSet) (*model.RuleSet, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, common.NewError("rule set name is required")
	}
	rules, err := validateRuleSetText(in.Rules)
	if err != nil {
		return nil, err
	}
	policy, ok := NormalizePolicy(in.DefaultPolicy)
	if !ok {
		return nil, common.NewError("policy must be DIRECT, PROXY or REJECT")
	}
	db := database.GetDB()
	if in.Id == 0 {
		var maxSort int
		db.Model(&model.RuleSet{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSort)
		row := &model.RuleSet{
			Name: name, Category: strings.TrimSpace(in.Category), Rules: rules, DefaultPolicy: policy,
			SortOrder: maxSort + 1,
		}
		return row, db.Create(row).Error
	}
	var row model.RuleSet
	if err := db.First(&row, in.Id).Error; err != nil {
		return nil, common.NewError("rule set not found")
	}
	row.Name, row.Category, row.Rules, row.DefaultPolicy = name, strings.TrimSpace(in.Category), rules, policy
	if row.BuiltinKey != "" {
		row.Edited = true
	}
	return &row, db.Save(&row).Error
}

// Delete removes an admin-made set; built-in sets can only be reset, or they would come back.
func (s *RuleSetService) Delete(id int) error {
	var row model.RuleSet
	db := database.GetDB()
	if err := db.First(&row, id).Error; err != nil {
		return common.NewError("rule set not found")
	}
	if row.BuiltinKey != "" {
		return common.NewError("built-in rule sets cannot be deleted; reset it or leave it unpicked")
	}
	return db.Delete(&row).Error
}

// ResetBuiltin restores a built-in set to the shipped list and lets upgrades refresh it again.
func (s *RuleSetService) ResetBuiltin(id int) (*model.RuleSet, error) {
	db := database.GetDB()
	var row model.RuleSet
	if err := db.First(&row, id).Error; err != nil {
		return nil, common.NewError("rule set not found")
	}
	for _, b := range builtinRuleSets {
		if b.Key != row.BuiltinKey {
			continue
		}
		row.Name, row.Category, row.Rules, row.DefaultPolicy = b.Name, b.Category, strings.Join(b.Rules, "\n"), b.Policy
		row.BuiltinVersion, row.Edited = b.Version, false
		return &row, db.Save(&row).Error
	}
	return nil, common.NewError("only built-in rule sets can be reset")
}

// ParseRuleSetPicks reads a stored pick list; a damaged value counts as no picks.
func ParseRuleSetPicks(raw string) []RuleSetPick {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var picks []RuleSetPick
	if json.Unmarshal([]byte(raw), &picks) != nil {
		return nil
	}
	return picks
}

// NormalizeChoice validates a routing choice and returns its storable form.
func NormalizeChoice(ch RoutingChoice) (picksJSON, extra string, err error) {
	seen := map[int]bool{}
	picks := make([]RuleSetPick, 0, len(ch.RuleSets))
	for _, p := range ch.RuleSets {
		policy, ok := NormalizePolicy(p.Policy)
		if !ok {
			return "", "", common.NewError("policy must be DIRECT, PROXY or REJECT")
		}
		if p.Id <= 0 || seen[p.Id] {
			continue
		}
		seen[p.Id] = true
		picks = append(picks, RuleSetPick{Id: p.Id, Policy: policy})
	}
	if extra, err = validateExtraRules(ch.ExtraRules); err != nil {
		return "", "", err
	}
	if len(picks) > 0 {
		b, _ := json.Marshal(picks)
		picksJSON = string(b)
	}
	return picksJSON, extra, nil
}

func (s *RuleSetService) GlobalRouting() (RoutingChoice, error) {
	picks, err := s.settingService.getString("subClashRuleSets")
	if err != nil {
		return RoutingChoice{}, err
	}
	extra, err := s.settingService.getString("subClashExtraRules")
	if err != nil {
		return RoutingChoice{}, err
	}
	return RoutingChoice{RuleSets: ParseRuleSetPicks(picks), ExtraRules: extra}, nil
}

func (s *RuleSetService) SaveGlobalRouting(ch RoutingChoice) error {
	picks, extra, err := NormalizeChoice(ch)
	if err != nil {
		return err
	}
	if err := s.settingService.setString("subClashRuleSets", picks); err != nil {
		return err
	}
	return s.settingService.setString("subClashExtraRules", extra)
}

// BuildRules expands a choice into Clash rules: hand-written lines first, then each picked set in
// order. Sets deleted since they were picked are skipped.
func (s *RuleSetService) BuildRules(ch RoutingChoice) ([]string, error) {
	var out []string
	for _, line := range ruleLines(ch.ExtraRules) {
		r, err := extraRule(line)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if len(ch.RuleSets) == 0 {
		return out, nil
	}
	ids := make([]int, 0, len(ch.RuleSets))
	for _, p := range ch.RuleSets {
		ids = append(ids, p.Id)
	}
	var rows []model.RuleSet
	if err := database.GetDB().Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[int]model.RuleSet, len(rows))
	for _, r := range rows {
		byID[r.Id] = r
	}
	seen := map[string]bool{}
	for _, p := range ch.RuleSets {
		set, ok := byID[p.Id]
		policy, valid := NormalizePolicy(p.Policy)
		if !ok || !valid {
			continue
		}
		for _, line := range ruleLines(set.Rules) {
			m, err := ruleSetMatch(line)
			if err != nil || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, withPolicy(m, policy))
		}
	}
	return out, nil
}

// GroupRouting is a client group's own routing; Override is false when the group follows the global one.
type GroupRouting struct {
	Template string
	Choice   RoutingChoice
	Override bool
}

// GroupRoutingBySubID finds the routing of the subscriber's group, using the oldest client of the
// subId like the other per-group subscription lookups.
func GroupRoutingBySubID(subID string) GroupRouting {
	if subID == "" {
		return GroupRouting{}
	}
	db := database.GetDB()
	var rec model.ClientRecord
	if err := db.Where("sub_id = ?", subID).Order("id ASC").First(&rec).Error; err != nil || rec.Group == "" {
		return GroupRouting{}
	}
	var group model.ClientGroup
	if err := db.Where("name = ?", rec.Group).First(&group).Error; err != nil {
		return GroupRouting{}
	}
	g := GroupRouting{
		Template: strings.TrimSpace(group.ClashRules),
		Choice:   RoutingChoice{RuleSets: ParseRuleSetPicks(group.ClashRuleSets), ExtraRules: group.ClashExtraRules},
	}
	g.Override = g.Template != "" || !g.Choice.Empty()
	return g
}
