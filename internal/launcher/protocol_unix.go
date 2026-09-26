//go:build !windows && !darwin

package launcher

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// On Linux (and the BSDs) URL schemes follow the freedesktop.org specs: an
// application's .desktop file declares MimeType=x-scheme-handler/claude, and
// the default handler is recorded in mimeapps.list. Browsers open claude://
// links through xdg-open (Chromium) or GIO (Firefox), which both read that
// file. Profile Manager writes its own .desktop entry and makes it the
// default; Claude Desktop builds take it back on start-up (Electron runs
// xdg-settings), which the GUI's polling re-claims.

const (
	routerDesktopFile = "claude-profile-manager-links.desktop"
	schemeMime        = "x-scheme-handler/claude"
)

// ProtocolSupported reports whether claude:// routing is available here.
// Without xdg-mime the default is written to mimeapps.list directly.
func ProtocolSupported() bool { return true }

func hasXdgMime() bool {
	_, err := exec.LookPath("xdg-mime")
	return err == nil
}

// currentHandler returns the desktop-file ID that opens claude:// links.
func currentHandler() string {
	if hasXdgMime() {
		out, err := exec.Command("xdg-mime", "query", "default", schemeMime).Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return mimeappsDefault(mimeappsPath(), schemeMime)
}

// setHandler makes the desktop-file ID id the default for claude:// links.
func setHandler(id string) error {
	if hasXdgMime() {
		out, err := exec.Command("xdg-mime", "default", id, schemeMime).CombinedOutput()
		if err == nil {
			return nil
		}
		// xdg-mime can fail on minimal desktops (e.g. no gio/kde tools);
		// fall through to writing the file it would have written.
		if werr := setMimeappsDefault(mimeappsPath(), schemeMime, id); werr != nil {
			return fmt.Errorf("xdg-mime: %v: %s", err, out)
		}
		return nil
	}
	return setMimeappsDefault(mimeappsPath(), schemeMime, id)
}

// ProtocolStatus reports the current claude:// handler.
func ProtocolStatus(root string) ProtocolState {
	cur := currentHandler()
	st := ProtocolState{Ours: cur == routerDesktopFile, Handler: cur}
	if st.Ours {
		if dir, err := applicationsDir(); err == nil && !fileExists(filepath.Join(dir, routerDesktopFile)) {
			st.Ours = false
			st.Warning = routerDesktopFile + " is the default but the file is missing from " + dir
		}
	}
	return st
}

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

func applicationsDir() (string, error) {
	dir := filepath.Join(dataHome(), "applications")
	return dir, os.MkdirAll(dir, 0o755)
}

func mimeappsPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "mimeapps.list")
}

// routerExe is the program the .desktop entry runs. An AppImage runs from a
// temporary mount that disappears when it exits, so point at the AppImage
// file itself.
func routerExe() (string, error) {
	if a := os.Getenv("APPIMAGE"); a != "" && fileExists(a) {
		return a, nil
	}
	return selfExe()
}

// execReserved are the characters the Desktop Entry spec says must be quoted
// in an Exec argument.
const execReserved = " \t\n\"'\\><~|&;$*?#()`"

// quoteExecArg quotes one argument for a .desktop Exec key. A plain path is
// left bare: xdg-utils' fallback (no GNOME/KDE, as on many minimal or tiling
// desktops) splits Exec on spaces and checks the first word exists, so a
// quoted path makes xdg-open skip our handler. Otherwise the spec quotes with
// double quotes, escapes " ` $ \ inside them, and then the whole value is a
// string in which \ is escaped once more. % is doubled either way.
func quoteExecArg(s string) string {
	if s != "" && !strings.ContainsAny(s, execReserved) {
		return strings.ReplaceAll(s, "%", "%%")
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '`', '$':
			b.WriteString(`\\`)
			b.WriteRune(r)
		case '\\':
			b.WriteString(`\\\\`)
		case '%':
			b.WriteString("%%")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func routerDesktopEntry(exe string) string {
	return `[Desktop Entry]
Type=Application
Name=Claude Profile Manager (link router)
Comment=Routes Claude Desktop sign-in links to the right profile
Exec=` + quoteExecArg(exe) + ` handle-url %u
MimeType=` + schemeMime + `;
NoDisplay=true
Terminal=false
`
}

// EnsureProtocol makes Profile Manager the claude:// handler (see the
// Windows implementation for why this is called repeatedly). The handler it
// replaces is remembered so it can be restored and used for the main install.
func EnsureProtocol(root string) (bool, error) {
	exe, err := routerExe()
	if err != nil {
		return false, err
	}
	dir, err := applicationsDir()
	if err != nil {
		return false, err
	}
	entry := routerDesktopEntry(exe)
	path := filepath.Join(dir, routerDesktopFile)
	wrote := false
	if old, _ := os.ReadFile(path); string(old) != entry {
		if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
			return false, err
		}
		wrote = true
	}
	if wrote {
		// GIO (Firefox, GNOME) consults mimeinfo.cache for the handlers an
		// entry claims; refresh it where the tool exists.
		if udd, err := exec.LookPath("update-desktop-database"); err == nil {
			_ = exec.Command(udd, dir).Run()
		}
	}
	cur := currentHandler()
	if cur == routerDesktopFile {
		return wrote, nil
	}
	if cur != "" {
		_ = writeBackup(root, cur)
	}
	if err := setHandler(routerDesktopFile); err != nil {
		return false, err
	}
	return true, nil
}

// UnregisterProtocol hands claude:// back to the handler we replaced.
func UnregisterProtocol(root string) error {
	var err error
	if currentHandler() == routerDesktopFile {
		if b := readBackup(root); b != "" && b != routerDesktopFile {
			err = setHandler(b)
		} else {
			err = removeMimeappsDefault(mimeappsPath(), schemeMime, routerDesktopFile)
		}
	}
	if dir, derr := applicationsDir(); derr == nil {
		_ = os.Remove(filepath.Join(dir, routerDesktopFile))
		if udd, lerr := exec.LookPath("update-desktop-database"); lerr == nil {
			_ = exec.Command(udd, dir).Run()
		}
	}
	return err
}

// OpenDefaultAppsSettings is Windows-only.
func OpenDefaultAppsSettings() error { return errors.New("not supported on this OS") }

// mainHandlerCommand builds the command line of the .desktop handler we
// replaced (e.g. a community Claude Desktop build, native or Flatpak), so
// "Main Claude Desktop" behaves exactly as if we had never taken over.
func mainHandlerCommand(root, url string) []string {
	id := readBackup(root)
	if id == "" || id == routerDesktopFile {
		return nil
	}
	path := findDesktopFile(id)
	if path == "" {
		return nil
	}
	exec_, err := desktopExec(path)
	if err != nil || exec_ == "" {
		return nil
	}
	args, err := expandExec(exec_, url)
	if err != nil || len(args) == 0 {
		return nil
	}
	if !filepath.IsAbs(args[0]) {
		p, err := exec.LookPath(args[0])
		if err != nil {
			return nil
		}
		args[0] = p
	}
	return args
}

// dataDirs lists the XDG application data directories, most important first.
func dataDirs() []string {
	dirs := []string{dataHome()}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(sys, ":") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// findDesktopFile resolves a desktop-file ID to a path. IDs map "-" to "/"
// for entries installed in subdirectories (vendor-app.desktop may live at
// applications/vendor/app.desktop).
func findDesktopFile(id string) string {
	if strings.ContainsAny(id, "/\\") || !strings.HasSuffix(id, ".desktop") {
		return ""
	}
	for _, d := range dataDirs() {
		base := filepath.Join(d, "applications")
		if p := filepath.Join(base, id); fileExists(p) {
			return p
		}
		parts := strings.Split(id, "-")
		for i := 1; i < len(parts); i++ {
			p := filepath.Join(base, filepath.Join(parts[:i]...), strings.Join(parts[i:], "-"))
			if fileExists(p) {
				return p
			}
		}
	}
	return ""
}

// desktopExec reads the Exec key of a .desktop file's [Desktop Entry] group.
func desktopExec(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	group := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			group = line
			continue
		}
		if group != "[Desktop Entry]" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == "Exec" {
			return unescapeDesktopString(strings.TrimSpace(v)), nil
		}
	}
	return "", sc.Err()
}

// unescapeDesktopString undoes the string-level escapes of a .desktop value.
func unescapeDesktopString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// expandExec splits an (already string-unescaped) Exec value into arguments
// and substitutes url for the URL/file field codes, as a launcher would.
func expandExec(s, url string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg, quoted, sawURL := false, false, false
	flush := func() {
		if inArg {
			args = append(args, cur.String())
		}
		cur.Reset()
		inArg = false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted:
			if c == '"' {
				quoted = false
			} else if c == '\\' && i+1 < len(s) && strings.IndexByte("\"`$\\", s[i+1]) >= 0 {
				i++
				cur.WriteByte(s[i])
			} else if c == '%' && i+1 < len(s) && s[i+1] == '%' {
				i++
				cur.WriteByte('%')
			} else {
				cur.WriteByte(c)
			}
		case c == '"':
			quoted, inArg = true, true
		case c == ' ' || c == '\t':
			flush()
		case c == '%' && i+1 < len(s):
			i++
			switch s[i] {
			case '%':
				cur.WriteByte('%')
				inArg = true
			case 'u', 'U', 'f', 'F':
				cur.WriteString(url)
				inArg, sawURL = true, true
			default:
				// %i %c %k and deprecated codes expand to nothing here.
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote in Exec")
	}
	flush()
	// Flatpak exports wrap file arguments in @@u … @@ markers for
	// --file-forwarding; they are only meaningful to flatpak itself and a
	// URL passes through unchanged, so drop them.
	out := args[:0]
	for _, a := range args {
		if a != "@@u" && a != "@@" && a != "@@f" {
			out = append(out, a)
		}
	}
	if !sawURL && len(out) > 0 {
		out = append(out, url)
	}
	return out, nil
}

// mimeappsDefault reads the [Default Applications] entry for mime.
func mimeappsDefault(path, mime string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	group := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			group = line
			continue
		}
		if group != "[Default Applications]" {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == mime {
			first, _, _ := strings.Cut(v, ";")
			return strings.TrimSpace(first)
		}
	}
	return ""
}

// setMimeappsDefault sets (id != "") or removes (id == "") the
// [Default Applications] entry for mime, keeping everything else.
func setMimeappsDefault(path, mime, id string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	var out []string
	group, inDefaults, found, done := "", false, false, false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			if inDefaults && !done && id != "" {
				out = append(out, mime+"="+id+";")
				done = true
			}
			group = t
			inDefaults = group == "[Default Applications]"
			found = found || inDefaults
			out = append(out, line)
			continue
		}
		if inDefaults {
			if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == mime {
				if id != "" && !done {
					out = append(out, mime+"="+id+";")
					done = true
				}
				continue
			}
		}
		out = append(out, line)
	}
	if id != "" && !done {
		if !found {
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, "[Default Applications]")
		} else {
			// The group was last in the file; drop trailing blanks before
			// appending so the entry stays inside it.
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
		}
		out = append(out, mime+"="+id+";")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

// removeMimeappsDefault drops the default for mime if it is still id.
func removeMimeappsDefault(path, mime, id string) error {
	if mimeappsDefault(path, mime) != id {
		return nil
	}
	return setMimeappsDefault(path, mime, "")
}

func platformDiagnostics(root string) string {
	var b strings.Builder
	tool := "xdg-mime"
	if !hasXdgMime() {
		tool = "mimeapps.list (xdg-mime not installed)"
	}
	fmt.Fprintf(&b, "Default via %s: %s\n", tool, currentHandler())
	if dir, err := applicationsDir(); err == nil {
		p := filepath.Join(dir, routerDesktopFile)
		if data, err := os.ReadFile(p); err == nil {
			for _, l := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(l, "Exec=") {
					fmt.Fprintf(&b, "Router entry:             %s (%s)\n", p, l)
				}
			}
		} else {
			fmt.Fprintf(&b, "Router entry:             %s (missing)\n", p)
		}
	}
	if args := mainHandlerCommand(root, "claude://…"); len(args) > 0 {
		fmt.Fprintf(&b, "Main install runs:        %s\n", strings.Join(args, " "))
	}
	return b.String()
}

// WatchProtocol is Windows-only; other platforms rely on polling.
func WatchProtocol(root string, enabled func() bool) {}
