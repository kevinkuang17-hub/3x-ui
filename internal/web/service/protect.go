package service

import (
	"io"
	"os"
	"path/filepath"
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
	Mode       string   `json:"mode"`
	Title      string   `json:"title"`
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

// ApplyStored loads the stored decoy and allowlist into the running servers.
func (s *ProtectService) ApplyStored() {
	mode, _ := s.settingService.getString("decoyMode")
	title, _ := s.settingService.getString("decoyTitle")
	decoy.Configure(mode, title, DecoySiteDir())
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
	title, err := s.settingService.getString("decoyTitle")
	if err != nil {
		return nil, err
	}
	base, _ := s.settingService.GetBasePath()
	return &DecoySettings{
		Mode: mode, Title: title, Modes: decoy.Modes(), CustomSite: customSiteInstalled(),
		SecretPath: strings.Trim(base, "/") != "",
	}, nil
}

func (s *ProtectService) SaveDecoy(mode, title string) error {
	if !decoy.ValidMode(mode) {
		return common.NewError("unknown decoy mode:", mode)
	}
	if mode == decoy.ModeCustom && !customSiteInstalled() {
		return common.NewError("upload a site archive (zip with index.html) first")
	}
	title = strings.TrimSpace(title)
	if len(title) > 80 {
		return common.NewError("the site title is at most 80 characters")
	}
	if err := s.settingService.setString("decoyMode", mode); err != nil {
		return err
	}
	if err := s.settingService.setString("decoyTitle", title); err != nil {
		return err
	}
	decoy.Configure(mode, title, DecoySiteDir())
	return nil
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

func allowsWith(raw, ip string) bool {
	nets, err := panelaccess.Parse(raw)
	if err != nil {
		return false
	}
	parsed, _ := panelaccess.Parse(ip)
	if len(parsed) == 0 {
		return false
	}
	if parsed[0].IP.IsLoopback() {
		return true
	}
	for _, n := range nets {
		if n.Contains(parsed[0].IP) {
			return true
		}
	}
	return false
}

// ClearPanelAccess empties the stored allowlist; the CLI uses it to recover a locked-out panel.
func (s *SettingService) ClearPanelAccess() error {
	return s.setString("panelAllowIps", "")
}
