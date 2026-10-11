package service

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/decoy"
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

// ClearPanelAccess empties the stored allowlist; the CLI uses it to recover a locked-out panel.
func (s *SettingService) ClearPanelAccess() error {
	return s.setString("panelAllowIps", "")
}
