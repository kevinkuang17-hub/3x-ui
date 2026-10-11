package job

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/op/go-logging"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/eventbus"
	"github.com/mhsanaei/3x-ui/v3/internal/linehealth"
	xuilogger "github.com/mhsanaei/3x-ui/v3/internal/logger"
)

type fakeDialer struct {
	mu   sync.Mutex
	down map[string]bool
}

func (f *fakeDialer) set(addr string, isDown bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down[addr] = isDown
}

func (f *fakeDialer) dial(_ context.Context, addr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down[addr] {
		return errors.New("refused")
	}
	return nil
}

func seedLineHealth(t *testing.T, ports ...int) (*LineHealthJob, *fakeDialer, map[int]int) {
	t.Helper()
	xuilogger.InitLogger(logging.ERROR)
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	linehealth.Reset()
	t.Cleanup(linehealth.Reset)
	if err := database.GetDB().Create(&model.Setting{Key: "subLineHealth", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}
	node := &model.Node{Name: "hk-a", Scheme: "https", Address: "hk.example.com", Port: 2053, ApiToken: "tok", Enable: true, Status: "online", XrayState: "running"}
	if err := database.GetDB().Create(node).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	ids := map[int]int{}
	for _, port := range ports {
		ib := &model.Inbound{
			UserId: 1, Enable: true, Port: port, Protocol: model.VLESS, Tag: fmt.Sprintf("in-%d", port),
			Remark: fmt.Sprintf("HK A %d", port), Settings: `{"clients":[]}`, StreamSettings: `{"network":"tcp"}`, NodeID: &node.Id,
		}
		if err := database.GetDB().Create(ib).Error; err != nil {
			t.Fatalf("create inbound: %v", err)
		}
		ids[port] = ib.Id
	}
	fd := &fakeDialer{down: map[string]bool{}}
	j := NewLineHealthJob()
	j.dial = fd.dial
	return j, fd, ids
}

func TestLineHealthHidesAfterThresholdAndRestores(t *testing.T) {
	j, fd, ids := seedLineHealth(t, 10001, 10002, 10003, 10004)
	events := collectNodeEvents(t)
	fd.set("hk.example.com:10002", true)

	for range lineHealthFailThreshold - 1 {
		j.Run()
	}
	if linehealth.IsDown(ids[10002]) {
		t.Fatal("line hidden before reaching the failure threshold")
	}
	j.Run()
	if !linehealth.IsDown(ids[10002]) || linehealth.IsDown(ids[10001]) {
		t.Fatalf("after threshold want only 10002 hidden, got %v", linehealth.Down())
	}

	fd.set("hk.example.com:10002", false)
	j.Run()
	if linehealth.IsDown(ids[10002]) {
		t.Fatal("recovered line must be restored on the first good probe")
	}
	got := events()
	if len(got) != 2 || got[0].Type != eventbus.EventLineDown || got[1].Type != eventbus.EventLineUp {
		t.Fatalf("want line.down then line.up, got %+v", got)
	}
	if data, ok := got[0].Data.(*eventbus.LineHealthData); !ok || data.InboundId != ids[10002] {
		t.Fatalf("line.down data = %+v", got[0].Data)
	}
}

func TestLineHealthMassFailureLeavesSubscriptionsAlone(t *testing.T) {
	j, fd, _ := seedLineHealth(t, 10011, 10012, 10013, 10014)
	for _, p := range []int{10011, 10012, 10013} {
		fd.set(fmt.Sprintf("hk.example.com:%d", p), true)
	}
	for range lineHealthFailThreshold + 2 {
		j.Run()
	}
	if down := linehealth.Down(); len(down) != 0 {
		t.Fatalf("mass failure must hide nothing, got %v", down)
	}
}

func TestLineHealthDisabledClearsMarks(t *testing.T) {
	j, fd, ids := seedLineHealth(t, 10021, 10022, 10023)
	fd.set("hk.example.com:10021", true)
	for range lineHealthFailThreshold {
		j.Run()
	}
	if !linehealth.IsDown(ids[10021]) {
		t.Fatal("precondition: line should be hidden")
	}
	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "subLineHealth").Update("value", "false").Error; err != nil {
		t.Fatal(err)
	}
	j.Run()
	if len(linehealth.Down()) != 0 {
		t.Fatal("turning the feature off must restore every line")
	}
}

func TestLineHealthUDPFollowsNodeXrayState(t *testing.T) {
	ib := &model.Inbound{Protocol: model.Hysteria, Port: 443}
	j := NewLineHealthJob()
	j.dial = func(context.Context, string) error { t.Fatal("UDP lines must not be TCP-dialed"); return nil }
	cases := []struct {
		status, xray string
		want         lineVerdict
	}{
		{"online", "running", lineOK},
		{"online", "stopped", lineFail},
		{"offline", "stopped", lineUnknown},
		{"online", "", lineUnknown},
	}
	for _, c := range cases {
		if got := j.probeOne(ib, &model.Node{Address: "x", Status: c.status, XrayState: c.xray}); got != c.want {
			t.Fatalf("%s/%s: got %v want %v", c.status, c.xray, got, c.want)
		}
	}
	if !linehealth.IsUDP(&model.Inbound{Protocol: model.VLESS, StreamSettings: `{"network":"kcp"}`}) {
		t.Fatal("kcp transport is UDP")
	}
}

func TestLineProbeHostFollowsAdvertisedAddress(t *testing.T) {
	node := &model.Node{Address: "hk.example.com"}
	cases := []struct {
		name string
		ib   model.Inbound
		node *model.Node
		want string
	}{
		{"node address by default", model.Inbound{Listen: "0.0.0.0"}, node, "hk.example.com"},
		{"custom share address", model.Inbound{ShareAddrStrategy: "custom", ShareAddr: "cdn.example.com"}, node, "cdn.example.com"},
		{"public listen strategy", model.Inbound{ShareAddrStrategy: "listen", Listen: "203.0.113.9"}, node, "203.0.113.9"},
		{"fallback child on loopback", model.Inbound{Listen: "127.0.0.1"}, node, ""},
		{"unix socket", model.Inbound{Listen: "/run/xray.sock"}, node, ""},
		{"external proxy", model.Inbound{StreamSettings: `{"externalProxy":[{"dest":"relay.example.com","port":443}]}`}, node, ""},
		{"tailscale node address", model.Inbound{}, &model.Node{Address: "100.101.102.103"}, ""},
		{"lan node address", model.Inbound{}, &model.Node{Address: "192.168.1.10"}, ""},
	}
	for _, c := range cases {
		if got := linehealth.ProbeHost(&c.ib, c.node); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestLineHealthSkipsInboundsBehindManagedHosts(t *testing.T) {
	j, fd, ids := seedLineHealth(t, 10031, 10032, 10033)
	if err := database.GetDB().Create(&model.Host{InboundId: ids[10031], Remark: "cdn"}).Error; err != nil {
		t.Fatal(err)
	}
	fd.set("hk.example.com:10031", true)
	for range lineHealthFailThreshold + 1 {
		j.Run()
	}
	if linehealth.IsDown(ids[10031]) {
		t.Fatal("an inbound reached through managed hosts must not be judged by the node port")
	}
}

func TestLineHealthHidesLinesProbesCannotReach(t *testing.T) {
	j, _, ids := seedLineHealth(t, 10041, 10042, 10043)
	now := time.Now().Unix()
	for probeID := 1; probeID <= 2; probeID++ {
		database.GetDB().Create(&model.IspProbeResult{ProbeId: probeID, InboundId: ids[10041], Ok: false, CheckedAt: now})
	}
	for range lineHealthFailThreshold {
		j.Run()
	}
	if !linehealth.IsDown(ids[10041]) || linehealth.IsDown(ids[10042]) {
		t.Fatalf("a line every ISP probe fails must be hidden even when the master reaches it: %v", linehealth.Down())
	}
}
