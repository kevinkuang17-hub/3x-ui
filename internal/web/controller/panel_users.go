package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

// PanelUserController manages extra panel logins under /panel/api/users (admins only, except /me).
type PanelUserController struct {
	users service.PanelUserService
}

func NewPanelUserController(g *gin.RouterGroup) *PanelUserController {
	a := &PanelUserController{}
	g.GET("/me", a.me)
	g.GET("/list", a.list)
	g.POST("/add", a.add)
	g.POST("/update/:id", a.update)
	g.POST("/del/:id", a.del)
	return a
}

func (a *PanelUserController) me(c *gin.Context) {
	u := session.GetLoginUser(c)
	if u == nil {
		jsonObj(c, nil, nil)
		return
	}
	jsonObj(c, gin.H{"id": u.Id, "username": u.Username, "role": service.RoleOf(u.Id)}, nil)
}

func (a *PanelUserController) list(c *gin.Context) {
	users, err := a.users.List()
	jsonObj(c, users, err)
}

type panelUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (a *PanelUserController) add(c *gin.Context) {
	var req panelUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	u, err := a.users.Add(req.Username, req.Password, req.Role)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.settings.users.saved"), u, nil)
}

func (a *PanelUserController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var req panelUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.users.saved"), a.users.Update(id, req.Password, req.Role))
}

func (a *PanelUserController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	acting := 0
	if u := session.GetLoginUser(c); u != nil {
		acting = u.Id
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.users.deleted"), a.users.Delete(id, acting))
}
