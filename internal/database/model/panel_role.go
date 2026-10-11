package model

// Panel roles. The first user (lowest id) is always an admin.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
)

// PanelRole gives a panel user a role; users without a row keep full (admin) access.
type PanelRole struct {
	UserId    int    `json:"userId" gorm:"primaryKey;autoIncrement:false"`
	Role      string `json:"role" gorm:"not null"`
	CreatedAt int64  `json:"createdAt" gorm:"autoCreateTime"`
}

func (PanelRole) TableName() string { return "panel_roles" }
