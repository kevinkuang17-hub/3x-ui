package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
)

func TestNodeCostRemindersOncePerRenewal(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	node := &model.Node{Name: "HK A", Scheme: "https", Address: "198.51.100.1", Port: 2053, ApiToken: "t", Enable: true}
	db.Create(node)
	s := &NodeCostService{}
	now := time.Unix(1_800_000_000, 0)
	if err := s.Set(&model.NodeCost{NodeId: 999, Monthly: 5}); err == nil {
		t.Fatal("cost for an unknown node must be refused")
	}
	if err := s.Set(&model.NodeCost{NodeId: node.Id, Monthly: 4.5, Currency: "usd", RenewAt: now.Add(10 * 24 * time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if ev, _ := s.DueReminders(now); len(ev) != 0 {
		t.Fatalf("renewal in 10 days is outside the window: %v", ev)
	}
	later := now.Add(8 * 24 * time.Hour)
	ev, err := s.DueReminders(later)
	if err != nil || len(ev) != 1 || ev[0].Type != eventbus.EventVpsRenew || ev[0].Source != "HK A" {
		t.Fatalf("reminder = %+v err=%v", ev, err)
	}
	if d, ok := ev[0].Data.(*eventbus.VpsRenewData); !ok || d.Currency != "USD" || d.Monthly != 4.5 {
		t.Fatalf("data = %+v", ev[0].Data)
	}
	if again, _ := s.DueReminders(later); len(again) != 0 {
		t.Fatal("a renewal must be announced once")
	}
	// Renewed: a new date re-arms the reminder.
	if err := s.Set(&model.NodeCost{NodeId: node.Id, Monthly: 4.5, RenewAt: later.Add(30 * 24 * time.Hour).Unix()}); err != nil {
		t.Fatal(err)
	}
	if ev, _ := s.DueReminders(later.Add(28 * 24 * time.Hour)); len(ev) != 1 {
		t.Fatalf("next renewal must be announced, got %v", ev)
	}
	db.Delete(&model.Node{}, node.Id)
	if rows, _ := s.List(); len(rows) != 0 {
		t.Fatal("costs of deleted nodes must be dropped")
	}
}
