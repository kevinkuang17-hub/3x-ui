package model

// IspProbe is a small agent on a home router or VPS inside a given ISP that checks from there
// whether node lines are reachable (e.g. from China Telecom, Unicom and Mobile networks).
type IspProbe struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name      string `json:"name" gorm:"not null"`
	Isp       string `json:"isp"`
	TokenHash string `json:"-" gorm:"uniqueIndex"`
	LastSeen  int64  `json:"lastSeen"`
	LastIp    string `json:"lastIp"`
	CreatedAt int64  `json:"createdAt" gorm:"autoCreateTime"`
}

func (IspProbe) TableName() string { return "isp_probes" }

// IspProbeResult is the latest check of one line (inbound) from one probe.
type IspProbeResult struct {
	ProbeId   int   `json:"probeId" gorm:"primaryKey;autoIncrement:false"`
	InboundId int   `json:"inboundId" gorm:"primaryKey;autoIncrement:false"`
	Ok        bool  `json:"ok"`
	LatencyMs int   `json:"latencyMs"`
	CheckedAt int64 `json:"checkedAt"`
}

func (IspProbeResult) TableName() string { return "isp_probe_results" }
