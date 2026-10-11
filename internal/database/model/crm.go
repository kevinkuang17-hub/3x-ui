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

// NodeDns links a node to a Cloudflare A record the panel keeps pointed at the node's IP.
type NodeDns struct {
	NodeId    int    `json:"nodeId" gorm:"primaryKey;autoIncrement:false"`
	Name      string `json:"name" gorm:"not null"`
	LastIp    string `json:"lastIp"`
	SyncedAt  int64  `json:"syncedAt"`
	LastError string `json:"lastError"`
	// ReportKey authorises the node's own cron job to report its public IP (POST /node-ip/:key).
	ReportKey string `json:"-" gorm:"index"`
}

func (NodeDns) TableName() string { return "node_dns" }

// NodeJoinTicket is a single-use invitation for a fresh server to register itself as a node.
type NodeJoinTicket struct {
	Token     string `gorm:"primaryKey"`
	ExpiresAt int64  `gorm:"index"`
	// Reserved by the first (prepare) call so a re-run of the script keeps the same name.
	Name   string
	Domain string
	Ip     string
}

func (NodeJoinTicket) TableName() string { return "node_join_tickets" }

// NodeCost tracks what a node's server costs and when it must be renewed.
type NodeCost struct {
	NodeId   int     `json:"nodeId" gorm:"primaryKey;autoIncrement:false"`
	Provider string  `json:"provider"`
	Monthly  float64 `json:"monthly"`
	Currency string  `json:"currency"`
	RenewAt  int64   `json:"renewAt"` // unix seconds, 0 = not tracked
	Note     string  `json:"note"`
	// RemindedFor is the RenewAt the last reminder was sent for, so each renewal is announced once.
	RemindedFor int64 `json:"-"`
}

func (NodeCost) TableName() string { return "node_costs" }
