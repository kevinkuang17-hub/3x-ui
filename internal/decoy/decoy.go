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
	ModeLogistics   = "logistics"
	ModeTech        = "tech"
	ModeBlog        = "blog"
	ModePhoto       = "photo"
	ModeMaintenance = "maintenance"
	ModeCustom      = "custom"
)

//go:embed templates/*.html
var templatesFS embed.FS

var pages = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

// Site is what the admin fills in for a template; empty fields fall back to the template's defaults.
type Site struct {
	Title   string `json:"title"`
	Tagline string `json:"tagline"`
	Email   string `json:"email"`
}

var defaults = map[string]Site{
	ModeCompany:     {Title: "Horizon Supply Co.", Tagline: "Reliable sourcing and logistics, from factory floor to your warehouse."},
	ModeLogistics:   {Title: "Bluewater Freight", Tagline: "Sea and air freight, customs and delivery handled by one team."},
	ModeTech:        {Title: "Northlight Labs", Tagline: "We design and build dependable software for growing businesses."},
	ModeBlog:        {Title: "Field Notes", Tagline: "Notes on photography, slow travel and small software projects."},
	ModePhoto:       {Title: "Mira Chen Photography", Tagline: "Landscape, street and portrait photography."},
	ModeMaintenance: {Title: "Site under maintenance", Tagline: "We are performing scheduled maintenance. Please check back soon."},
}

// templated lists the modes that render a built-in template.
var templated = []string{ModeNginx, ModeCompany, ModeLogistics, ModeTech, ModeBlog, ModePhoto, ModeMaintenance}

type settings struct {
	mode string
	site Site
	dir  string
}

var current atomic.Pointer[settings]

// DefaultTitle is the site name a template shows when the admin left it empty.
func DefaultTitle(mode string) string {
	return defaults[mode].Title
}

// Modes lists the selectable modes, off first.
func Modes() []string {
	return append(append([]string{ModeOff}, templated...), ModeCustom)
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
func Configure(mode string, site Site, dir string) {
	if !ValidMode(mode) {
		mode = ModeOff
	}
	site.Title, site.Tagline, site.Email = strings.TrimSpace(site.Title), strings.TrimSpace(site.Tagline), strings.TrimSpace(site.Email)
	current.Store(&settings{mode: mode, site: site, dir: dir})
}

func Enabled() bool {
	s := current.Load()
	return s != nil && s.mode != ModeOff
}

// Page renders a built-in template, for both serving and the panel's preview.
func Page(mode string, site Site) ([]byte, error) {
	d := defaults[mode]
	if site.Title == "" {
		site.Title = d.Title
	}
	if site.Tagline == "" {
		site.Tagline = d.Tagline
	}
	contact := "#contact"
	if site.Email != "" {
		contact = "mailto:" + site.Email
	}
	var buf bytes.Buffer
	err := pages.ExecuteTemplate(&buf, mode+".html", map[string]any{
		"Title": site.Title, "Tagline": site.Tagline, "Email": site.Email, "ContactHref": contact,
		"Year": time.Now().Year(),
	})
	return buf.Bytes(), err
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
	page, err := Page(s.mode, s.site)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", page)
	c.Abort()
}

func notFound(c *gin.Context) {
	page, _ := Page("notfound", Site{})
	c.Data(http.StatusNotFound, "text/html; charset=utf-8", page)
	c.Abort()
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
