package job

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// VpsRenewJob announces servers that renew within service.RenewReminderWindow (vps.renew).
type VpsRenewJob struct {
	costs service.NodeCostService
}

func NewVpsRenewJob() *VpsRenewJob { return &VpsRenewJob{} }

func (j *VpsRenewJob) Run() {
	events, err := j.costs.DueReminders(time.Now())
	if err != nil {
		logger.Warning("vps renew reminders failed:", err)
		return
	}
	for _, e := range events {
		logger.Infof("server for node %s renews soon", e.Source)
		if EventBus != nil {
			EventBus.Publish(e)
		}
	}
}
