//go:build !windows && !darwin

package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const routerDesktopFile = "claude-profile-manager-links.desktop"

// ProtocolSupported reports whether claude:// routing is available here.
func ProtocolSupported() bool {
	_, err := exec.LookPath("xdg-mime")
	return err == nil
}

func xdgQuery() string {
	out, err := exec.Command("xdg-mime", "query", "default", "x-scheme-handler/claude").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ProtocolStatus reports the current claude:// handler.
func ProtocolStatus(root string) ProtocolState {
	cur := xdgQuery()
	return ProtocolState{Ours: cur == routerDesktopFile, Handler: cur}
}

func applicationsDir() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	dir = filepath.Join(dir, "applications")
	return dir, os.MkdirAll(dir, 0o755)
}

// EnsureProtocol makes Profile Manager the claude:// handler (see the
// Windows implementation for why this is called repeatedly).
func EnsureProtocol(root string) (bool, error) {
	exe, err := selfExe()
	if err != nil {
		return false, err
	}
	dir, err := applicationsDir()
	if err != nil {
		return false, err
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Claude Profile Manager (link router)
Comment=Routes Claude Desktop sign-in links to the right profile
Exec="%s" handle-url %%u
MimeType=x-scheme-handler/claude;
NoDisplay=true
Terminal=false
`, strings.ReplaceAll(exe, `"`, `\"`))
	path := filepath.Join(dir, routerDesktopFile)
	if old, _ := os.ReadFile(path); string(old) != entry {
		if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
			return false, err
		}
	}
	cur := xdgQuery()
	if cur == routerDesktopFile {
		return false, nil
	}
	if cur != "" {
		_ = writeBackup(root, cur)
	}
	if out, err := exec.Command("xdg-mime", "default", routerDesktopFile, "x-scheme-handler/claude").CombinedOutput(); err != nil {
		return false, fmt.Errorf("xdg-mime: %v: %s", err, out)
	}
	return true, nil
}

// UnregisterProtocol hands claude:// back to the handler we replaced.
func UnregisterProtocol(root string) error {
	if b := readBackup(root); b != "" && xdgQuery() == routerDesktopFile {
		if out, err := exec.Command("xdg-mime", "default", b, "x-scheme-handler/claude").CombinedOutput(); err != nil {
			return fmt.Errorf("xdg-mime: %v: %s", err, out)
		}
	}
	if dir, err := applicationsDir(); err == nil {
		_ = os.Remove(filepath.Join(dir, routerDesktopFile))
	}
	return nil
}

// OpenDefaultAppsSettings is Windows-only.
func OpenDefaultAppsSettings() error { return errors.New("not supported on this OS") }

// On Linux the main install is reached through the detected Claude Desktop.
func mainHandlerCommand(root, url string) []string { return nil }

func platformDiagnostics() string {
	return "xdg-mime default:         " + xdgQuery() + "\n"
}

// WatchProtocol is Windows-only; other platforms rely on polling.
func WatchProtocol(root string, enabled func() bool) {}
