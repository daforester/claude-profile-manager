//go:build darwin

package launcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
)

// TerminalChoices lists the terminal options offered on this OS.
func TerminalChoices() []string {
	return []string{settings.TerminalAuto, settings.TerminalMacTerminal, settings.TerminalITerm, settings.TerminalCustom}
}

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func openTerminal(s *settings.Settings, p *profile.Profile, script, dir string) error {
	switch s.Terminal {
	case settings.TerminalCustom:
		cmd, err := customTerminalCommand(s.CustomTerminal, script, Title(p), dir)
		if err != nil {
			return err
		}
		return startDetached(cmd)
	case settings.TerminalITerm:
		return exec.Command("open", "-a", "iTerm", script).Run()
	default:
		// .command files are executed by Terminal.app when opened.
		return exec.Command("open", "-a", "Terminal", script).Run()
	}
}

// ErrDesktopNotFound means no Claude Desktop installation was detected.
var ErrDesktopNotFound = errors.New("Claude Desktop not found in /Applications — install it or set its path in Settings")

// FindDesktop returns the Claude Desktop main executable inside Claude.app.
// It is run directly (not via `open`) so that the profile environment and
// --user-data-dir reach it and a separate instance starts.
func FindDesktop(root, configured string) (string, error) {
	candidates := []string{configured}
	if configured == "" {
		home, _ := os.UserHomeDir()
		candidates = []string{"/Applications/Claude.app", filepath.Join(home, "Applications", "Claude.app")}
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.HasSuffix(strings.TrimSuffix(c, "/"), ".app") {
			if exe := appExecutable(c); exe != "" {
				return exe, nil
			}
			continue
		}
		if fileExists(c) {
			return c, nil
		}
	}
	if configured != "" {
		return "", errors.New("configured Claude Desktop path is not a valid app: " + configured)
	}
	return "", ErrDesktopNotFound
}

func appExecutable(app string) string {
	macos := filepath.Join(app, "Contents", "MacOS")
	if p := filepath.Join(macos, "Claude"); fileExists(p) {
		return p
	}
	entries, err := os.ReadDir(macos)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			return filepath.Join(macos, e.Name())
		}
	}
	return ""
}

// PortableSupported reports whether this OS needs/supports a portable copy.
func PortableSupported() bool { return false }

// MSIXVersion is Windows-only.
func MSIXVersion() (string, error) { return "", errors.New("not supported on macOS") }

// MakePortableDesktop is Windows-only.
func MakePortableDesktop(string, func(string)) (string, error) {
	return "", errors.New("not needed on macOS")
}
