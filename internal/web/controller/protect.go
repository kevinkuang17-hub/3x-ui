package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
	"github.com/mhsanaei/3x-ui/v3/internal/panelaccess"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service/panel"
)

// ProtectController serves Protect & speed under /panel/api/protect: the decoy site, the panel
// allowlist, Cloudflare proxying and network tuning, for this server and (from a master) its nodes.
type ProtectController struct {
	panel       panel.PanelService
	protect     service.ProtectService
	speedup     service.SpeedupService
	dns         service.NodeDnsService
	nodeService service.NodeService
	settings    service.SettingService
}

func NewProtectController(g *gin.RouterGroup) *ProtectController {
	a := &ProtectController{}
	g.GET("/speedup", a.speedupStatus)
	g.POST("/speedup/apply", a.speedupApply)
	g.POST("/speedup/revert", a.speedupRevert)
	g.GET("/access", a.access)
	g.POST("/access", a.saveAccess)
	g.POST("/access/lockToCaller", a.lockToCaller)
	g.GET("/decoy", a.decoy)
	g.POST("/decoy", a.saveDecoy)
	g.POST("/decoy/upload", a.uploadDecoy)
	g.POST("/decoy/preview", a.previewDecoy)
	g.GET("/decoy/page", a.decoyPage)
	g.GET("/firewall", a.firewall)
	g.POST("/firewall/allow", a.allowFirewall)
	g.POST("/portcheck", a.portCheck)
	g.GET("/front", a.front)
	g.POST("/front", a.saveFront)
	g.GET("/cf", a.cfInfo)
	g.POST("/cf/status", a.cfStatus)
	g.POST("/cf/proxied", a.cfProxied)
	g.GET("/nodes", a.nodes)
	g.POST("/nodes/:id/:action", a.nodeAction)
	return a
}

func (a *ProtectController) speedupStatus(c *gin.Context) {
	st, err := a.speedup.Status()
	jsonObj(c, st, err)
}

func (a *ProtectController) speedupApply(c *gin.Context) {
	st, err := a.speedup.Apply()
	jsonMsgObj(c, I18nWeb(c, "pages.protect.speedupApplied"), st, err)
}

func (a *ProtectController) speedupRevert(c *gin.Context) {
	st, err := a.speedup.Revert()
	jsonMsgObj(c, I18nWeb(c, "pages.protect.speedupReverted"), st, err)
}

func (a *ProtectController) access(c *gin.Context) {
	raw, err := a.protect.AllowIps()
	jsonObj(c, gin.H{"allowIps": raw, "callerIp": callerIP(c)}, err)
}

func callerIP(c *gin.Context) string {
	if ip := panelaccess.RemoteIP(c.Request); ip != nil {
		return ip.String()
	}
	return ""
}

func (a *ProtectController) saveAccess(c *gin.Context) {
	var body struct {
		AllowIps string `json:"allowIps"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.protect.saved"), a.protect.SaveAllowIps(body.AllowIps, callerIP(c)))
}

// lockToCaller is what a master calls on its node: afterwards only that master reaches the panel.
func (a *ProtectController) lockToCaller(c *gin.Context) {
	ip, err := a.protect.LockToCaller(callerIP(c))
	jsonObj(c, gin.H{"allowIps": ip}, err)
}

func (a *ProtectController) decoy(c *gin.Context) {
	st, err := a.protect.Decoy()
	jsonObj(c, st, err)
}

func (a *ProtectController) front(c *gin.Context) {
	jsonObj(c, a.protect.Front(), nil)
}

// saveFront stores the port-443 switches and restarts the panel, since listeners only change on start.
func (a *ProtectController) saveFront(c *gin.Context) {
	var body struct {
		Enabled   bool `json:"enabled"`
		FrontOnly bool `json:"frontOnly"`
		// Port is the website's TCP port; 0 keeps the current one.
		Port int `json:"port"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	viaFront := false
	if addr, ok := c.Request.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, port, err := net.SplitHostPort(addr.String()); err == nil {
			viaFront = port == strconv.Itoa(a.protect.FrontPort())
		}
	}
	if err := a.protect.SaveFront(body.Enabled, body.FrontOnly, viaFront, body.Port); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.protect.frontSaved"), a.panel.RestartPanel(3*time.Second))
}

type decoyBody struct {
	Mode string `json:"mode"`
	decoy.Site
}

func (a *ProtectController) saveDecoy(c *gin.Context) {
	var body decoyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.protect.decoyDeployed"), a.protect.SaveDecoy(body.Mode, body.Site))
}

// previewDecoy returns the rendered template so the panel can show it before deploying.
func (a *ProtectController) previewDecoy(c *gin.Context) {
	var body decoyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	page, err := a.protect.PreviewDecoy(body.Mode, body.Site)
	jsonObj(c, string(page), err)
}

// decoyPage hands the deployed decoy to the inbound form, which embeds it as Hysteria2's masquerade.
func (a *ProtectController) decoyPage(c *gin.Context) {
	page, err := a.protect.DecoyPage()
	jsonObj(c, page, err)
}

func (a *ProtectController) firewall(c *gin.Context) {
	rules, err := service.InboundFirewallRules()
	jsonObj(c, gin.H{"status": a.protect.Firewall(), "rules": rules}, err)
}

// allowFirewall opens the given ports in ufw, or every local inbound's ports when none are given.
func (a *ProtectController) allowFirewall(c *gin.Context) {
	var body struct {
		Rules []service.FirewallRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	rules := body.Rules
	if len(rules) == 0 {
		var err error
		if rules, err = service.InboundFirewallRules(); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	done, err := a.protect.AllowPorts(rules)
	jsonMsgObj(c, I18nWeb(c, "pages.protect.firewallOpened"), done, err)
}

// portCheck reports whether a port is bound here and let in by ufw; a master asks its nodes this.
func (a *ProtectController) portCheck(c *gin.Context) {
	var body struct {
		Port  int    `json:"port"`
		Proto string `json:"proto"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	pc, err := a.protect.CheckPort(body.Port, body.Proto)
	jsonObj(c, pc, err)
}

func (a *ProtectController) uploadDecoy(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	f, err := fh.Open()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	defer f.Close()
	n, err := a.protect.UploadSite(f, fh.Size)
	jsonMsgObj(c, I18nWeb(c, "pages.protect.siteUploaded"), gin.H{"files": n}, err)
}

func (a *ProtectController) cfInfo(c *gin.Context) {
	webDomain, _ := a.settings.GetWebDomain()
	port, _ := a.settings.GetPort()
	subDomain, _ := a.settings.GetSubDomain()
	subPort, _ := a.settings.GetSubPort()
	subEnable, _ := a.settings.GetSubEnable()
	token, _ := a.settings.GetCfApiToken()
	jsonObj(c, gin.H{
		"panelDomain": webDomain, "panelPort": port,
		"subDomain": subDomain, "subPort": subPort, "subEnable": subEnable,
		"cfConfigured": token != "",
	}, nil)
}

type cfProxyBody struct {
	Domain string `json:"domain"`
	Port   int    `json:"port"`
	On     bool   `json:"on"`
	// Panel marks the panel's own domain: behind Cloudflare every visitor arrives from its edge IPs.
	Panel bool `json:"panel"`
}

func (a *ProtectController) cfStatus(c *gin.Context) {
	var body cfProxyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	st, err := a.dns.ProxyStatus(ctx, body.Domain, body.Port)
	jsonObj(c, st, err)
}

func (a *ProtectController) cfProxied(c *gin.Context) {
	var body cfProxyBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if body.On && body.Panel && panelaccess.Active() {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), errProxyWithAllowlist)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	st, err := a.dns.SetProxied(ctx, body.Domain, body.Port, body.On)
	jsonMsgObj(c, I18nWeb(c, "pages.protect.saved"), st, err)
}

type nodeProtectRow struct {
	Id       int                    `json:"id"`
	Name     string                 `json:"name"`
	Address  string                 `json:"address"`
	Online   bool                   `json:"online"`
	Speedup  *service.SpeedupStatus `json:"speedup,omitempty"`
	AllowIps *string                `json:"allowIps,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// nodes asks every online node for its tuning and allowlist in parallel; one slow node only blanks its own row.
func (a *ProtectController) nodes(c *gin.Context) {
	list, err := a.nodeService.GetAll()
	if err != nil {
		jsonObj(c, nil, err)
		return
	}
	rows := make([]nodeProtectRow, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, n := range list {
		rows[i] = nodeProtectRow{Id: n.Id, Name: n.Name, Address: n.Address, Online: n.Enable && n.Status == "online"}
		if !rows[i].Online {
			continue
		}
		wg.Add(1)
		go func(row *nodeProtectRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var st service.SpeedupStatus
			if raw, err := a.call(ctx, row.Id, http.MethodGet, "speedup", nil); err != nil {
				row.Error = err.Error()
			} else if json.Unmarshal(raw, &st) == nil {
				row.Speedup = &st
			}
			var acc struct {
				AllowIps string `json:"allowIps"`
			}
			if raw, err := a.call(ctx, row.Id, http.MethodGet, "access", nil); err == nil && json.Unmarshal(raw, &acc) == nil {
				row.AllowIps = &acc.AllowIps
			}
		}(&rows[i])
	}
	wg.Wait()
	jsonObj(c, rows, nil)
}

func (a *ProtectController) call(ctx context.Context, nodeID int, method, path string, body any) (json.RawMessage, error) {
	mgr := runtime.GetManager()
	if mgr == nil {
		return nil, errNoRuntime
	}
	n, err := a.nodeService.GetById(nodeID)
	if err != nil || n == nil {
		return nil, errNodeNotFound
	}
	remote, err := mgr.RemoteFor(n)
	if err != nil {
		return nil, err
	}
	return remote.ProtectCall(ctx, method, path, body)
}

var nodeProtectActions = map[string]struct{ method, path string }{
	"speedupApply":  {http.MethodPost, "speedup/apply"},
	"speedupRevert": {http.MethodPost, "speedup/revert"},
	"lock":          {http.MethodPost, "access/lockToCaller"},
	"unlock":        {http.MethodPost, "access"},
	"firewallAllow": {http.MethodPost, "firewall/allow"},
}

func (a *ProtectController) nodeAction(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	action, known := nodeProtectActions[c.Param("action")]
	if err != nil || !known {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), errUnknownProtectAction)
		return
	}
	var body any
	switch c.Param("action") {
	case "unlock":
		body = map[string]string{"allowIps": ""}
	case "firewallAllow":
		// No rules: the node opens the ports of all its own inbounds.
		body = map[string]any{"rules": []any{}}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	raw, err := a.call(ctx, id, action.method, action.path, body)
	jsonMsgObj(c, I18nWeb(c, "pages.protect.saved"), raw, err)
}

var (
	errNoRuntime            = errors.New("runtime manager unavailable")
	errNodeNotFound         = errors.New("node not found")
	errUnknownProtectAction = errors.New("unknown node action")
	errProxyWithAllowlist   = errors.New("clear the panel access list first: behind Cloudflare every visitor, you included, comes from Cloudflare addresses")
)
