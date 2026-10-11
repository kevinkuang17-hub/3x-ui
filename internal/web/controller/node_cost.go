package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// NodeCostController tracks server costs and renewal dates under /panel/api/nodes/cost.
type NodeCostController struct {
	costs service.NodeCostService
}

func NewNodeCostController(g *gin.RouterGroup) *NodeCostController {
	a := &NodeCostController{}
	g.GET("/list", a.list)
	g.POST("/set", a.set)
	g.POST("/del/:id", a.del)
	return a
}

func (a *NodeCostController) list(c *gin.Context) {
	rows, err := a.costs.List()
	jsonObj(c, rows, err)
}

func (a *NodeCostController) set(c *gin.Context) {
	var row model.NodeCost
	if err := c.ShouldBindJSON(&row); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.costs.Set(&row); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.nodes.cost.saved"), row, nil)
}

func (a *NodeCostController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.nodes.cost.saved"), a.costs.Delete(id))
}
