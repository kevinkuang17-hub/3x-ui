package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// ConfigHistoryController exposes Xray template versions under /panel/api/xray/history.
type ConfigHistoryController struct {
	history     service.ConfigHistoryService
	xrayService service.XrayService
}

func NewConfigHistoryController(g *gin.RouterGroup) *ConfigHistoryController {
	a := &ConfigHistoryController{}
	g.GET("/list", a.list)
	g.GET("/get/:id", a.get)
	g.POST("/restore/:id", a.restore)
	return a
}

func (a *ConfigHistoryController) list(c *gin.Context) {
	snaps, err := a.history.List()
	jsonObj(c, snaps, err)
}

func (a *ConfigHistoryController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	snap, err := a.history.Get(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"id": snap.Id, "createdAt": snap.CreatedAt, "good": snap.Good, "note": snap.Note, "content": snap.Content}, nil)
}

func (a *ConfigHistoryController) restore(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if _, err := a.history.Restore(id, "restored from version "+strconv.Itoa(id)); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.xray.history.restoreFailed"), err)
		return
	}
	if a.xrayService.IsXrayRunning() {
		if err := a.xrayService.RestartXray(false); err != nil {
			jsonMsg(c, I18nWeb(c, "pages.xray.history.restoreFailed"), err)
			return
		}
	}
	jsonMsg(c, I18nWeb(c, "pages.xray.history.restored"), nil)
}
