package service

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

const (
	PlanModeRenew = "renew" // extend from the later of now and the current expiry
	PlanModeReset = "reset" // restart the duration from now
	dayMillis     = int64(24 * time.Hour / time.Millisecond)
	maxPlanDays   = 36500
)

type PlanService struct{}

func (s *PlanService) List() ([]model.Plan, error) {
	var plans []model.Plan
	err := database.GetDB().Order("sort_order asc, id asc").Find(&plans).Error
	return plans, err
}

func (s *PlanService) Get(id int, name string) (*model.Plan, error) {
	var p model.Plan
	q := database.GetDB()
	if id > 0 {
		q = q.Where("id = ?", id)
	} else {
		q = q.Where("name = ?", strings.TrimSpace(name))
	}
	if err := q.First(&p).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewError("plan not found")
		}
		return nil, err
	}
	return &p, nil
}

func validatePlan(p *model.Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Group = strings.TrimSpace(p.Group)
	if p.Name == "" {
		return common.NewError("plan name is required")
	}
	if p.TotalGB < 0 || p.Days < 0 || p.LimitIp < 0 {
		return common.NewError("traffic, days and device limit must not be negative")
	}
	if p.Days > maxPlanDays {
		return common.NewError("a plan can last at most", maxPlanDays, "days")
	}
	if p.DelayStart && p.Days == 0 {
		return common.NewError("start-on-first-use needs a duration in days")
	}
	return nil
}

func (s *PlanService) Add(p *model.Plan) error {
	if err := validatePlan(p); err != nil {
		return err
	}
	p.Id = 0
	return database.GetDB().Create(p).Error
}

func (s *PlanService) Update(id int, p *model.Plan) error {
	if err := validatePlan(p); err != nil {
		return err
	}
	if _, err := s.Get(id, ""); err != nil {
		return err
	}
	p.Id = id
	return database.GetDB().Model(&model.Plan{}).Where("id = ?", id).Select("*").Omit("id", "created_at").Updates(p).Error
}

func (s *PlanService) Delete(id int) error {
	return database.GetDB().Delete(&model.Plan{}, id).Error
}

// PlanExpiry computes the client's new expiryTime (ms) under a plan; negative values are the
// panel's "start counting on first use" encoding.
func PlanExpiry(p *model.Plan, currentExpiry int64, mode string, now time.Time) int64 {
	if p.Days == 0 {
		return 0
	}
	span := int64(p.Days) * dayMillis
	if p.DelayStart {
		if mode == PlanModeRenew && currentExpiry < 0 {
			return currentExpiry - span
		}
		if mode == PlanModeRenew && currentExpiry > now.UnixMilli() {
			return currentExpiry + span
		}
		return -span
	}
	base := now.UnixMilli()
	switch {
	case mode == PlanModeRenew && currentExpiry > base:
		base = currentExpiry
	case mode == PlanModeRenew && currentExpiry < 0:
		// Unstarted delay-start days are still owed: they begin now, with the new days on top.
		base -= currentExpiry
	}
	return base + span
}

type PlanApplyResult struct {
	Applied int               `json:"applied"`
	Failed  map[string]string `json:"failed,omitempty"`
}

// Apply puts every listed client on the plan: limits and expiry first, then optional traffic
// reset and group. Each client is independent; failures are reported per email.
func (s *PlanService) Apply(clientSvc *ClientService, inboundSvc *InboundService, p *model.Plan, emails []string, mode string) (PlanApplyResult, bool, error) {
	if mode == "" {
		mode = PlanModeRenew
	}
	if mode != PlanModeRenew && mode != PlanModeReset {
		return PlanApplyResult{}, false, common.NewError("mode must be renew or reset")
	}
	res := PlanApplyResult{Failed: map[string]string{}}
	needRestart := false
	now := time.Now()
	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		rec, err := clientSvc.GetRecordByEmail(nil, email)
		if err != nil {
			res.Failed[email] = err.Error()
			continue
		}
		// Reset before re-enabling: otherwise the traffic job can disable the client again in between.
		if p.ResetTraffic {
			nr, err := clientSvc.ResetTrafficByEmail(inboundSvc, email)
			needRestart = needRestart || nr
			if err != nil {
				res.Failed[email] = err.Error()
				continue
			}
		}
		expiry := PlanExpiry(p, rec.ExpiryTime, mode, now)
		nr, err := clientSvc.applyClientFieldByEmail(inboundSvc, email, func(c map[string]any) {
			c["totalGB"] = p.TotalGB
			c["expiryTime"] = expiry
			c["limitIp"] = p.LimitIp
			c["enable"] = true
		})
		needRestart = needRestart || nr
		if err != nil {
			res.Failed[email] = err.Error()
			continue
		}
		if p.Group != "" && rec.Group != p.Group {
			if _, err := clientSvc.AddToGroup([]string{email}, p.Group); err != nil {
				res.Failed[email] = err.Error()
				continue
			}
		}
		res.Applied++
	}
	if len(res.Failed) == 0 {
		res.Failed = nil
	}
	return res, needRestart, nil
}
