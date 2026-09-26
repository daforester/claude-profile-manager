//go:build !windows && !darwin

package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// isolateXDG points every XDG lookup at a temp dir so tests never touch the
// real desktop configuration.
func isolateXDG(t *testing.T) (data, config string) {
	t.Helper()
	d := t.TempDir()
	data, config = filepath.Join(d, "data"), filepath.Join(d, "config")
	t.Setenv("HOME", d)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(d, "sys"))
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	t.Setenv("KDE_FULL_SESSION", "")
	t.Setenv("GNOME_DESKTOP_SESSION_ID", "")
	t.Setenv("DESKTOP_SESSION", "")
	t.Setenv("XDG_SESSION_DESKTOP", "")
	t.Setenv("APPIMAGE", "")
	return data, config
}

func TestExecQuotingRoundTrip(t *testing.T) {
	for _, exe := range []string{
		"/usr/local/bin/claude-profile-manager",
		"/opt/Claude Profile Manager/claude-profile-manager",
		`/home/a"b/c$d/e` + "`f" + `/g\h/100%/cpm`,
	} {
		entry := routerDesktopEntry(exe)
		path := filepath.Join(t.TempDir(), "x.desktop")
		if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
			t.Fatal(err)
		}
		v, err := desktopExec(path)
		if err != nil {
			t.Fatal(err)
		}
		args, err := expandExec(v, "claude://login?code=1")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{exe, "handle-url", "claude://login?code=1"}
		if !reflect.DeepEqual(args, want) {
			t.Errorf("round trip of %q:\n got %q\nwant %q\nentry:\n%s", exe, args, want, entry)
		}
		if dfv, err := exec.LookPath("desktop-file-validate"); err == nil {
			if out, err := exec.Command(dfv, path).CombinedOutput(); err != nil {
				t.Errorf("desktop-file-validate rejected entry for %q: %s", exe, out)
			}
		}
	}
}

func TestExpandExec(t *testing.T) {
	url := "claude://x"
	cases := []struct {
		in   string
		want []string
	}{
		{"claude-desktop %u", []string{"claude-desktop", url}},
		{"claude-desktop", []string{"claude-desktop", url}},
		{`"/opt/My App/claude" --flag %U`, []string{"/opt/My App/claude", "--flag", url}},
		{"app %i %c %k --pct=100%% %u", []string{"app", "--pct=100%", url}},
		{"/usr/bin/flatpak run --branch=stable --command=claude --file-forwarding com.anthropic.Claude @@u %u @@",
			[]string{"/usr/bin/flatpak", "run", "--branch=stable", "--command=claude", "--file-forwarding", "com.anthropic.Claude", url}},
	}
	for _, c := range cases {
		got, err := expandExec(c.in, url)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("expandExec(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	if _, err := expandExec(`"unterminated %u`, url); err == nil {
		t.Error("expected an error for an unterminated quote")
	}
}

func TestMimeappsEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mimeapps.list")
	const mime = "x-scheme-handler/claude"
	if err := setMimeappsDefault(path, mime, "a.desktop"); err != nil {
		t.Fatal(err)
	}
	if got := mimeappsDefault(path, mime); got != "a.desktop" {
		t.Fatalf("got %q", got)
	}
	orig := "[Added Associations]\ntext/plain=gedit.desktop;\n\n[Default Applications]\ntext/html=firefox.desktop;\nx-scheme-handler/claude=claude-desktop.desktop;\n\n[Removed Associations]\n"
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setMimeappsDefault(path, mime, "b.desktop"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := mimeappsDefault(path, mime); got != "b.desktop" {
		t.Fatalf("got %q in\n%s", got, data)
	}
	if !strings.Contains(string(data), "text/html=firefox.desktop;") || !strings.Contains(string(data), "text/plain=gedit.desktop;") {
		t.Errorf("other entries lost:\n%s", data)
	}
	if strings.Count(string(data), mime) != 1 {
		t.Errorf("duplicate entry:\n%s", data)
	}
	if err := removeMimeappsDefault(path, mime, "b.desktop"); err != nil {
		t.Fatal(err)
	}
	if got := mimeappsDefault(path, mime); got != "" {
		t.Errorf("still set to %q", got)
	}
}

func TestFindDesktopFileAndMainHandler(t *testing.T) {
	data, _ := isolateXDG(t)
	root := t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude-desktop")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(data, "applications", "vendor")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "[Desktop Entry]\nType=Application\nName=Claude\nExec=" + quoteExecArg(bin) + " --no-sandbox %u\n\n[Desktop Action new]\nExec=wrong\n"
	if err := os.WriteFile(filepath.Join(sub, "claude.desktop"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeBackup(root, "vendor-claude.desktop"); err != nil {
		t.Fatal(err)
	}
	got := mainHandlerCommand(root, "claude://y")
	want := []string{bin, "--no-sandbox", "claude://y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mainHandlerCommand = %q, want %q", got, want)
	}
	if err := writeBackup(root, routerDesktopFile); err != nil {
		t.Fatal(err)
	}
	if got := mainHandlerCommand(root, "claude://y"); got != nil {
		t.Errorf("our own entry must never be the main handler, got %q", got)
	}
}

// TestProtocolLifecycle registers, re-claims and unregisters the handler
// through whichever backend this machine has (xdg-mime or mimeapps.list).
func TestProtocolLifecycle(t *testing.T) {
	data, config := isolateXDG(t)
	root := t.TempDir()
	apps := filepath.Join(data, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	// A pre-existing Claude Desktop registration, as its installer leaves it.
	if err := os.WriteFile(filepath.Join(apps, "claude-desktop.desktop"),
		[]byte("[Desktop Entry]\nType=Application\nName=Claude\nExec=/bin/true %u\nMimeType=x-scheme-handler/claude;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := setHandler("claude-desktop.desktop"); err != nil {
		t.Fatal(err)
	}
	if got := currentHandler(); got != "claude-desktop.desktop" {
		t.Fatalf("setup: handler = %q", got)
	}

	changed, err := EnsureProtocol(root)
	if err != nil || !changed {
		t.Fatalf("EnsureProtocol = %v, %v", changed, err)
	}
	if st := ProtocolStatus(root); !st.Ours || st.Warning != "" {
		t.Fatalf("status after register: %+v", st)
	}
	if got := mimeappsDefault(filepath.Join(config, "mimeapps.list"), schemeMime); got != routerDesktopFile {
		t.Errorf("mimeapps.list default = %q", got)
	}
	if b := readBackup(root); b != "claude-desktop.desktop" {
		t.Errorf("backup = %q", b)
	}
	if args := mainHandlerCommand(root, "claude://z"); !reflect.DeepEqual(args, []string{"/bin/true", "claude://z"}) {
		t.Errorf("main handler = %q", args)
	}
	if changed, err := EnsureProtocol(root); err != nil || changed {
		t.Errorf("second EnsureProtocol should be a no-op, got %v, %v", changed, err)
	}

	// Claude Desktop takes the scheme back on start-up; we re-claim it.
	if err := setHandler("claude-desktop.desktop"); err != nil {
		t.Fatal(err)
	}
	if changed, err := EnsureProtocol(root); err != nil || !changed || !ProtocolStatus(root).Ours {
		t.Fatalf("re-claim: %v, %v, %+v", changed, err, ProtocolStatus(root))
	}
	if b := readBackup(root); b != "claude-desktop.desktop" {
		t.Errorf("backup after re-claim = %q", b)
	}

	if err := UnregisterProtocol(root); err != nil {
		t.Fatal(err)
	}
	if got := currentHandler(); got != "claude-desktop.desktop" {
		t.Errorf("after unregister handler = %q", got)
	}
	if fileExists(filepath.Join(apps, routerDesktopFile)) {
		t.Error("router .desktop file left behind")
	}
}

func TestProtocolLifecycleWithoutXdgMime(t *testing.T) {
	if _, err := exec.LookPath("xdg-mime"); err != nil {
		t.Skip("already covered by TestProtocolLifecycle")
	}
	t.Setenv("PATH", t.TempDir())
	TestProtocolLifecycle(t)
}
