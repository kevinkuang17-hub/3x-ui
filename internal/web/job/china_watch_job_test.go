package job

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
)

// One alert when China loses a line and one when it comes back, however many rounds pass.
func TestChinaWatchAlertsOnTransitionsOnly(t *testing.T) {
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	db := database.GetDB()
	node := &model.Node{Name: "HK A", Scheme: "https", Address: "hk.example.com", Port: 2053, ApiToken: "t", Enable: true}
	db.Create(node)
	ib := &model.Inbound{
		UserId: 1, Enable: true, Port: 443, Protocol: model.VLESS, Tag: "hk", Remark: "HK A Reality",
		Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp"}`, NodeID: &node.Id,
	}
	db.Create(ib)
	report := func(ok bool, ping int) {
		now := time.Now().Unix()
		for probe := 1; probe <= 2; probe++ {
			db.Save(&model.IspProbeResult{ProbeId: probe, InboundId: ib.Id, Ok: ok, PingMs: ping, CheckedAt: now})
		}
	}
	events := collectNodeEvents(t)
	j := NewChinaWatchJob()

	report(false, 38)
	j.Run()
	j.Run()
	report(true, 38)
	j.Run()

	got := events()
	if len(got) != 2 || got[0].Type != eventbus.EventChinaBlocked || got[1].Type != eventbus.EventChinaOk {
		t.Fatalf("events = %+v, want blocked then ok", got)
	}
	data, _ := got[0].Data.(*eventbus.ChinaLineData)
	if data == nil || data.State != "port" || data.Node != "HK A" || data.Host != "hk.example.com" || data.Port != 443 || data.Probes != 2 {
		t.Fatalf("blocked event data = %+v", data)
	}
}
