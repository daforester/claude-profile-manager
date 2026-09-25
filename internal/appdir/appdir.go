// Package appdir resolves where Claude Profile Manager keeps its own data.
package appdir

import (
	"os"
	"path/filepath"
)

// EnvOverride lets users relocate all manager data (profiles, settings,
// isolated Claude directories), e.g. onto another drive.
const EnvOverride = "CPM_HOME"

// Root returns the directory that holds profiles.json, settings.json and the
// per-profile data directories. It is created if missing.
func Root() (string, error) {
	dir := os.Getenv(EnvOverride)
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			home, herr := os.UserHomeDir()
			if herr != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
		dir = filepath.Join(base, "ClaudeProfileManager")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// DefaultClaudeConfigDir is the directory Claude Code uses when
// CLAUDE_CONFIG_DIR is not set (~/.claude).
func DefaultClaudeConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}
