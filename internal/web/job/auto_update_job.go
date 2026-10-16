package job

import (
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

// AutoUpdateJob runs hourly; the panel service decides whether this is the hour to update.
type AutoUpdateJob struct {
	panel panel.PanelService
}

func NewAutoUpdateJob() *AutoUpdateJob {
	return new(AutoUpdateJob)
}

func (j *AutoUpdateJob) Run() {
	if result := j.panel.RunAutoUpdate(time.Now()); result != "" {
		logger.Info("auto update:", result)
	}
}
