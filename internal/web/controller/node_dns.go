package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// NodeDnsController manages Cloudflare DNS names for nodes under /panel/api/nodes/dns.
type NodeDnsController struct {
	dns service.NodeDnsService
}

func NewNodeDnsController(g *gin.RouterGroup) *NodeDnsController {
	a := &NodeDnsController{}
	g.GET("/list", a.list)
	g.POST("/set", a.set)
	g.POST("/sync/:id", a.sync)
	g.POST("/unlink/:id", a.unlink)
	return a
}

type nodeDnsView struct {
	NodeID     int    `json:"nodeId"`
	ReportPath string `json:"reportPath"`
	Name       string `json:"name"`
	LastIP     string `json:"lastIp"`
	ObservedIP string `json:"observedIp"`
	SyncedAt   int64  `json:"syncedAt"`
	LastError  string `json:"lastError"`
}

func (a *NodeDnsController) list(c *gin.Context) {
	rows, err := a.dns.List()
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	out := make([]nodeDnsView, 0, len(rows))
	for _, r := range rows {
		report := ""
		if r.ReportKey != "" {
			report = basePathOf(c) + "node-ip/" + r.ReportKey
		}
		out = append(out, nodeDnsView{r.NodeId, report, r.Name, r.LastIp, service.ObservedIP(r.NodeId), r.SyncedAt, r.LastError})
	}
	jsonObj(c, gin.H{"configured": a.dns.Configured(), "records": out, "script": JoinScriptURL}, nil)
}

type nodeDnsSetRequest struct {
	NodeID       int    `json:"nodeId"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	UseAsAddress bool   `json:"useAsAddress"`
}

func (a *NodeDnsController) set(c *gin.Context) {
	var req nodeDnsSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	row, err := a.dns.Set(ctx, req.NodeID, req.Name, req.IP, req.UseAsAddress)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.nodes.dns.failed"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.nodes.dns.saved"), row, nil)
}

type nodeDnsSyncRequest struct {
	IP string `json:"ip"`
}

func (a *NodeDnsController) sync(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var req nodeDnsSyncRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	row, err := a.dns.Sync(ctx, id, req.IP)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.nodes.dns.failed"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.nodes.dns.saved"), row, nil)
}

func (a *NodeDnsController) unlink(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.nodes.dns.unlinked"), a.dns.Unlink(id))
}
