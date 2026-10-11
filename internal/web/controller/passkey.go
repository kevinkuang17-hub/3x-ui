package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

// PasskeyController manages the signed-in user's passkeys under /panel/api/passkeys.
type PasskeyController struct {
	passkeys service.PasskeyService
}

func NewPasskeyController(g *gin.RouterGroup) *PasskeyController {
	a := &PasskeyController{}
	g.GET("/list", a.list)
	g.POST("/register/begin", a.registerBegin)
	g.POST("/register/finish", a.registerFinish)
	g.POST("/del/:id", a.del)
	return a
}

func relyingParty(c *gin.Context) (*webauthn.WebAuthn, string, error) {
	forwardedHTTPS := isTrustedForwardedRequest(c) && strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	return service.RelyingParty(c.Request, forwardedHTTPS)
}

func sessionUserID(c *gin.Context) int {
	if u := realLoginUser(c); u != nil {
		return u.Id
	}
	return 0
}

func (a *PasskeyController) list(c *gin.Context) {
	rows, err := a.passkeys.List(sessionUserID(c))
	jsonObj(c, rows, err)
}

func (a *PasskeyController) registerBegin(c *gin.Context) {
	w, rpID, err := relyingParty(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.passkeys.failed"), err)
		return
	}
	flow, options, err := a.passkeys.BeginRegistration(w, rpID, sessionUserID(c))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.passkeys.failed"), err)
		return
	}
	jsonObj(c, gin.H{"flow": flow, "options": options}, nil)
}

func (a *PasskeyController) registerFinish(c *gin.Context) {
	w, rpID, err := relyingParty(c)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.passkeys.failed"), err)
		return
	}
	row, err := a.passkeys.FinishRegistration(w, rpID, c.Query("flow"), sessionUserID(c), c.Query("name"), c.Request)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.passkeys.failed"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.settings.passkeys.added"), row, nil)
}

func (a *PasskeyController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.passkeys.deleted"), a.passkeys.Delete(sessionUserID(c), id))
}

// PasskeyLoginBegin is public: it starts a username-less passkey sign-in.
func PasskeyLoginBegin(c *gin.Context) {
	if _, ok := defaultLoginLimiter.allow(getRemoteIp(c), "passkey"); !ok {
		pureJsonMsg(c, http.StatusOK, false, I18nWeb(c, "pages.login.toasts.wrongUsernameOrPassword"))
		return
	}
	w, rpID, err := relyingParty(c)
	if err != nil {
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	flow, options, err := (&service.PasskeyService{}).BeginLogin(w, rpID)
	if err != nil {
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "obj": gin.H{"flow": flow, "options": options}})
}

// PasskeyLoginFinish is public: a verified passkey signs the browser in, like a password login.
func PasskeyLoginFinish(c *gin.Context) {
	remoteIP := getRemoteIp(c)
	if blockedUntil, ok := defaultLoginLimiter.allow(remoteIP, "passkey"); !ok {
		logger.Warningf("failed passkey login: IP=%q, blocked_until=%s", remoteIP, blockedUntil)
		pureJsonMsg(c, http.StatusOK, false, I18nWeb(c, "pages.login.toasts.wrongUsernameOrPassword"))
		return
	}
	w, rpID, err := relyingParty(c)
	if err != nil {
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	user, err := (&service.PasskeyService{}).FinishLogin(w, rpID, c.Query("flow"), c.Request)
	if err != nil {
		defaultLoginLimiter.registerFailure(remoteIP, "passkey")
		logger.Warningf("failed passkey login: IP=%q, reason=%q", remoteIP, err.Error())
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	defaultLoginLimiter.registerSuccess(remoteIP, "passkey")
	if err := session.SetLoginUser(c, user); err != nil {
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	logger.Infof("logged in with a passkey: username=%q, IP=%q", user.Username, remoteIP)
	jsonMsg(c, I18nWeb(c, "pages.login.toasts.successLogin"), nil)
}
