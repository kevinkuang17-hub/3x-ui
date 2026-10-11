package service

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// AppTagPrefix marks the outbounds and rules this feature owns in the Xray template.
const AppTagPrefix = "xui-app-"

// appCatalog maps an app key to the geosite categories that identify its traffic.
var appCatalog = map[string][]string{
	"netflix":   {"geosite:netflix"},
	"youtube":   {"geosite:youtube"},
	"google":    {"geosite:google"},
	"telegram":  {"geosite:telegram"},
	"openai":    {"geosite:openai"},
	"tiktok":    {"geosite:tiktok"},
	"spotify":   {"geosite:spotify"},
	"disney":    {"geosite:disney"},
	"meta":      {"geosite:facebook", "geosite:instagram"},
	"twitter":   {"geosite:twitter"},
	"github":    {"geosite:github"},
	"apple":     {"geosite:apple"},
	"microsoft": {"geosite:microsoft"},
	"steam":     {"geosite:steam"},
}

// AppTrafficService measures traffic per app without changing where it goes: each tracked app
// gets an outbound cloned from the default (first) outbound and a routing rule appended after
// all existing rules, so Xray's per-outbound stats count it while user rules still win.
type AppTrafficService struct {
	settingService     SettingService
	xraySettingService XraySettingService
}

func AppCatalog() []string {
	keys := make([]string, 0, len(appCatalog))
	for k := range appCatalog {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *AppTrafficService) template() (map[string]any, error) {
	raw, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, common.NewError("xray template is not valid JSON:", err)
	}
	return cfg, nil
}

func isAppTag(v any) bool {
	tag, _ := v.(string)
	return strings.HasPrefix(tag, AppTagPrefix)
}

// stripApps removes this feature's outbounds and rules from a parsed template.
func stripApps(cfg map[string]any) {
	if outs, ok := cfg["outbounds"].([]any); ok {
		kept := outs[:0:0]
		for _, o := range outs {
			if m, ok := o.(map[string]any); ok && isAppTag(m["tag"]) {
				continue
			}
			kept = append(kept, o)
		}
		cfg["outbounds"] = kept
	}
	if routing, ok := cfg["routing"].(map[string]any); ok {
		if rules, ok := routing["rules"].([]any); ok {
			kept := rules[:0:0]
			for _, r := range rules {
				if m, ok := r.(map[string]any); ok && isAppTag(m["outboundTag"]) {
					continue
				}
				kept = append(kept, r)
			}
			routing["rules"] = kept
		}
	}
}

// addApps appends one cloned outbound and one trailing rule per app, and turns on outbound stats.
func addApps(cfg map[string]any, apps []string) error {
	outs, _ := cfg["outbounds"].([]any)
	if len(outs) == 0 {
		return common.NewError("the Xray template has no outbound to clone")
	}
	base, ok := outs[0].(map[string]any)
	if !ok {
		return common.NewError("the first outbound is not an object")
	}
	// A second copy of a WireGuard outbound would open a competing tunnel with the same key.
	if proto, _ := base["protocol"].(string); proto == "wireguard" {
		return common.NewError("the default outbound is WireGuard; app tracking needs a freedom or proxy default outbound")
	}
	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
		cfg["routing"] = routing
	}
	// With IPIfNonMatch/IPOnDemand a new domain rule would win over the user's IP rules.
	if ds, _ := routing["domainStrategy"].(string); ds != "" && ds != "AsIs" {
		return common.NewError("app tracking needs routing domainStrategy AsIs; it is", ds)
	}
	rules, _ := routing["rules"].([]any)
	seen := map[string]bool{}
	for _, app := range apps {
		domains, known := appCatalog[app]
		if !known {
			return common.NewError("unknown app:", app)
		}
		if seen[app] {
			continue
		}
		seen[app] = true
		clone := make(map[string]any, len(base))
		for k, v := range base {
			clone[k] = v
		}
		clone["tag"] = AppTagPrefix + app
		outs = append(outs, clone)
		doms := make([]any, len(domains))
		for i, d := range domains {
			doms[i] = d
		}
		rules = append(rules, map[string]any{"type": "field", "domain": doms, "outboundTag": AppTagPrefix + app})
	}
	cfg["outbounds"] = outs
	routing["rules"] = rules
	// Xray only counts per-outbound bytes with these policy switches on.
	policy, _ := cfg["policy"].(map[string]any)
	if policy == nil {
		policy = map[string]any{}
		cfg["policy"] = policy
	}
	system, _ := policy["system"].(map[string]any)
	if system == nil {
		system = map[string]any{}
		policy["system"] = system
	}
	system["statsOutboundUplink"] = true
	system["statsOutboundDownlink"] = true
	return nil
}

// EnabledApps lists the apps currently tracked in the template.
func (s *AppTrafficService) EnabledApps() ([]string, error) {
	cfg, err := s.template()
	if err != nil {
		return nil, err
	}
	var apps []string
	outs, _ := cfg["outbounds"].([]any)
	for _, o := range outs {
		if m, ok := o.(map[string]any); ok && isAppTag(m["tag"]) {
			apps = append(apps, strings.TrimPrefix(m["tag"].(string), AppTagPrefix))
		}
	}
	sort.Strings(apps)
	return apps, nil
}

// SetApps rewrites the template so exactly these apps are tracked (none = feature off).
func (s *AppTrafficService) SetApps(apps []string) error {
	cfg, err := s.template()
	if err != nil {
		return err
	}
	stripApps(cfg)
	if len(apps) > 0 {
		if err := addApps(cfg, apps); err != nil {
			return err
		}
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return s.xraySettingService.SaveXraySetting(string(out))
}

type AppTraffic struct {
	App  string `json:"app"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

// Traffic returns the recorded bytes per tracked app, largest first.
func (s *AppTrafficService) Traffic() ([]AppTraffic, error) {
	var rows []model.OutboundTraffics
	if err := database.GetDB().Where("tag LIKE ?", AppTagPrefix+"%").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AppTraffic, 0, len(rows))
	for _, r := range rows {
		out = append(out, AppTraffic{App: strings.TrimPrefix(r.Tag, AppTagPrefix), Up: r.Up, Down: r.Down})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Up+out[i].Down > out[j].Up+out[j].Down })
	return out, nil
}
