package controller

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

// operatorWritePrefixes are the /panel/api areas an operator may change: clients and their
// groups, plans, and their own account and passkeys.
var operatorWritePrefixes = []string{"/clients/", "/plans/", "/users/me", "/passkeys/"}

// operatorWriteExact are read-style POST endpoints the client pages depend on.
var operatorWriteExact = map[string]bool{
	"/setting/defaultSettings": true,
	"/setting/validateRegex":   true,
}

// operatorHiddenReads are GET endpoints that would leak secrets or the whole database.
var operatorHiddenReads = []string{
	"/server/getDb", "/server/getConfigJson", "/server/getWebCertFiles", "/nodes/webCert",
	"/xray/history", "/setting/", "/users/list", "/server/clientIps",
}

func operatorAllowed(method, rel string) bool {
	if method == http.MethodGet || method == http.MethodHead {
		for _, p := range operatorHiddenReads {
			if strings.HasPrefix(rel, p) {
				return false
			}
		}
		return true
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

// enforcePanelRole limits operator logins to client management; API tokens keep their own
// scopes and admin sessions are unaffected.
func enforcePanelRole(c *gin.Context) {
	if c.GetBool("api_authed") {
		c.Next()
		return
	}
	user := session.GetLoginUser(c)
	if user == nil || service.RoleOf(user.Id) != model.RoleOperator {
		c.Next()
		return
	}
	if operatorAllowed(c.Request.Method, relAPIPath(c.FullPath())) {
		c.Next()
		return
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"success": false,
		"msg":     "your panel account (operator) is not allowed to do this",
	})
}
