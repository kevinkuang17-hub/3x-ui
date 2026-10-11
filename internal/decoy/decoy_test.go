package decoy

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func serve(t *testing.T, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	Respond(c)
	return w
}

func TestServeTemplates(t *testing.T) {
	t.Cleanup(func() { Configure(ModeOff, "", "") })
	Configure(ModeCompany, `<script>alert(1)</script> & Co`, "")
	w := serve(t, http.MethodGet, "/")
	body := w.Body.String()
	if w.Code != http.StatusOK || w.Header().Get("Server") != "nginx" {
		t.Fatalf("decoy root = %d, Server %q", w.Code, w.Header().Get("Server"))
	}
	if strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("the admin's site name must be HTML-escaped")
	}
	if w := serve(t, http.MethodGet, "/wp-login.php"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "404 Not Found") {
		t.Fatalf("unknown paths must get an nginx-style 404, got %d", w.Code)
	}
	if w := serve(t, http.MethodPost, "/"); w.Code != http.StatusNotFound {
		t.Fatalf("non-GET must not render the site, got %d", w.Code)
	}

	Configure(ModeBlog, "", "")
	if body := serve(t, http.MethodGet, "/index.html").Body.String(); !strings.Contains(body, DefaultTitle(ModeBlog)) {
		t.Fatal("an empty site name falls back to the template default")
	}
	Configure("bogus", "", "")
	if Enabled() {
		t.Fatal("an unknown mode must switch the decoy off")
	}
}

func TestServeCustomSite(t *testing.T) {
	t.Cleanup(func() { Configure(ModeOff, "", "") })
	dir := t.TempDir()
	for name, content := range map[string]string{
		"index.html":       "home",
		"about/index.html": "about",
		"css/site.css":     "body{}",
		".git/config":      "secret",
		"empty/note.txt":   "x",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	Configure(ModeCustom, "", dir)
	cases := []struct {
		path string
		code int
		body string
	}{
		{"/", 200, "home"},
		{"/about/", 200, "about"},
		{"/css/site.css", 200, "body{}"},
		{"/.git/config", 404, "404 Not Found"},
		{"/empty/", 404, "404 Not Found"},
		{"/../../etc/passwd", 404, "404 Not Found"},
	}
	for _, c := range cases {
		w := serve(t, http.MethodGet, c.path)
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s = %d %q, want %d containing %q", c.path, w.Code, w.Body.String(), c.code, c.body)
		}
	}
}

func zipOf(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestInstallZip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "site")
	r := zipOf(t, map[string]string{"mysite/index.html": "v1", "mysite/img/logo.svg": "<svg/>"})
	n, err := InstallZip(r, r.Size(), dir)
	if err != nil || n != 2 {
		t.Fatalf("InstallZip = %d, %v", n, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(b) != "v1" {
		t.Fatal("a single top folder must be unpacked from inside it")
	}

	r = zipOf(t, map[string]string{"index.html": "v2"})
	if _, err := InstallZip(r, r.Size(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "img", "logo.svg")); !os.IsNotExist(err) {
		t.Fatal("a new upload must replace the old site entirely")
	}

	for name, files := range map[string]map[string]string{
		"zip slip":      {"index.html": "x", "../escape.html": "x"},
		"absolute path": {"index.html": "x", "/etc/cron.d/x": "x"},
		"no index":      {"home.html": "x"},
	} {
		r := zipOf(t, files)
		if _, err := InstallZip(r, r.Size(), dir); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "index.html")); string(b) != "v2" {
		t.Fatal("a rejected upload must leave the installed site alone")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.html")); !os.IsNotExist(err) {
		t.Fatal("zip slip wrote outside the site folder")
	}
}
