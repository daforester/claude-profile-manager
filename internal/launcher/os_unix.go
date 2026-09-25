//go:build !windows && !darwin

package launcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
)

// TerminalChoices lists the terminal options offered on this OS.
func TerminalChoices() []string {
	return []string{settings.TerminalAuto, settings.TerminalCustom}
}

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

// linuxTerminals are tried in order; args precede the script path.
var linuxTerminals = []struct {
	bin  string
	args []string
}{
	{"x-terminal-emulator", []string{"-e"}},
	{"gnome-terminal", []string{"--"}},
	{"ptyxis", []string{"--"}},
	{"konsole", []string{"-e"}},
	{"xfce4-terminal", []string{"-x"}},
	{"mate-terminal", []string{"-x"}},
	{"tilix", []string{"-e"}},
	{"kitty", nil},
	{"alacritty", []string{"-e"}},
	{"wezterm", []string{"start", "--"}},
	{"foot", nil},
	{"xterm", []string{"-e"}},
}

func openTerminal(s *settings.Settings, p *profile.Profile, script, dir string) error {
	if s.Terminal == settings.TerminalCustom {
		cmd, err := customTerminalCommand(s.CustomTerminal, script, Title(p), dir)
		if err != nil {
			return err
		}
		return startDetached(cmd)
	}
	for _, t := range linuxTerminals {
		bin, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, append(append([]string{}, t.args...), script)...)
		cmd.Dir = dir
		return startDetached(cmd)
	}
	return errors.New("no terminal emulator found — set a custom terminal command in Settings, e.g. `kitty {script}`")
}

// ErrDesktopNotFound means no Claude Desktop installation was detected.
var ErrDesktopNotFound = errors.New("Claude Desktop not found — Anthropic does not ship an official Linux build; set the path to your community build (e.g. claude-desktop) in Settings")

// FindDesktop locates a Linux Claude Desktop build.
func FindDesktop(root, configured string) (string, error) {
	if configured != "" {
		if fileExists(configured) {
			return configured, nil
		}
		return "", errors.New("configured Claude Desktop path does not exist: " + configured)
	}
	if p, err := exec.LookPath("claude-desktop"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		"/usr/bin/claude-desktop",
		"/usr/local/bin/claude-desktop",
		"/opt/Claude/claude-desktop",
		"/opt/Claude/claude",
		"/usr/lib/claude-desktop/claude-desktop",
		filepath.Join(home, ".local", "bin", "claude-desktop"),
	} {
		if fileExists(c) {
			return c, nil
		}
	}
	return "", ErrDesktopNotFound
}

// PortableSupported reports whether this OS needs/supports a portable copy.
func PortableSupported() bool { return false }

// MSIXVersion is Windows-only.
func MSIXVersion() (string, error) { return "", errors.New("not supported on this OS") }

// MakePortableDesktop is Windows-only.
func MakePortableDesktop(string, func(string)) (string, error) {
	return "", errors.New("not needed on this OS")
}
