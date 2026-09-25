// Package profile models an isolated Claude login context and persists the
// collection of profiles to disk.
package profile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"claude-profile-manager/internal/shellwords"
)

// Profile is one isolated Claude context: its own Claude Code config
// directory (and therefore its own login), its own Claude Desktop user-data
// directory, and optional launch customisations.
type Profile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color,omitempty"` // #RRGGBB accent shown in the UI

	// ClaudeConfigDir overrides the Claude Code config directory
	// (CLAUDE_CONFIG_DIR). Empty means <root>/profiles/<id>/claude-code.
	ClaudeConfigDir string `json:"claudeConfigDir,omitempty"`
	// DesktopDataDir overrides the Claude Desktop --user-data-dir.
	// Empty means <root>/profiles/<id>/claude-desktop.
	DesktopDataDir string `json:"desktopDataDir,omitempty"`

	// WorkingDir is where Claude Code starts. Empty means the user's home.
	WorkingDir string `json:"workingDir,omitempty"`
	// ExtraArgs are appended to every Claude Code invocation,
	// e.g. `--model opus --permission-mode plan`.
	ExtraArgs string `json:"extraArgs,omitempty"`
	// Env holds extra environment variables (KEY=VALUE) for both apps.
	Env map[string]string `json:"env,omitempty"`

	// IsolateHome additionally points HOME/USERPROFILE at a per-profile
	// directory, for tools that ignore CLAUDE_CONFIG_DIR. Off by default
	// because it also hides your git/ssh config from Claude.
	IsolateHome bool `json:"isolateHome,omitempty"`
	// KeepInheritedAuth keeps ANTHROPIC_API_KEY & co. from the parent
	// environment. Off by default: an inherited key would silently override
	// the profile's own login.
	KeepInheritedAuth bool `json:"keepInheritedAuth,omitempty"`

	// TrayUsage shows a system-tray icon with this profile's plan usage.
	TrayUsage bool `json:"trayUsage,omitempty"`

	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`

	root string // manager data root, set by the Store
}

// Palette offers a few distinguishable accent colours for new profiles.
var Palette = []string{"#D97757", "#6A9BCC", "#788C5D", "#B45FA8", "#C9A227", "#4F9D9A", "#8C6D5A", "#E05D5D"}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// NewID returns a short random identifier that is safe as a directory name.
func NewID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("p%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Dir is the per-profile data directory managed by the app.
func (p *Profile) Dir() string { return filepath.Join(p.root, "profiles", p.ID) }

// ConfigDir is the effective CLAUDE_CONFIG_DIR for Claude Code.
func (p *Profile) ConfigDir() string {
	if p.ClaudeConfigDir != "" {
		return expandHome(p.ClaudeConfigDir)
	}
	return filepath.Join(p.Dir(), "claude-code")
}

// DesktopDir is the effective Electron --user-data-dir for Claude Desktop.
func (p *Profile) DesktopDir() string {
	if p.DesktopDataDir != "" {
		return expandHome(p.DesktopDataDir)
	}
	return filepath.Join(p.Dir(), "claude-desktop")
}

// HomeDir is the substitute home used when IsolateHome is on.
func (p *Profile) HomeDir() string { return filepath.Join(p.Dir(), "home") }

// StartDir is where Claude Code should start.
func (p *Profile) StartDir() string {
	if p.WorkingDir != "" {
		return expandHome(p.WorkingDir)
	}
	home, _ := os.UserHomeDir()
	return home
}

// Args parses ExtraArgs.
func (p *Profile) Args() ([]string, error) { return shellwords.Split(p.ExtraArgs) }

// Validate checks user-editable fields.
func (p *Profile) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New("name is required")
	}
	if p.Color != "" && !hexColor.MatchString(p.Color) {
		return fmt.Errorf("colour %q must look like #RRGGBB", p.Color)
	}
	if _, err := p.Args(); err != nil {
		return fmt.Errorf("extra arguments: %w", err)
	}
	for k := range p.Env {
		if k == "" || strings.ContainsAny(k, "= \t") {
			return fmt.Errorf("invalid environment variable name %q", k)
		}
	}
	if p.WorkingDir != "" {
		if st, err := os.Stat(expandHome(p.WorkingDir)); err != nil || !st.IsDir() {
			return fmt.Errorf("working directory %q does not exist", p.WorkingDir)
		}
	}
	return nil
}

// EnsureDirs creates the directories the profile needs before launch.
func (p *Profile) EnsureDirs() error {
	dirs := []string{p.ConfigDir(), p.DesktopDir()}
	if p.IsolateHome {
		dirs = append(dirs, p.HomeDir())
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// Slug is a filesystem- and CLI-friendly version of the name.
func (p *Profile) Slug() string { return Slugify(p.Name) }

// Slugify lower-cases s and replaces runs of non-alphanumerics with '-'.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// ParseEnv converts KEY=VALUE lines into a map, ignoring blanks and # comments.
func ParseEnv(text string) (map[string]string, error) {
	env := map[string]string{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", i+1)
		}
		env[k] = v
	}
	return env, nil
}

// FormatEnv is the inverse of ParseEnv, with keys sorted.
func FormatEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + env[k] + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
