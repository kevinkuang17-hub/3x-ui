// Package probeapi serves the ISP probe agent (download, targets, report) on both the panel and
// the subscription server: from inside China the panel domain may be blocked while the
// subscription domain, often behind a CDN, still answers.
package probeapi

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

//go:embed probe-agent.sh
var agentScript []byte

// Register adds /probe/:token routes to g; remoteIP names the caller as the server trusts it.
func Register(g *gin.RouterGroup, remoteIP func(*gin.Context) string) {
	g.GET("/probe/:token", func(c *gin.Context) {
		text, err := (&service.IspProbeService{}).TargetsText(c.Param("token"), remoteIP(c))
		c.Header("Cache-Control", "no-store")
		if err != nil {
			c.String(http.StatusForbidden, err.Error())
			return
		}
		c.String(http.StatusOK, text)
	})
	g.POST("/probe/:token", func(c *gin.Context) {
		var req struct {
			Results []service.ProbeReportItem `json:"results"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
			return
		}
		n, err := (&service.IspProbeService{}).Report(c.Param("token"), remoteIP(c), req.Results)
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"stored": n}})
	})
	g.GET("/probe/:token/agent.sh", serveAgent)
	// Self-enrollment: every router runs the same command; each gets its own probe token back.
	g.GET("/probe-enroll/:key/agent.sh", serveAgent)
	g.POST("/probe-enroll/:key", func(c *gin.Context) {
		token, err := (&service.IspProbeService{}).Enroll(c.Param("key"), c.PostForm("deviceId"), c.PostForm("name"), remoteIP(c))
		c.Header("Cache-Control", "no-store")
		if err != nil {
			c.String(http.StatusForbidden, err.Error())
			return
		}
		c.String(http.StatusOK, token)
	})
}

func serveAgent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", agentScript)
}
