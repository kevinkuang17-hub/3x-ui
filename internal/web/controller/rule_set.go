package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// RuleSetController serves the routing rule library and the global rule picks under /panel/api/ruleSets.
type RuleSetController struct {
	ruleSets service.RuleSetService
}

func NewRuleSetController(g *gin.RouterGroup) *RuleSetController {
	a := &RuleSetController{}
	g.GET("/list", a.list)
	g.POST("/save", a.save)
	g.POST("/del/:id", a.del)
	g.POST("/reset/:id", a.reset)
	g.GET("/routing", a.routing)
	g.POST("/routing", a.saveRouting)
	g.POST("/preview", a.preview)
	return a
}

func (a *RuleSetController) list(c *gin.Context) {
	rows, err := a.ruleSets.List()
	jsonObj(c, rows, err)
}

func (a *RuleSetController) save(c *gin.Context) {
	var body model.RuleSet
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	row, err := a.ruleSets.Save(body)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.ruleSets.saved"), row, nil)
}

func (a *RuleSetController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err == nil {
		err = a.ruleSets.Delete(id)
	}
	jsonMsg(c, I18nWeb(c, "pages.ruleSets.deleted"), err)
}

func (a *RuleSetController) reset(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	row, err := a.ruleSets.ResetBuiltin(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.ruleSets.saved"), row, nil)
}

func (a *RuleSetController) routing(c *gin.Context) {
	ch, err := a.ruleSets.GlobalRouting()
	jsonObj(c, ch, err)
}

func (a *RuleSetController) saveRouting(c *gin.Context) {
	var body service.RoutingChoice
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	err := a.ruleSets.SaveGlobalRouting(body)
	jsonMsg(c, I18nWeb(c, "pages.ruleSets.saved"), err)
}

// preview shows the rules a choice adds in front of the template, so the admin can check them before saving.
func (a *RuleSetController) preview(c *gin.Context) {
	var body service.RoutingChoice
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if _, _, err := service.NormalizeChoice(body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	rules, err := a.ruleSets.BuildRules(body)
	jsonObj(c, gin.H{"rules": rules}, err)
}
