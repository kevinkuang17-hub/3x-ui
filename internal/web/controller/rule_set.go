package controller

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/sub"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// RuleSetController serves the routing rule library and the global rule picks under /panel/api/ruleSets.
type RuleSetController struct {
	ruleSets service.RuleSetService
	clients  service.ClientService
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
	g.POST("/groups", a.groups)
	g.GET("/dns", a.dns)
	g.POST("/dns", a.saveDns)
	g.POST("/import", a.importConfig)
	g.GET("/client", a.clientRouting)
	g.POST("/client", a.saveClientRouting)
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
	v, err := a.ruleSets.GlobalRoutingView()
	jsonObj(c, v, err)
}

func (a *RuleSetController) saveRouting(c *gin.Context) {
	var body struct {
		service.RoutingChoice
		// Template is optional so older callers that send only the picks keep the template.
		Template *string `json:"template"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	// Check both parts before writing either, so a bad template does not leave the picks half saved.
	if _, _, err := service.NormalizeChoice(body.RoutingChoice); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if body.Template != nil {
		if err := a.ruleSets.SaveTemplate(*body.Template); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	jsonMsg(c, I18nWeb(c, "pages.ruleSets.saved"), a.ruleSets.SaveGlobalRouting(body.RoutingChoice))
}

// groups lists the policy groups of a template so picks can send traffic to them.
func (a *RuleSetController) groups(c *gin.Context) {
	var body struct {
		Template string `json:"template"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	names, err := sub.ClashTemplateGroups(body.Template)
	if names == nil {
		names = []string{}
	}
	jsonObj(c, names, err)
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

func (a *RuleSetController) dns(c *gin.Context) {
	jsonObj(c, a.ruleSets.GlobalDns(), nil)
}

func (a *RuleSetController) saveDns(c *gin.Context) {
	var body service.DnsConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.ruleSets.saved"), a.ruleSets.SaveGlobalDns(body))
}

// importConfig turns a Clash config (an https address or pasted YAML) into rule sets.
func (a *RuleSetController) importConfig(c *gin.Context) {
	var body struct {
		URL       string `json:"url"`
		Content   string `json:"content"`
		ImportDns bool   `json:"importDns"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	content, label := []byte(body.Content), I18nWeb(c, "pages.ruleSets.importPasted")
	if strings.TrimSpace(body.URL) != "" {
		fetched, host, err := sub.FetchClashConfig(body.URL)
		if err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
		content, label = fetched, host
	}
	res, err := a.ruleSets.ImportClashConfig(content, label, body.ImportDns)
	jsonMsgObj(c, I18nWeb(c, "pages.ruleSets.imported"), res, err)
}

func (a *RuleSetController) clientRouting(c *gin.Context) {
	v, err := a.clients.GetClientRouting(c.Query("email"))
	jsonObj(c, v, err)
}

type clientRoutingBody struct {
	Email string `json:"email"`
	// Own false makes the client follow its group's (or the global) rules again.
	Own        bool                  `json:"own"`
	ClashRules string                `json:"clashRules"`
	RuleSets   []service.RuleSetPick `json:"ruleSets"`
	ExtraRules string                `json:"extraRules"`
	// Dns null or omitted follows the group's or global DNS; an object sets the client's own.
	Dns json.RawMessage `json:"dns"`
}

func (a *RuleSetController) saveClientRouting(c *gin.Context) {
	var body clientRoutingBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	var choice *service.RoutingChoice
	if body.Own {
		choice = &service.RoutingChoice{RuleSets: body.RuleSets, ExtraRules: body.ExtraRules}
	}
	var dns *service.DnsConfig
	if len(body.Dns) > 0 && string(body.Dns) != "null" {
		dns = &service.DnsConfig{}
		if err := json.Unmarshal(body.Dns, dns); err != nil {
			jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
			return
		}
	}
	if err := a.clients.SetClientRouting(body.Email, body.ClashRules, choice, dns); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	v, err := a.clients.GetClientRouting(body.Email)
	jsonMsgObj(c, I18nWeb(c, "pages.ruleSets.saved"), v, err)
	notifyClientsChanged()
}
