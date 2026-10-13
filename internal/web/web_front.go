package web

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/network"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// denyPanelAccess answers an address outside the panel allowlist like a plain web server would.
func denyPanelAccess(c *gin.Context) {
	if decoy.Enabled() {
		decoy.Respond(c)
		return
	}
	c.AbortWithStatus(http.StatusNotFound)
}

// startFront also serves the panel on 443 when enabled and reports whether the panel port may go
// loopback-only; any failure keeps that port open so the admin is not locked out.
func (s *Server) startFront(engine http.Handler, certFile, keyFile string) bool {
	protect := &service.ProtectService{}
	enabled, frontOnly := protect.FrontSettings()
	if !enabled {
		service.SetFrontStatus(service.FrontStatus{})
		return false
	}
	if conflict := protect.FrontConflict(); conflict != "" {
		logger.Warning("Front website on port 443 not started: already used by", conflict)
		service.SetFrontStatus(service.FrontStatus{Error: "port 443 is used by " + conflict})
		return false
	}
	fail := func(err error) bool {
		logger.Warning("Front website on port 443 not started:", err)
		service.SetFrontStatus(service.FrontStatus{Error: err.Error()})
		return false
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fail(err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", net.JoinHostPort("", strconv.Itoa(service.FrontPort)))
	if err != nil {
		return fail(err)
	}
	s.frontServer = &http.Server{
		Handler:           engine,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	tlsLn := tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	go network.ServeHTTP(s.frontServer, tlsLn, "Front website")
	logger.Info("Front website running HTTPS on", ln.Addr())
	service.SetFrontStatus(service.FrontStatus{Listening: true, FrontOnly: frontOnly})
	return frontOnly
}
