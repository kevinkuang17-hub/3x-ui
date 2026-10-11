package controller

import (
	_ "embed"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// probeAgentScript is served by the master itself, which the probe can always reach, unlike
// GitHub from some networks.
//
//go:embed scripts/probe-agent.sh
var probeAgentScript []byte

// IspProbeController manages ISP probes under /panel/api/probes.
type IspProbeController struct {
	probes service.IspProbeService
}

func NewIspProbeController(g *gin.RouterGroup) *IspProbeController {
	a := &IspProbeController{}
	g.GET("/matrix", a.matrix)
	g.POST("/add", a.add)
	g.POST("/del/:id", a.del)
	return a
}

func (a *IspProbeController) matrix(c *gin.Context) {
	m, err := a.probes.Matrix()
	jsonObj(c, m, err)
}

type probeAddRequest struct {
	Name string `json:"name"`
	Isp  string `json:"isp"`
}

func (a *IspProbeController) add(c *gin.Context) {
	var req probeAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	p, token, err := a.probes.Add(req.Name, req.Isp)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"probe": p, "path": basePathOf(c) + "probe/" + token}, nil)
}

func (a *IspProbeController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.nodes.probes.deleted"), a.probes.Delete(id))
}

// ProbeTargets is public: the agent fetches "inboundId host port" lines with its token.
func ProbeTargets(c *gin.Context) {
	text, err := (&service.IspProbeService{}).TargetsText(c.Param("token"), getRemoteIp(c))
	c.Header("Cache-Control", "no-store")
	if err != nil {
		c.String(http.StatusForbidden, err.Error())
		return
	}
	c.String(http.StatusOK, text)
}

// ProbeReport is public: the agent posts its results with its token.
func ProbeReport(c *gin.Context) {
	var req struct {
		Results []service.ProbeReportItem `json:"results"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
		return
	}
	n, err := (&service.IspProbeService{}).Report(c.Param("token"), getRemoteIp(c), req.Results)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"stored": n}})
}

// ProbeAgent is public: the install command downloads the agent script from here.
func ProbeAgent(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", probeAgentScript)
}
