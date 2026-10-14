package service

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
	"github.com/mhsanaei/3x-ui/v3/internal/linehealth"
	"github.com/mhsanaei/3x-ui/v3/internal/panelaccess"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// ProtectService holds the anti-detection settings: the decoy site and the panel IP allowlist.
type ProtectService struct {
	settingService SettingService
}

type DecoySettings struct {
	Mode string `json:"mode"`
	decoy.Site
	Modes      []string `json:"modes"`
	CustomSite bool     `json:"customSite"`
	// SecretPath is false when the panel sits at "/", where every path is the panel and no decoy can show.
	SecretPath bool `json:"secretPath"`
}

func DecoySiteDir() string {
	return filepath.Join(config.GetDBFolderPath(), "decoy-site")
}

func customSiteInstalled() bool {
	info, err := os.Stat(filepath.Join(DecoySiteDir(), "index.html"))
	return err == nil && !info.IsDir()
}

func (s *ProtectService) storedSite() decoy.Site {
	title, _ := s.settingService.getString("decoyTitle")
	tagline, _ := s.settingService.getString("decoyTagline")
	email, _ := s.settingService.getString("decoyEmail")
	return decoy.Site{Title: title, Tagline: tagline, Email: email}
}

// ApplyStored loads the stored decoy and allowlist into the running servers.
func (s *ProtectService) ApplyStored() {
	mode, _ := s.settingService.getString("decoyMode")
	decoy.Configure(mode, s.storedSite(), DecoySiteDir())
	raw, _ := s.settingService.getString("panelAllowIps")
	if err := panelaccess.Configure(raw); err != nil {
		// A stored list that no longer parses must not lock the admin out.
		_ = panelaccess.Configure("")
	}
}

func (s *ProtectService) Decoy() (*DecoySettings, error) {
	mode, err := s.settingService.getString("decoyMode")
	if err != nil {
		return nil, err
	}
	base, _ := s.settingService.GetBasePath()
	return &DecoySettings{
		Mode: mode, Site: s.storedSite(), Modes: decoy.Modes(), CustomSite: customSiteInstalled(),
		SecretPath: strings.Trim(base, "/") != "",
	}, nil
}

var emailPattern = regexp.MustCompile(`^[^@\s<>"]+@[^@\s<>"]+\.[^@\s<>"]+$`)

func cleanSite(site decoy.Site) (decoy.Site, error) {
	site.Title, site.Tagline, site.Email = strings.TrimSpace(site.Title), strings.TrimSpace(site.Tagline), strings.TrimSpace(site.Email)
	if len([]rune(site.Title)) > 80 || len([]rune(site.Tagline)) > 200 {
		return site, common.NewError("the site name is at most 80 characters and the slogan at most 200")
	}
	if site.Email != "" && !emailPattern.MatchString(site.Email) {
		return site, common.NewError("not an email address:", site.Email)
	}
	return site, nil
}

// SaveDecoy deploys the decoy: the chosen template with the admin's details, live at once.
func (s *ProtectService) SaveDecoy(mode string, site decoy.Site) error {
	if !decoy.ValidMode(mode) {
		return common.NewError("unknown decoy mode:", mode)
	}
	if mode == decoy.ModeCustom && !customSiteInstalled() {
		return common.NewError("upload a site archive (zip with index.html) first")
	}
	site, err := cleanSite(site)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{
		"decoyMode": mode, "decoyTitle": site.Title, "decoyTagline": site.Tagline, "decoyEmail": site.Email,
	} {
		if err := s.settingService.setString(key, value); err != nil {
			return err
		}
	}
	decoy.Configure(mode, site, DecoySiteDir())
	return nil
}

// maxDecoyPageBytes caps the page embedded into Xray's config as Hysteria2's masquerade.
const maxDecoyPageBytes = 512 << 10

// DecoyPage is the deployed decoy as one HTML document, which Hysteria2 serves over HTTP/3 to
// anyone who opens its address without the password.
func (s *ProtectService) DecoyPage() (string, error) {
	mode, _ := s.settingService.getString("decoyMode")
	var page []byte
	var err error
	switch {
	case mode == decoy.ModeCustom:
		if page, err = os.ReadFile(filepath.Join(DecoySiteDir(), "index.html")); err != nil {
			return "", common.NewError("upload a site archive (zip with index.html) first")
		}
	case mode == "" || mode == decoy.ModeOff || !decoy.ValidMode(mode):
		return "", common.NewError("deploy a decoy website on the Protect page first")
	default:
		if page, err = decoy.Page(mode, s.storedSite()); err != nil {
			return "", err
		}
	}
	if len(page) > maxDecoyPageBytes {
		return "", common.NewErrorf("the site page is over %d KB; use a smaller index.html", maxDecoyPageBytes>>10)
	}
	return string(page), nil
}

// PreviewDecoy renders a template with unsaved details for the panel's preview.
func (s *ProtectService) PreviewDecoy(mode string, site decoy.Site) ([]byte, error) {
	if mode == decoy.ModeOff || mode == decoy.ModeCustom || !decoy.ValidMode(mode) {
		return nil, common.NewError("no built-in template:", mode)
	}
	site, err := cleanSite(site)
	if err != nil {
		return nil, err
	}
	return decoy.Page(mode, site)
}

func (s *ProtectService) UploadSite(r io.ReaderAt, size int64) (int, error) {
	return decoy.InstallZip(r, size, DecoySiteDir())
}

func (s *ProtectService) AllowIps() (string, error) {
	return s.settingService.getString("panelAllowIps")
}

// SaveAllowIps stores and applies the allowlist; the address the admin saves from must be on it.
func (s *ProtectService) SaveAllowIps(raw, callerIP string) error {
	nets, err := panelaccess.Parse(raw)
	if err != nil {
		return common.NewError(err.Error())
	}
	parts := make([]string, 0, len(nets))
	for _, n := range nets {
		parts = append(parts, n.String())
	}
	normalized := strings.Join(parts, ",")
	if len(nets) > 0 && isLoopback(callerIP) {
		return common.NewError("the panel cannot see your real address (a local proxy without X-Real-IP?); open the panel directly to set the allowlist")
	}
	if len(nets) > 0 && !allowsWith(normalized, callerIP) {
		return common.NewError("the list must include the address you are using now:", callerIP)
	}
	if err := s.settingService.setString("panelAllowIps", normalized); err != nil {
		return err
	}
	return panelaccess.Configure(normalized)
}

// LockToCaller lets only the caller (the master, when a master asks) and loopback open this panel.
func (s *ProtectService) LockToCaller(callerIP string) (string, error) {
	if callerIP == "" {
		return "", common.NewError("cannot tell the caller's address")
	}
	return callerIP, s.SaveAllowIps(callerIP, callerIP)
}

func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

func allowsWith(raw, ip string) bool {
	nets, err := panelaccess.Parse(raw)
	if err != nil {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// ClearPanelAccess empties the allowlist and reopens the panel port; the CLI uses it to recover
// a locked-out panel.
func (s *SettingService) ClearPanelAccess() error {
	if err := s.setString("panelFrontOnly", "false"); err != nil {
		return err
	}
	return s.setString("panelAllowIps", "")
}

// DefaultFrontPort is where the decoy site is served unless the admin picks another port; on 443
// its address has no port, like any website.
const DefaultFrontPort = 443

// FrontPort is the TCP port the front website (decoy plus panel) listens on.
func (s *ProtectService) FrontPort() int {
	raw, _ := s.settingService.getString("decoyFrontPort")
	if port, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && port > 0 && port <= 65535 {
		return port
	}
	return DefaultFrontPort
}

// FrontStatus is what the running panel did with the front website at its last start.
type FrontStatus struct {
	Listening bool   `json:"listening"`
	FrontOnly bool   `json:"frontOnly"`
	Error     string `json:"error"`
}

var frontStatus atomic.Pointer[FrontStatus]

func SetFrontStatus(st FrontStatus) { frontStatus.Store(&st) }

func CurrentFrontStatus() FrontStatus {
	if st := frontStatus.Load(); st != nil {
		return *st
	}
	return FrontStatus{}
}

// FrontSettings reports whether the panel should also serve on port 443 and whether its own port
// should then be closed to the outside.
func (s *ProtectService) FrontSettings() (enabled, frontOnly bool) {
	enabled, _ = s.settingService.getBool("decoyFront")
	frontOnly, _ = s.settingService.getBool("panelFrontOnly")
	return enabled, enabled && frontOnly
}

type FrontView struct {
	Enabled   bool        `json:"enabled"`
	FrontOnly bool        `json:"frontOnly"`
	HasCert   bool        `json:"hasCert"`
	Conflict  string      `json:"conflict"`
	Port      int         `json:"port"`
	PanelPort int         `json:"panelPort"`
	Running   FrontStatus `json:"running"`
}

// frontConflict names what already listens on the TCP port here: a local Xray inbound, the panel or
// the subscription server. Starting the front site next to it would fail or crash the panel.
func (s *ProtectService) frontConflict(port int) string {
	var inbounds []model.Inbound
	database.GetDB().Where("port = ? AND enable = ? AND node_id IS NULL", port, true).Find(&inbounds)
	for i := range inbounds {
		if linehealth.IsUDP(&inbounds[i]) {
			continue
		}
		if inbounds[i].Remark != "" {
			return inbounds[i].Remark
		}
		return inbounds[i].Tag
	}
	if s.panelPort() == port {
		return "panel"
	}
	if on, _ := s.settingService.GetSubEnable(); on {
		if subPort, _ := s.settingService.GetSubPort(); subPort == port {
			return "subscription"
		}
	}
	return ""
}

// panelPort is the port the panel listens on, honouring the XUI_PORT override.
func (s *ProtectService) panelPort() int {
	if port, configured, err := config.GetPortOverride(); configured && err == nil {
		return port
	}
	port, _ := s.settingService.GetPort()
	return port
}

// FrontConflict is checked again at every start, since inbounds and ports can change later.
func (s *ProtectService) FrontConflict() string {
	return s.frontConflict(s.FrontPort())
}

func (s *ProtectService) Front() *FrontView {
	enabled, frontOnly := s.FrontSettings()
	cert, _ := s.settingService.GetCertFile()
	key, _ := s.settingService.GetKeyFile()
	return &FrontView{
		Enabled: enabled, FrontOnly: frontOnly, HasCert: cert != "" && key != "", Conflict: s.FrontConflict(),
		Port: s.FrontPort(), PanelPort: s.panelPort(), Running: CurrentFrontStatus(),
	}
}

// SaveFront stores the front-site switches; they apply when the panel restarts. Closing the panel
// port is only allowed while the front site is already up, so the admin keeps a way in.
// viaFront says the request itself came in on the front port, which proves that port is reachable
// from outside before the panel port is closed. port 0 keeps the current front port.
func (s *ProtectService) SaveFront(enabled, frontOnly, viaFront bool, port int) error {
	v := s.Front()
	if port == 0 {
		port = v.Port
	}
	if port < 1 || port > 65535 {
		return common.NewErrorf("not a port: %d", port)
	}
	if port != v.Port && v.FrontOnly {
		return common.NewError("open the panel port again before moving the website to another port")
	}
	if enabled && !v.HasCert {
		return common.NewError("the panel has no certificate; set one up first (x-ui menu → SSL)")
	}
	if conflict := s.frontConflict(port); enabled && conflict != "" {
		return common.NewErrorf("port %d is used by %q; move it or pick another port for the website", port, conflict)
	}
	if frontOnly && (!enabled || !v.Running.Listening || port != v.Port) {
		return common.NewErrorf("turn on the website on port %d and restart the panel first, then close the panel port", port)
	}
	if frontOnly && !v.FrontOnly && !viaFront {
		return common.NewErrorf("open the panel through the website address (port %d) and close the panel port from there, so you know that address works", port)
	}
	if frontOnly {
		if pool, _ := s.settingService.NodeMtlsClientCAPool(); pool != nil {
			return common.NewError("this panel is a node managed over mTLS; its master needs the panel port")
		}
	}
	if err := s.settingService.setString("decoyFront", strconv.FormatBool(enabled)); err != nil {
		return err
	}
	if err := s.settingService.setString("decoyFrontPort", strconv.Itoa(port)); err != nil {
		return err
	}
	return s.settingService.setString("panelFrontOnly", strconv.FormatBool(enabled && frontOnly))
}
