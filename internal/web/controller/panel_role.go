package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

const realUserKey = "panel_real_user"

// operatorWritePrefixes are the /panel/api areas an operator may change: clients and their
// groups, plans, and their own account and passkeys.
var operatorWritePrefixes = []string{"/clients/", "/plans/", "/users/me", "/passkeys/"}

// operatorWriteExact are read-style POST endpoints the client pages depend on.
var operatorWriteExact = map[string]bool{
	"/setting/defaultSettings": true,
	"/setting/validateRegex":   true,
}

// operatorReadPrefixes is an allowlist: a GET route added later stays admin-only until listed.
// Full inbound objects are left out on purpose (they carry REALITY/TLS private keys).
var operatorReadPrefixes = []string{
	"/clients/", "/plans/", "/users/me", "/passkeys/",
	"/inbounds/options",
	"/server/status", "/server/cpuHistory/", "/server/history/", "/server/getXrayVersion",
	"/server/getPanelUpdateInfo", "/server/xrayMetricsState", "/server/xrayMetricsHistory/",
	"/server/xrayObservatory",
	"/nodes/list", "/nodes/history/", "/nodes/cost/list",
	"/apps/status", "/probes/matrix",
}

func operatorAllowed(method, rel string) bool {
	if method == http.MethodGet || method == http.MethodHead {
		for _, p := range operatorReadPrefixes {
			if rel == p || strings.HasPrefix(rel, p) {
				return true
			}
		}
		return false
	}
	if operatorWriteExact[rel] {
		return true
	}
	for _, p := range operatorWritePrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// realLoginUser is the account actually signed in. Extra users act on the primary admin's
// data (inbounds are scoped to it), so handlers that care about identity must use this.
func realLoginUser(c *gin.Context) *model.User {
	if v, ok := c.Get(realUserKey); ok {
		if u, ok := v.(*model.User); ok {
			return u
		}
	}
	return session.GetLoginUser(c)
}

// enforcePanelRole limits operator logins to client management and lets extra users see the
// primary admin's inbounds; API tokens keep their own scopes and the primary admin is untouched.
func enforcePanelRole(c *gin.Context) {
	if c.GetBool("api_authed") {
		c.Next()
		return
	}
	user := session.GetLoginUser(c)
	if user == nil {
		c.Next()
		return
	}
	c.Set(realUserKey, user)
	if service.RoleOf(user.Id) == model.RoleOperator && !operatorAllowed(c.Request.Method, relAPIPath(c.FullPath())) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"success": false,
			"msg":     "your panel account (operator) is not allowed to do this",
		})
		return
	}
	if primary, err := service.PrimaryUser(); err == nil && primary.Id != user.Id {
		session.SetAPIAuthUser(c, primary)
	} else if err != nil {
		logger.Warning("panel role: primary user lookup failed:", err)
	}
	c.Next()
}

// DenyOperatorWebsocket keeps operators off the live feed, which broadcasts full inbounds.
func DenyOperatorWebsocket(c *gin.Context) {
	if user := session.GetLoginUser(c); user != nil && service.RoleOf(user.Id) == model.RoleOperator {
		c.AbortWithStatus(http.StatusForbidden)
		return
	}
	c.Next()
}
