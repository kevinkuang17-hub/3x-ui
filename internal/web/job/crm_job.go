package job

import (
	"context"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/crm"
)

// CrmJob publishes client lifecycle events (traffic 80%, depleted, expiring, expired) and
// drains the CRM webhook outbox; both are skipped while the CRM webhook is off.
type CrmJob struct {
	settingService service.SettingService
	crm            *crm.Service
	running        sync.Mutex
}

func NewCrmJob(c *crm.Service) *CrmJob {
	return &CrmJob{crm: c}
}

func (j *CrmJob) Run() {
	if !j.running.TryLock() {
		return
	}
	defer j.running.Unlock()
	if on, err := j.settingService.GetCrmWebhookEnable(); err != nil || !on {
		return
	}
	// Lifecycle events go straight to the outbox, not the shared bus: a first-run burst would
	// overflow subscriber queues and crowd out Telegram/Discord/email alerts.
	if _, err := j.crm.ScanLifecycle(); err != nil {
		logger.Warning("crm: lifecycle scan failed:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := j.crm.DeliverPending(ctx); err != nil {
		logger.Warning("crm: webhook delivery failed:", err)
	}
}
