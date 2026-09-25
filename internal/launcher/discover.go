package launcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ErrCLINotFound means the claude executable could not be located.
var ErrCLINotFound = errors.New("Claude Code CLI (`claude`) not found — install it or set its path in Settings")

// FindCLI locates the Claude Code executable. configured wins when set.
func FindCLI(configured string) (string, error) {
	if configured != "" {
		if fileExists(configured) {
			return configured, nil
		}
		return "", errors.New("configured Claude Code path does not exist: " + configured)
	}
	if p, err := exec.LookPath("claude"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs, nil
		}
		return p, nil
	}
	for _, c := range cliCandidates() {
		if fileExists(c) {
			return c, nil
		}
	}
	return "", ErrCLINotFound
}

// cliCandidates covers install locations that are often missing from PATH,
// especially for GUI apps on macOS which do not inherit the shell PATH.
func cliCandidates() []string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		local := os.Getenv("LOCALAPPDATA")
		return []string{
			filepath.Join(home, ".local", "bin", "claude.exe"),
			filepath.Join(home, ".claude", "local", "claude.exe"),
			filepath.Join(appData, "npm", "claude.cmd"),
			filepath.Join(local, "Programs", "claude", "claude.exe"),
			filepath.Join(home, "scoop", "shims", "claude.exe"),
		}
	}
	return []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
		"/usr/bin/claude",
		filepath.Join(home, ".npm-global", "bin", "claude"),
		filepath.Join(home, ".bun", "bin", "claude"),
		filepath.Join(home, ".volta", "bin", "claude"),
	}
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// newestVersionDir picks the highest-versioned entry matching
// <parent>/<prefix><version> (e.g. app-0.14.3).
func newestVersionDir(parent, prefix string) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Slice(names, func(i, j int) bool {
		return compareVersions(strings.TrimPrefix(names[i], prefix), strings.TrimPrefix(names[j], prefix)) < 0
	})
	return filepath.Join(parent, names[len(names)-1])
}

// compareVersions compares dotted numeric versions; non-numeric parts
// compare lexically.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		xn, xok := atoi(x)
		yn, yok := atoi(y)
		switch {
		case xok && yok && xn != yn:
			if xn < yn {
				return -1
			}
			return 1
		case !(xok && yok) && x != y:
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// PortableDesktopDir is where MakePortableDesktop puts its copy (Windows).
func PortableDesktopDir(root string) string { return filepath.Join(root, "desktop-portable") }
