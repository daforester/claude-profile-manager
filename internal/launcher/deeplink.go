package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"claude-profile-manager/internal/profile"
)

// Claude Desktop signs in through the system browser, which hands the result
// back as a claude:// deep link. The OS has ONE handler for that scheme —
// normally the main Claude install — so a profile instance that started a
// sign-in never receives the callback. Profile Manager therefore registers
// itself as the claude:// handler and forwards each link to the right
// instance by starting Claude Desktop with that profile's --user-data-dir
// and the URL: Electron's single-instance lock hands the arguments to the
// already-running window for that data directory.
//
// Claude Desktop re-registers itself as the handler every time it starts, so
// the registration has to be re-asserted after each launch (EnsureProtocol).

// LoginMarkerTTL is how long after launching a profile's Desktop a claude://
// link is routed to it automatically.
const LoginMarkerTTL = 10 * time.Minute

type desktopLaunch struct {
	ProfileID string    `json:"profileId"`
	At        time.Time `json:"at"`
}

func markerPath(root string) string    { return filepath.Join(root, "desktop-login-target.json") }
func backupPath(root string) string    { return filepath.Join(root, "protocol-backup.txt") }
func routerLogPath(root string) string { return filepath.Join(root, "link-router.log") }

// RecordDesktopLaunch arms automatic routing of the next claude:// link to
// the profile whose Desktop was just started.
func RecordDesktopLaunch(root, profileID string) error {
	data, _ := json.Marshal(desktopLaunch{ProfileID: profileID, At: time.Now()})
	return os.WriteFile(markerPath(root), data, 0o600)
}

// TakeRecentDesktopLaunch returns the armed profile if it was launched within
// LoginMarkerTTL, and disarms it: one launch, one callback.
func TakeRecentDesktopLaunch(root string) (string, bool) {
	data, err := os.ReadFile(markerPath(root))
	if err != nil {
		return "", false
	}
	var m desktopLaunch
	if json.Unmarshal(data, &m) != nil || m.ProfileID == "" {
		return "", false
	}
	if age := time.Since(m.At); age < 0 || age > LoginMarkerTTL {
		return "", false
	}
	_ = os.Remove(markerPath(root))
	return m.ProfileID, true
}

// IsClaudeURL reports whether s is a claude:// deep link.
func IsClaudeURL(s string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "claude://")
}

// DeliverURL hands a claude:// link to Claude Desktop. p selects the profile
// instance; nil means the main (non-profile) Claude Desktop.
func (l *Launcher) DeliverURL(p *profile.Profile, url string) error {
	url = strings.TrimSpace(url)
	if !IsClaudeURL(url) {
		return errors.New("that is not a claude:// link")
	}
	if p == nil {
		LogRoute(l.Root, "main", url)
		return l.deliverToMain(url)
	}
	exe, err := FindDesktop(l.Root, l.Settings.ClaudeDesktopPath)
	if err != nil {
		return err
	}
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	LogRoute(l.Root, p.Name, url)
	cmd := exec.Command(exe, "--user-data-dir="+p.DesktopDir(), url)
	cmd.Env = ProcessEnv(p)
	cmd.Dir = p.StartDir()
	if err := startDetached(cmd); err != nil {
		return fmt.Errorf("starting Claude Desktop (%s): %w", exe, err)
	}
	return nil
}

// deliverToMain reproduces stock behaviour: run whatever handler Claude had
// registered before we took over, or the detected Claude Desktop.
func (l *Launcher) deliverToMain(url string) error {
	if args := mainHandlerCommand(l.Root, url); len(args) > 0 && fileExists(args[0]) {
		return startDetached(exec.Command(args[0], args[1:]...))
	}
	exe, err := FindDesktop(l.Root, l.Settings.ClaudeDesktopPath)
	if err != nil {
		return err
	}
	return startDetached(exec.Command(exe, url))
}

// LogRoute appends a line to link-router.log (query strings are dropped so
// no auth codes are written to disk).
func LogRoute(root, target, url string) {
	if i := strings.IndexAny(url, "?#"); i >= 0 {
		url = url[:i]
	}
	f, err := os.OpenFile(routerLogPath(root), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s -> %s %s\n", time.Now().Format(time.RFC3339), target, url)
}

func readBackup(root string) string {
	b, _ := os.ReadFile(backupPath(root))
	return strings.TrimSpace(string(b))
}

func writeBackup(root, v string) error {
	return os.WriteFile(backupPath(root), []byte(v+"\n"), 0o600)
}

// ProtocolState describes who currently handles claude:// links.
type ProtocolState struct {
	Ours    bool   // Profile Manager is the handler
	Handler string // current handler (command or .desktop name)
	// Warning explains an OS-level override we cannot change ourselves,
	// e.g. a Windows "default app" choice pointing elsewhere.
	Warning string
}

func selfExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

// LinkDiagnostics describes claude:// handling for troubleshooting.
func LinkDiagnostics(root string) string {
	var b strings.Builder
	exe, _ := selfExe()
	st := ProtocolStatus(root)
	fmt.Fprintf(&b, "Profile Manager exe:      %s\n", exe)
	fmt.Fprintf(&b, "Owned by Profile Manager: %v\n", st.Ours)
	fmt.Fprintf(&b, "Registered handler:       %s\n", st.Handler)
	if st.Warning != "" {
		fmt.Fprintf(&b, "WARNING:                  %s\n", st.Warning)
	}
	b.WriteString(platformDiagnostics())
	fmt.Fprintf(&b, "Replaced handler (backup): %s\n", readBackup(root))
	if data, err := os.ReadFile(markerPath(root)); err == nil {
		var m desktopLaunch
		if json.Unmarshal(data, &m) == nil {
			fmt.Fprintf(&b, "Armed for profile:        %s (%s ago)\n", m.ProfileID, time.Since(m.At).Round(time.Second))
		}
	} else {
		b.WriteString("Armed for profile:        (none)\n")
	}
	b.WriteString("\nRecent link-router.log (\"(received)\" = the OS called Profile Manager):\n")
	if data, err := os.ReadFile(routerLogPath(root)); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 20 {
			lines = lines[len(lines)-20:]
		}
		b.WriteString(strings.Join(lines, "\n") + "\n")
	} else {
		b.WriteString("(empty — Profile Manager has never been asked to open a claude:// link)\n")
	}
	return b.String()
}

// RoutingTestURL is opened by the one-time routing setup. Opening it makes
// Windows show its "How do you want to open this?" picker (when no default
// is chosen yet) and, once Profile Manager handles it, confirms routing works.
const RoutingTestURL = "claude://claude-profile-manager/routing-test"

// IsRoutingTest reports whether url is the setup test link.
func IsRoutingTest(url string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(url)), RoutingTestURL)
}

// OpenTestLink asks the OS to open RoutingTestURL, exactly as a browser would.
func OpenTestLink() error { return OpenPath(RoutingTestURL) }
