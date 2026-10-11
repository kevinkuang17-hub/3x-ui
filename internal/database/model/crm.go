package model

// CrmEventMark records that a client lifecycle event was already sent, so it fires once per
// crossing; the mark is dropped when the condition clears (renewal, traffic reset).
type CrmEventMark struct {
	Id        int    `gorm:"primaryKey;autoIncrement"`
	Email     string `gorm:"not null;uniqueIndex:idx_crm_event_mark,priority:1"`
	Event     string `gorm:"not null;uniqueIndex:idx_crm_event_mark,priority:2"`
	CreatedAt int64  `gorm:"autoCreateTime"`
}

func (CrmEventMark) TableName() string { return "crm_event_marks" }

// CrmWebhookOutbox holds webhook deliveries until the CRM acknowledges them with a 2xx.
type CrmWebhookOutbox struct {
	Id        int    `gorm:"primaryKey;autoIncrement"`
	Event     string `gorm:"not null"`
	Payload   string `gorm:"type:text;not null"`
	Attempts  int    `gorm:"default:0"`
	NextAt    int64  `gorm:"index;default:0"`
	LastError string `gorm:"type:text"`
	CreatedAt int64  `gorm:"autoCreateTime"`
}

func (CrmWebhookOutbox) TableName() string { return "crm_webhook_outbox" }

// SsoTicket is a single-use, short-lived login link minted for a CRM via the API.
type SsoTicket struct {
	Token      string `gorm:"primaryKey"`
	Username   string `gorm:"not null"`
	Redirect   string
	ExpiresAt  int64 `gorm:"index"`
	LoginEpoch int64
}

func (SsoTicket) TableName() string { return "sso_tickets" }
