package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

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
	// The subscription server serves the same routes and is often the address China still reaches.
	jsonObj(c, gin.H{"probe": p, "path": basePathOf(c) + "probe/" + token, "subUrl": a.probes.SubProbeURL(token)}, nil)
}

func (a *IspProbeController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.nodes.probes.deleted"), a.probes.Delete(id))
}

// RemoteIP is the caller's address as the panel trusts it, for routes registered elsewhere.
func RemoteIP(c *gin.Context) string { return getRemoteIp(c) }
