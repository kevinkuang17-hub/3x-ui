package controller

import (
	"context"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/crm"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

// CrmController exposes CRM integration endpoints under /panel/api/crm (admin scope only).
type CrmController struct {
	crm *crm.Service
}

func NewCrmController(g *gin.RouterGroup) *CrmController {
	a := &CrmController{crm: crm.NewService()}
	g.POST("/sso", a.mintSSO)
	g.POST("/webhook/test", a.testWebhook)
	return a
}

type ssoRequest struct {
	Username string `json:"username"`
	Redirect string `json:"redirect"`
}

func basePathOf(c *gin.Context) string {
	base := c.GetString("base_path")
	if base == "" {
		base = "/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base
}

func (a *CrmController) mintSSO(c *gin.Context) {
	var req ssoRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	token, exp, err := a.crm.MintSSO(req.Username, req.Redirect)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	path := basePathOf(c) + "sso/" + token
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	jsonObj(c, gin.H{"path": path, "url": scheme + "://" + c.Request.Host + path, "expiresAt": exp}, nil)
}

func (a *CrmController) testWebhook(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := a.crm.Test(ctx); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.crmWebhookTestFailed"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.crmWebhookTestOk"), nil)
}

// SsoLanding answers GET /sso/:token with a page that POSTs the token back, so link-preview bots
// that only fetch the URL never burn the ticket or receive the session cookie.
func SsoLanding(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Robots-Tag", "noindex")
	if !crm.NewService().PeekSSO(c.Param("token")) {
		c.Redirect(http.StatusFound, basePathOf(c))
		return
	}
	nonce := html.EscapeString(c.GetString("csp_nonce"))
	page := `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<title>3X-UI</title></head><body style="font-family:sans-serif;display:flex;min-height:90vh;align-items:center;justify-content:center">` +
		`<form id="sso" method="post"><button type="submit" style="font-size:16px;padding:10px 24px">Continue to panel</button></form>` +
		`<script nonce="` + nonce + `">document.getElementById("sso").submit()</script></body></html>`
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

// SsoLogin redeems a CRM-minted single-use link: it sets the panel session and redirects.
func SsoLogin(c *gin.Context) {
	user, redirect, err := crm.NewService().ConsumeSSO(c.Param("token"))
	c.Header("Cache-Control", "no-store")
	base := basePathOf(c)
	if err != nil {
		logger.Warningf("sso login refused: IP=%q, reason=%q", getRemoteIp(c), err.Error())
		c.Redirect(http.StatusSeeOther, base)
		return
	}
	if err := session.SetLoginUser(c, user); err != nil {
		logger.Warning("sso: unable to save session:", err)
		c.Redirect(http.StatusSeeOther, base)
		return
	}
	logger.Infof("logged in via CRM SSO: username=%q, IP=%q", user.Username, getRemoteIp(c))
	target := base + "panel/"
	if redirect != "" {
		target = strings.TrimSuffix(base, "/") + redirect
	}
	c.Redirect(http.StatusSeeOther, target)
}
