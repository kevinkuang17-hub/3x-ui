package job

import (
	"runtime"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/porthop"
)

// PortHopJob keeps the nat redirects for Hysteria2 port hopping in step with this server's own
// inbounds; a node panel runs the same job for the inbounds it hosts.
type PortHopJob struct {
	mu      sync.Mutex
	lastErr string
}

func NewPortHopJob() *PortHopJob {
	return new(PortHopJob)
}

func (j *PortHopJob) Run() {
	if runtime.GOOS != "linux" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var rows []model.Inbound
	err := database.GetDB().Select("port", "stream_settings").
		Where("enable = ? AND node_id IS NULL AND protocol = ?", true, model.Hysteria).
		Order("id").Find(&rows).Error
	if err != nil {
		logger.Warning("port hop job: load inbounds:", err)
		return
	}
	inbounds := make([]porthop.Inbound, 0, len(rows))
	for _, r := range rows {
		inbounds = append(inbounds, porthop.Inbound{Port: r.Port, StreamSettings: r.StreamSettings})
	}
	err = porthop.Apply(porthop.Redirects(inbounds))
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != "" && msg != j.lastErr {
		logger.Warning("port hop job:", msg)
	}
	j.lastErr = msg
}
