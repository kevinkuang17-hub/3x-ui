package controller

import (
	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// AppTrafficController exposes per-app traffic under /panel/api/apps.
type AppTrafficController struct {
	apps        service.AppTrafficService
	xrayService service.XrayService
}

func NewAppTrafficController(g *gin.RouterGroup) *AppTrafficController {
	a := &AppTrafficController{}
	g.GET("/status", a.status)
	g.POST("/set", a.set)
	return a
}

func (a *AppTrafficController) status(c *gin.Context) {
	enabled, err := a.apps.EnabledApps()
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	traffic, err := a.apps.Traffic()
	jsonObj(c, gin.H{"catalog": service.AppCatalog(), "enabled": enabled, "traffic": traffic}, err)
}

func (a *AppTrafficController) set(c *gin.Context) {
	var req struct {
		Apps []string `json:"apps"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.apps.SetApps(req.Apps); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if a.xrayService.IsXrayRunning() {
		if err := a.xrayService.RestartXray(false); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	jsonMsg(c, I18nWeb(c, "pages.xray.apps.saved"), nil)
}
