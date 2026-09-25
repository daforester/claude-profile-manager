// Package settings stores app-wide preferences (tool paths, terminal choice).
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Terminal choices. Not every value applies to every OS; "auto" always does.
const (
	TerminalAuto            = "auto"
	TerminalWindowsTerminal = "windows-terminal" // Windows
	TerminalCmd             = "cmd"              // Windows
	TerminalPowerShell      = "powershell"       // Windows
	TerminalMacTerminal     = "terminal"         // macOS Terminal.app
	TerminalITerm           = "iterm"            // macOS iTerm2
	TerminalCustom          = "custom"           // any OS: CustomTerminal template
)

// Settings are global preferences.
type Settings struct {
	// ClaudeCLIPath overrides auto-detection of the `claude` executable.
	ClaudeCLIPath string `json:"claudeCliPath,omitempty"`
	// ClaudeDesktopPath overrides auto-detection of Claude Desktop
	// (claude.exe on Windows, Claude.app on macOS, a binary on Linux).
	ClaudeDesktopPath string `json:"claudeDesktopPath,omitempty"`
	// Terminal selects how Claude Code windows are opened.
	Terminal string `json:"terminal,omitempty"`
	// CustomTerminal is a command template used when Terminal is "custom".
	// {script} is replaced with the launch script path, {title} with the
	// window title, e.g. `kitty --title {title} {script}`.
	CustomTerminal string `json:"customTerminal,omitempty"`
	// HideTray turns the system-tray icon off. With the tray on (the
	// default) closing the window keeps the app resident; quit from the
	// tray menu. Changes apply on the next start.
	HideTray bool `json:"hideTray,omitempty"`

	// UsageRefreshMinutes is how often plan usage is polled per profile.
	UsageRefreshMinutes int `json:"usageRefreshMinutes,omitempty"`
	// TrayIconStyle is how per-profile tray icons show usage: "bar" or "percent".
	TrayIconStyle string `json:"trayIconStyle,omitempty"`
	// TrayMetric selects which limit tray icons track: "session", "weekly" or "max".
	TrayMetric string `json:"trayMetric,omitempty"`

	// Usage pop-out window.
	PopoutOpen   bool     `json:"popoutOpen,omitempty"`
	PopoutPinned bool     `json:"popoutPinned,omitempty"`
	PopoutHidden []string `json:"popoutHidden,omitempty"` // profile IDs not listed
	// RouteLinks makes Profile Manager the claude:// handler so Claude
	// Desktop sign-in callbacks reach the profile that asked for them.
	RouteLinks bool `json:"routeLinks,omitempty"`
	// RouteLinksAsked records that the user has been offered RouteLinks.
	RouteLinksAsked bool `json:"routeLinksAsked,omitempty"`
	// PortableDesktopVersion records the Claude Desktop version last copied
	// out of the Windows MSIX package (see launcher.MakePortableDesktop).
	PortableDesktopVersion string `json:"portableDesktopVersion,omitempty"`
}

func path(root string) string { return filepath.Join(root, "settings.json") }

// Load reads settings from root, returning defaults if none exist.
func Load(root string) (*Settings, error) {
	s := &Settings{}
	defer s.normalise()
	data, err := os.ReadFile(path(root))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return s, err
	}
	s.normalise()
	return s, nil
}

// Save writes settings to root.
func (s *Settings) Save(root string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(root), data, 0o600)
}

// Tray icon styles and metrics.
const (
	TrayStyleBar     = "bar"
	TrayStylePercent = "percent"
	MetricSession    = "session"
	MetricWeekly     = "weekly"
	MetricMax        = "max"
)

func (s *Settings) normalise() {
	if s.Terminal == "" {
		s.Terminal = TerminalAuto
	}
	if s.UsageRefreshMinutes <= 0 {
		s.UsageRefreshMinutes = 5
	}
	if s.TrayIconStyle == "" {
		s.TrayIconStyle = TrayStyleBar
	}
	if s.TrayMetric == "" {
		s.TrayMetric = MetricSession
	}
}

// PopoutShows reports whether the pop-out lists the profile.
func (s *Settings) PopoutShows(id string) bool {
	for _, h := range s.PopoutHidden {
		if h == id {
			return false
		}
	}
	return true
}

// SetPopoutShows includes or excludes a profile from the pop-out.
func (s *Settings) SetPopoutShows(id string, show bool) {
	out := s.PopoutHidden[:0]
	for _, h := range s.PopoutHidden {
		if h != id {
			out = append(out, h)
		}
	}
	if !show {
		out = append(out, id)
	}
	s.PopoutHidden = out
}
