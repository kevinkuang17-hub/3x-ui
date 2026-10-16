package eventbus

import "time"

// EventType identifies the kind of event flowing through the bus.
type EventType string

const (
	// Outbound health (observatory-driven)
	EventOutboundDown EventType = "outbound.down"
	EventOutboundUp   EventType = "outbound.up"

	// Xray core (local)
	EventXrayCrash EventType = "xray.crash"
	// Conservative mode restored the last good template after a breaking edit
	EventXrayRollback EventType = "xray.rollback"

	// Node health (heartbeat-driven)
	EventNodeDown EventType = "node.down"
	EventNodeUp   EventType = "node.up"

	// Line health (node-hosted inbound reachability, drives subscription hiding)
	EventLineDown EventType = "line.down"
	EventLineUp   EventType = "line.up"

	// ISP probes inside China can no longer (or can again) reach a line
	EventChinaBlocked EventType = "china.blocked"
	EventChinaOk      EventType = "china.ok"

	// A node's server is due for renewal soon (VPS cost tracking)
	EventVpsRenew EventType = "vps.renew"

	// Client lifecycle (CRM webhook)
	EventClientTraffic80 EventType = "client.traffic80"
	EventClientDepleted  EventType = "client.depleted"
	EventClientExpiring  EventType = "client.expiring"
	EventClientExpired   EventType = "client.expired"

	// System health
	EventCPUHigh    EventType = "cpu.high"
	EventMemoryHigh EventType = "memory.high"

	// Security
	EventLoginAttempt EventType = "login.attempt"
)

// Event is the unit of information flowing through the bus.
type Event struct {
	Type      EventType
	Source    string    // outbound tag, node name, client email, IP, etc.
	Data      any       // event-specific payload, may be nil
	Timestamp time.Time // when the event was detected
}

// OutboundHealthData carries observatory details for outbound events.
type OutboundHealthData struct {
	Delay int64  // last measured delay in ms, 0 if unknown
	Error string // last error if probe failed, empty if up
}

// NodeHealthData carries heartbeat details for node events.
type NodeHealthData struct {
	NodeId    int
	LatencyMs int
	CpuPct    float64
	MemPct    float64
	XrayState string // "running", "stopped", etc.
	XrayError string
}

// LoginEventData carries login attempt details.
type LoginEventData struct {
	Username string
	IP       string
	Time     string
	Status   string // "success" or "fail"
	Reason   string
}

// SystemMetricData carries raw system metric values for threshold-based events.
type SystemMetricData struct {
	Percent   float64 // current usage percentage
	Threshold int     // configured threshold
}

// LineHealthData identifies the inbound behind a line.down / line.up event.
type LineHealthData struct {
	InboundId int    `json:"inboundId"`
	NodeId    int    `json:"nodeId"`
	Tag       string `json:"tag"`
	Port      int    `json:"port"`
}

// ClientLifecycleData carries the client snapshot behind a client.* event.
type ClientLifecycleData struct {
	Email      string `json:"email"`
	SubID      string `json:"subId"`
	Group      string `json:"group"`
	Up         int64  `json:"up"`
	Down       int64  `json:"down"`
	Total      int64  `json:"total"`
	ExpiryTime int64  `json:"expiryTime"`
}

// VpsRenewData describes the server behind a vps.renew event.
type VpsRenewData struct {
	NodeId   int     `json:"nodeId"`
	Provider string  `json:"provider"`
	Monthly  float64 `json:"monthly"`
	Currency string  `json:"currency"`
	RenewAt  int64   `json:"renewAt"`
}

// ChinaLineData describes a line as the probes inside China see it.
type ChinaLineData struct {
	InboundId int
	Node      string // empty for the panel's own server
	Host      string
	Port      int
	// State is the probes' verdict: ok, port (port blocked), ip (IP blocked) or blocked.
	State  string
	Probes int
}
