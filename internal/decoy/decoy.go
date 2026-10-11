// Package decoy serves an ordinary-looking website on every path that is not the panel's or the
// subscription server's secret path, so a scanner finds a plain site instead of a proxy panel.
package decoy

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	ModeOff         = "off"
	ModeNginx       = "nginx"
	ModeCompany     = "company"
	ModeBlog        = "blog"
	ModeMaintenance = "maintenance"
	ModeCustom      = "custom"
)

//go:embed templates/*.html
var templatesFS embed.FS

var pages = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

var defaultTitles = map[string]string{
	ModeCompany:     "Horizon Supply Co.",
	ModeBlog:        "Field Notes",
	ModeMaintenance: "Site under maintenance",
}

type settings struct {
	mode, title, dir string
}

var current atomic.Pointer[settings]

// DefaultTitle is the site name a template shows when the admin left it empty.
func DefaultTitle(mode string) string {
	return defaultTitles[mode]
}

// Modes lists the selectable modes, off first.
func Modes() []string {
	return []string{ModeOff, ModeNginx, ModeCompany, ModeBlog, ModeMaintenance, ModeCustom}
}

func ValidMode(mode string) bool {
	for _, m := range Modes() {
		if m == mode {
			return true
		}
	}
	return false
}

// Configure switches the decoy for both servers; dir is where an uploaded custom site lives.
func Configure(mode, title, dir string) {
	if !ValidMode(mode) {
		mode = ModeOff
	}
	current.Store(&settings{mode: mode, title: strings.TrimSpace(title), dir: dir})
}

func Enabled() bool {
	s := current.Load()
	return s != nil && s.mode != ModeOff
}

// Respond answers the request with the decoy site and stops the handler chain.
func Respond(c *gin.Context) {
	s := current.Load()
	if s == nil || s.mode == ModeOff {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Server", "nginx")
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		notFound(c)
		return
	}
	if s.mode == ModeCustom {
		serveCustom(c, s.dir)
		return
	}
	p := path.Clean("/" + c.Request.URL.Path)
	if p != "/" && p != "/index.html" {
		notFound(c)
		return
	}
	title := s.title
	if title == "" {
		title = DefaultTitle(s.mode)
	}
	render(c, http.StatusOK, s.mode+".html", title)
}

func render(c *gin.Context, status int, name, title string) {
	var buf bytes.Buffer
	if err := pages.ExecuteTemplate(&buf, name, map[string]any{"Title": title, "Year": time.Now().Year()}); err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(status, "text/html; charset=utf-8", buf.Bytes())
	c.Abort()
}

func notFound(c *gin.Context) {
	render(c, http.StatusNotFound, "notfound.html", "")
}

// serveCustom serves an uploaded site; folders without an index.html are not listed.
func serveCustom(c *gin.Context, dir string) {
	if dir == "" {
		notFound(c)
		return
	}
	p := path.Clean("/" + c.Request.URL.Path)
	full := filepath.Join(dir, filepath.FromSlash(p))
	info, err := os.Stat(full)
	if err == nil && info.IsDir() {
		full = filepath.Join(full, "index.html")
		info, err = os.Stat(full)
	}
	if err != nil || info.IsDir() || hiddenPath(p) {
		notFound(c)
		return
	}
	// The uploaded site shares the panel's origin; the sandbox keeps its scripts away from the admin session.
	c.Header("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-popups")
	c.File(full)
	c.Abort()
}

func hiddenPath(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}
