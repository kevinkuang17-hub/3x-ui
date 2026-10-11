package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// PlanController manages sellable plans under /panel/api/plans.
type PlanController struct {
	planService    service.PlanService
	clientService  service.ClientService
	inboundService service.InboundService
	xrayService    service.XrayService
}

func NewPlanController(g *gin.RouterGroup) *PlanController {
	a := &PlanController{}
	g.GET("/list", a.list)
	g.POST("/add", a.add)
	g.POST("/update/:id", a.update)
	g.POST("/del/:id", a.del)
	g.POST("/apply", a.apply)
	return a
}

func (a *PlanController) list(c *gin.Context) {
	plans, err := a.planService.List()
	jsonObj(c, plans, err)
}

func (a *PlanController) add(c *gin.Context) {
	var p model.Plan
	if err := c.ShouldBindJSON(&p); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.planService.Add(&p); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, p, nil)
}

func (a *PlanController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var p model.Plan
	if err := c.ShouldBindJSON(&p); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.planService.Update(id, &p); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, p, nil)
}

func (a *PlanController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.plans.deleted"), a.planService.Delete(id))
}

type planApplyRequest struct {
	PlanID   int      `json:"planId"`
	PlanName string   `json:"planName"`
	Emails   []string `json:"emails"`
	Mode     string   `json:"mode"`
}

func (a *PlanController) apply(c *gin.Context) {
	var req planApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	plan, err := a.planService.Get(req.PlanID, req.PlanName)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	res, needRestart, err := a.planService.Apply(&a.clientService, &a.inboundService, plan, req.Emails, req.Mode)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if needRestart {
		a.xrayService.SetToNeedRestart()
	}
	jsonObj(c, res, nil)
	notifyClientsChanged()
}
