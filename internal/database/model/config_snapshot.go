package model

// ConfigSnapshot is one saved version of the Xray template config; Good marks a version Xray
// ran with, which conservative mode can roll back to when a later edit breaks the core.
type ConfigSnapshot struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Hash      string `json:"hash" gorm:"index"`
	Content   string `json:"-" gorm:"type:text"`
	Size      int    `json:"size"`
	Good      bool   `json:"good"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"createdAt"`
}

func (ConfigSnapshot) TableName() string { return "config_snapshots" }
