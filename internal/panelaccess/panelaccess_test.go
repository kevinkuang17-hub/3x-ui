package panelaccess

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAllowsAndParse(t *testing.T) {
	t.Cleanup(func() { _ = Configure("") })
	if _, err := Parse("1.2.3.4, nope"); err == nil {
		t.Fatal("garbage must be rejected")
	}
	if err := Configure("203.0.113.5\n198.51.100.0/24 2001:db8::1"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"203.0.113.5": true, "203.0.113.6": false, "198.51.100.200": true,
		"2001:db8::1": true, "2001:db8::2": false, "127.0.0.1": true, "::1": true,
	}
	for ip, want := range cases {
		if got := Allows(net.ParseIP(ip)); got != want {
			t.Errorf("Allows(%s) = %v, want %v", ip, got, want)
		}
	}
	_ = Configure("")
	if !Allows(net.ParseIP("192.0.2.1")) {
		t.Fatal("an empty list must let everyone in")
	}
}

func TestMiddlewareUsesThePeerNotForwardedHeaders(t *testing.T) {
	t.Cleanup(func() { _ = Configure("") })
	if err := Configure("203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Middleware([]string{"/s/probe/"}, func(c *gin.Context) { c.AbortWithStatus(http.StatusTeapot) }))
	engine.GET("/*any", func(c *gin.Context) { c.Status(http.StatusOK) })

	do := func(path, remote, forwarded string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = remote
		if forwarded != "" {
			req.Header.Set("X-Forwarded-For", forwarded)
			req.Header.Set("X-Real-IP", forwarded)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w.Code
	}
	if got := do("/s/", "198.51.100.7:1000", "203.0.113.5"); got != http.StatusTeapot {
		t.Fatalf("a forged forwarding header must not pass, got %d", got)
	}
	if got := do("/s/", "203.0.113.5:1000", ""); got != http.StatusOK {
		t.Fatalf("the allowed peer must pass, got %d", got)
	}
	if got := do("/s/", "127.0.0.1:1000", "198.51.100.7"); got != http.StatusTeapot {
		t.Fatalf("a client behind a local reverse proxy must be judged by its own address, got %d", got)
	}
	if got := do("/s/", "127.0.0.1:1000", "203.0.113.5"); got != http.StatusOK {
		t.Fatalf("an allowed client behind a local reverse proxy must pass, got %d", got)
	}
	if got := do("/s/probe/abc", "198.51.100.7:1000", ""); got != http.StatusOK {
		t.Fatalf("public routes must pass, got %d", got)
	}
}
