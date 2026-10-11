package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
	"github.com/mhsanaei/3x-ui/v3/internal/panelaccess"
	"github.com/mhsanaei/3x-ui/v3/internal/web/global"
)

func protectRouter(t *testing.T, settings map[string]string) *gin.Engine {
	t.Helper()
	dbtest.InitDB(t, filepath.Join(t.TempDir(), "x-ui.db"))
	for k, v := range settings {
		if err := database.GetDB().Create(&model.Setting{Key: k, Value: v}).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		decoy.Configure(decoy.ModeOff, "", "")
		_ = panelaccess.Configure("")
	})
	previous := global.GetWebServer()
	s := NewServer()
	s.cron = cron.New(cron.WithLocation(time.Local), cron.WithSeconds())
	global.SetWebServer(s)
	t.Cleanup(func() {
		s.cancel()
		global.SetWebServer(previous)
	})
	engine, err := s.initRouter()
	if err != nil {
		t.Fatalf("init router: %v", err)
	}
	return engine
}

func get(engine *gin.Engine, path, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remote
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

func TestDecoyAnswersOutsideTheSecretPath(t *testing.T) {
	engine := protectRouter(t, map[string]string{"webBasePath": "/s3cret/", "decoyMode": "nginx"})

	root := get(engine, "/", "198.51.100.7:4000")
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), "Welcome to nginx!") {
		t.Fatalf("GET / must show the decoy page, got %d %q", root.Code, root.Body.String())
	}
	if probe := get(engine, "/panel/api/inbounds/list", "198.51.100.7:4000"); probe.Code != http.StatusNotFound ||
		!strings.Contains(probe.Body.String(), "nginx") {
		t.Fatalf("an unknown path must get the decoy's 404, got %d %q", probe.Code, probe.Body.String())
	}
	if login := get(engine, "/s3cret/", "198.51.100.7:4000"); strings.Contains(login.Body.String(), "Welcome to nginx!") {
		t.Fatal("the secret path must still reach the panel")
	}
}

func TestDecoyOffKeepsPlain404(t *testing.T) {
	engine := protectRouter(t, map[string]string{"webBasePath": "/s3cret/"})
	if w := get(engine, "/", "198.51.100.7:4000"); w.Code != http.StatusNotFound || w.Body.Len() != 0 {
		t.Fatalf("with the decoy off the panel must answer like before, got %d %q", w.Code, w.Body.String())
	}
}

func TestAllowlistHidesThePanelButKeepsPublicRoutes(t *testing.T) {
	engine := protectRouter(t, map[string]string{
		"webBasePath": "/s3cret/", "decoyMode": "maintenance", "panelAllowIps": "203.0.113.5",
	})
	blocked := get(engine, "/s3cret/", "198.51.100.7:4000")
	if !strings.Contains(blocked.Body.String(), decoy.DefaultTitle("maintenance")) {
		t.Fatalf("an address off the allowlist must see the decoy, got %d %q", blocked.Code, blocked.Body.String())
	}
	if allowed := get(engine, "/s3cret/", "203.0.113.5:4000"); strings.Contains(allowed.Body.String(), decoy.DefaultTitle("maintenance")) {
		t.Fatal("an allowed address must reach the panel")
	}
	if local := get(engine, "/s3cret/", "127.0.0.1:4000"); strings.Contains(local.Body.String(), decoy.DefaultTitle("maintenance")) {
		t.Fatal("loopback must always reach the panel")
	}
	join := get(engine, "/s3cret/probe/unknown-token", "198.51.100.7:4000")
	if strings.Contains(join.Body.String(), decoy.DefaultTitle("maintenance")) {
		t.Fatal("ISP probe routes must stay reachable for outside systems")
	}
}
