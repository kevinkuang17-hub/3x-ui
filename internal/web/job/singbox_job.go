package job

import (
	"context"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/singbox"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// SingboxJob keeps the sing-box sidecar serving exactly this server's enabled AnyTLS and
// NaiveProxy inbounds and their active clients; it also restarts a sidecar that died.
type SingboxJob struct {
	inboundService service.InboundService
	mu             sync.Mutex
	lastErr        string
}

func NewSingboxJob() *SingboxJob {
	return new(SingboxJob)
}

func (j *SingboxJob) Run() {
	j.mu.Lock()
	defer j.mu.Unlock()
	insts, err := j.inboundService.DesiredSingboxInstances()
	if err == nil {
		err = singbox.GetManager().Apply(context.Background(), insts)
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != "" && msg != j.lastErr {
		logger.Warning("sing-box job:", msg)
	}
	j.lastErr = msg
}
