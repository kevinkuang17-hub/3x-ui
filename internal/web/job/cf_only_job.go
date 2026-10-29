package job

import (
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// CfOnlyRefreshJob keeps Cloudflare-only ports in step with Cloudflare's published edge ranges.
type CfOnlyRefreshJob struct {
	protect service.ProtectService
}

func NewCfOnlyRefreshJob() *CfOnlyRefreshJob { return &CfOnlyRefreshJob{} }

func (j *CfOnlyRefreshJob) Run() {
	if err := j.protect.RefreshCfOnly(); err != nil {
		logger.Warning("cloudflare-only refresh:", err)
	}
}
