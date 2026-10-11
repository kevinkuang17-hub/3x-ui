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

// PasskeyCredential is a WebAuthn credential (passkey) a panel user registered for password-less login.
type PasskeyCredential struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"userId" gorm:"index;not null"`
	CredentialId string `json:"-" gorm:"uniqueIndex;not null"` // base64url
	Data         string `json:"-" gorm:"type:text;not null"`   // webauthn.Credential as JSON
	Name         string `json:"name"`
	CreatedAt    int64  `json:"createdAt" gorm:"autoCreateTime"`
	LastUsedAt   int64  `json:"lastUsedAt"`
}

func (PasskeyCredential) TableName() string { return "passkey_credentials" }
