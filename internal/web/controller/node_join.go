package controller

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// JoinScriptURL is the installer a new server runs; it lives in this fork's repository.
const JoinScriptURL = "https://raw.githubusercontent.com/kevinkuang17-hub/3x-ui/main/join-node.sh"

type NodeJoinController struct {
	join service.NodeJoinService
}

// NewNodeJoinController registers the admin side (/panel/api/nodes/join/*).
func NewNodeJoinController(g *gin.RouterGroup) *NodeJoinController {
	a := &NodeJoinController{}
	g.POST("/ticket", a.ticket)
	return a
}

func (a *NodeJoinController) ticket(c *gin.Context) {
	token, exp, err := a.join.MintTicket()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"token": token, "expiresAt": exp, "path": basePathOf(c) + "node-join/" + token, "script": JoinScriptURL}, nil)
}

type nodeJoinPrepareRequest struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
}

func joinError(c *gin.Context, status int, err error) {
	logger.Warningf("node join refused: IP=%q, reason=%q", getRemoteIp(c), err.Error())
	c.JSON(status, gin.H{"success": false, "msg": err.Error()})
}

// NodeJoinPrepare is public: the joining server proves itself with the ticket token.
func NodeJoinPrepare(c *gin.Context) {
	var req nodeJoinPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		joinError(c, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := (&service.NodeJoinService{}).Prepare(ctx, c.Param("token"), req.IP, req.Country, getRemoteIp(c))
	if err != nil {
		joinError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": res})
}

// NodeJoinFinish is public: it registers the freshly installed panel as a node.
func NodeJoinFinish(c *gin.Context) {
	var req service.NodeJoinFinish
	if err := c.ShouldBindJSON(&req); err != nil {
		joinError(c, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	res, err := (&service.NodeJoinService{}).Finish(ctx, c.Param("token"), req)
	if err != nil {
		joinError(c, http.StatusBadRequest, err)
		return
	}
	n := res.Node
	logger.Infof("node joined: name=%q address=%q", n.Name, n.Address)
	obj := gin.H{"id": n.Id, "name": n.Name, "address": n.Address, "reportPath": ""}
	if res.ReportKey != "" {
		obj["reportPath"] = basePathOf(c) + "node-ip/" + res.ReportKey
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": obj})
}

// NodeReportIP is public: a node's cron job calls it so its DNS record follows a new IP even when
// the master can no longer reach the node at the old one.
func NodeReportIP(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	var req struct {
		IP string `json:"ip"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		joinError(c, http.StatusBadRequest, err)
		return
	}
	ip, changed, err := (&service.NodeDnsService{}).ReportIP(ctx, c.Param("key"), req.IP, getRemoteIp(c))
	if err != nil {
		joinError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"ip": ip, "changed": changed}})
}
