package model

// RuleSet is a named list of Clash/Mihomo match lines (banks, shipping lines, …) that routing
// profiles pick with a policy. Built-in sets are seeded by key and stay editable.
type RuleSet struct {
	Id       int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name     string `json:"name" gorm:"not null"`
	Category string `json:"category"`
	// Rules holds one match per line: a bare domain, or TYPE,value without the policy.
	Rules         string `json:"rules" gorm:"type:text"`
	DefaultPolicy string `json:"defaultPolicy" gorm:"column:default_policy;default:'PROXY'"`
	BuiltinKey    string `json:"builtinKey" gorm:"column:builtin_key;index;default:''"`
	// BuiltinVersion and Edited let an upgrade refresh a built-in list the admin never changed.
	BuiltinVersion int   `json:"-" gorm:"column:builtin_version;default:0"`
	Edited         bool  `json:"edited" gorm:"default:false"`
	SortOrder      int   `json:"sortOrder" gorm:"default:0"`
	CreatedAt      int64 `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt      int64 `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (RuleSet) TableName() string { return "rule_sets" }

// ClientRouting is one subscription's own routing, ahead of its group's and the global one. It is
// keyed by subId because that is what a subscription request carries.
type ClientRouting struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	SubId           string `json:"subId" gorm:"column:sub_id;uniqueIndex;not null"`
	ClashRules      string `json:"clashRules" gorm:"column:clash_rules;type:text;default:''"`
	ClashRuleSets   string `json:"clashRuleSets" gorm:"column:clash_rule_sets;type:text;default:''"`
	ClashExtraRules string `json:"clashExtraRules" gorm:"column:clash_extra_rules;type:text;default:''"`
	// ClashDns is the subscription's own DNS (JSON); empty follows the group's or the global DNS.
	ClashDns  string `json:"clashDns" gorm:"column:clash_dns;type:text;default:''"`
	UpdatedAt int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (ClientRouting) TableName() string { return "client_routings" }
