package model

// Plan is a sellable package (traffic, duration, device limit, group) that can be applied to
// clients in one step, from the panel or a CRM.
type Plan struct {
	Id      int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name    string `json:"name" gorm:"uniqueIndex;not null"`
	TotalGB int64  `json:"totalGB" gorm:"column:total_gb;default:0"` // bytes, 0 = unlimited
	Days    int    `json:"days" gorm:"default:0"`                    // 0 = never expires
	LimitIp int    `json:"limitIp" gorm:"column:limit_ip;default:0"`
	Group   string `json:"group" gorm:"column:group_name;default:''"`
	// DelayStart makes the duration count from the client's first connection instead of now.
	DelayStart   bool   `json:"delayStart" gorm:"default:false"`
	ResetTraffic bool   `json:"resetTraffic"`
	Price        string `json:"price"`
	Remark       string `json:"remark"`
	SortOrder    int    `json:"sortOrder" gorm:"default:0"`
	CreatedAt    int64  `json:"createdAt" gorm:"autoCreateTime:milli"`
	UpdatedAt    int64  `json:"updatedAt" gorm:"autoUpdateTime:milli"`
}

func (Plan) TableName() string { return "plans" }
