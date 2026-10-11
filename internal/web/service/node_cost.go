package service

import (
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// RenewReminderWindow is how far ahead a vps.renew event is raised.
const RenewReminderWindow = 3 * 24 * time.Hour

type NodeCostService struct{}

// List returns cost entries, dropping those whose node was deleted since.
func (s *NodeCostService) List() ([]model.NodeCost, error) {
	db := database.GetDB()
	db.Where("node_id NOT IN (?)", db.Model(&model.Node{}).Select("id")).Delete(&model.NodeCost{})
	var rows []model.NodeCost
	err := db.Order("node_id asc").Find(&rows).Error
	return rows, err
}

func (s *NodeCostService) Set(c *model.NodeCost) error {
	if c.NodeId <= 0 {
		return common.NewError("node id is required")
	}
	if c.Monthly < 0 || c.RenewAt < 0 {
		return common.NewError("cost and renewal date must not be negative")
	}
	if exists, err := (&NodeService{}).NodeExists(c.NodeId); err != nil || !exists {
		return common.NewError("node not found")
	}
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	if c.Currency == "" {
		c.Currency = "USD"
	}
	var prev model.NodeCost
	if database.GetDB().Where("node_id = ?", c.NodeId).First(&prev).Error == nil && prev.RenewAt == c.RenewAt {
		c.RemindedFor = prev.RemindedFor
	}
	return database.GetDB().Save(c).Error
}

func (s *NodeCostService) Delete(nodeID int) error {
	return database.GetDB().Where("node_id = ?", nodeID).Delete(&model.NodeCost{}).Error
}

// DueReminders returns one vps.renew event per server renewing within the window that has not
// been announced for this renewal date yet, and records it as announced.
func (s *NodeCostService) DueReminders(now time.Time) ([]eventbus.Event, error) {
	db := database.GetDB()
	var rows []model.NodeCost
	limit := now.Add(RenewReminderWindow).Unix()
	if err := db.Where("renew_at > 0 AND renew_at <= ? AND renew_at <> reminded_for", limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	names := map[int]string{}
	var nodes []model.Node
	db.Select("id, name").Find(&nodes)
	for _, n := range nodes {
		names[n.Id] = n.Name
	}
	events := make([]eventbus.Event, 0, len(rows))
	for _, r := range rows {
		name, ok := names[r.NodeId]
		if !ok {
			continue
		}
		db.Model(&model.NodeCost{}).Where("node_id = ?", r.NodeId).Update("reminded_for", r.RenewAt)
		events = append(events, eventbus.Event{
			Type: eventbus.EventVpsRenew, Source: name, Timestamp: now,
			Data: &eventbus.VpsRenewData{NodeId: r.NodeId, Provider: r.Provider, Monthly: r.Monthly, Currency: r.Currency, RenewAt: r.RenewAt},
		})
	}
	return events, nil
}
