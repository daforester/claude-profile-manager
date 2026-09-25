package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
)

func newProfile(t *testing.T) *profile.Profile {
	t.Helper()
	s, err := profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := s.New("Test Profile")
	p.Env = map[string]string{"FOO": "bar baz", "ANTHROPIC_API_KEY": "profile-key"}
	p.ExtraArgs = `--model opus --append-system-prompt "be brief"`
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func TestEnviron(t *testing.T) {
	p := newProfile(t)
	base := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/old", "ANTHROPIC_AUTH_TOKEN=leak", "CLAUDE_CODE_OAUTH_TOKEN=leak", "ANTHROPIC_API_KEY=parent"}
	m := envMap(Environ(p, base))
	if m["CLAUDE_CONFIG_DIR"] != p.ConfigDir() {
		t.Errorf("CLAUDE_CONFIG_DIR = %q", m["CLAUDE_CONFIG_DIR"])
	}
	if _, ok := m["ANTHROPIC_AUTH_TOKEN"]; ok {
		t.Error("inherited auth token must be cleared")
	}
	if _, ok := m["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Error("inherited OAuth token must be cleared")
	}
	if m["ANTHROPIC_API_KEY"] != "profile-key" {
		t.Error("a key set on the profile itself must win")
	}
	if m["FOO"] != "bar baz" || m["PATH"] != "/bin" {
		t.Errorf("unexpected env %v", m)
	}
	if _, ok := m["HOME"]; ok {
		t.Error("HOME should not be set unless IsolateHome")
	}

	p.KeepInheritedAuth = true
	p.Env = nil
	m = envMap(Environ(p, base))
	if m["ANTHROPIC_AUTH_TOKEN"] != "leak" {
		t.Error("KeepInheritedAuth should keep inherited tokens")
	}

	p.IsolateHome = true
	m = envMap(Environ(p, base))
	if m["HOME"] != p.HomeDir() {
		t.Error("IsolateHome should set HOME")
	}
}

// TestPOSIXScript runs the generated script with a fake claude that dumps
// its environment and arguments.
func TestPOSIXScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test")
	}
	p := newProfile(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	fake := filepath.Join(dir, "fake claude")
	body := "#!/bin/sh\n{ pwd -P; echo \"CFG=$CLAUDE_CONFIG_DIR\"; echo \"FOO=$FOO\"; echo \"TOKEN=${ANTHROPIC_AUTH_TOKEN-unset}\"; for a in \"$@\"; do echo \"ARG=$a\"; done; } > " + out + "\n"
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work dir")
	_ = os.MkdirAll(work, 0o755)
	script, err := WriteScript(p, fake, work, []string{"--resume"})
	if err != nil {
		t.Fatal(err)
	}
	// Replace the trailing interactive shell so the test terminates.
	// true is /bin/true on Linux but /usr/bin/true on macOS.
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "SHELL="+trueBin, "ANTHROPIC_AUTH_TOKEN=leak")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v\n%s", err, b)
	}
	got, _ := os.ReadFile(out)
	want := strings.Join([]string{
		mustEval(t, work),
		"CFG=" + p.ConfigDir(),
		"FOO=bar baz",
		"TOKEN=unset",
		"ARG=--model", "ARG=opus", "ARG=--append-system-prompt", "ARG=be brief", "ARG=--resume",
	}, "\n") + "\n"
	if string(got) != want {
		t.Errorf("script output:\n%s\nwant:\n%s", got, want)
	}
}

func mustEval(t *testing.T, p string) string {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCmdScript(t *testing.T) {
	p := newProfile(t)
	s := cmdScript(p, `C:\Program Files\claude\claude.exe`, `C:\work 100%`, []string{"--model", "opus"})
	for _, want := range []string{
		"set \"CLAUDE_CONFIG_DIR=" + p.ConfigDir() + "\"\r\n",
		"set \"ANTHROPIC_AUTH_TOKEN=\"\r\n",
		`cd /d "C:\work 100%%"`,
		`call "C:\Program Files\claude\claude.exe" --model opus %*`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("cmd script missing %q:\n%s", want, s)
		}
	}
}

func TestSnippets(t *testing.T) {
	p := newProfile(t)
	ps, _ := Snippet(p, `C:\c\claude.exe`, ShellPowerShell)
	if !strings.Contains(ps, "$env:CLAUDE_CONFIG_DIR = '"+p.ConfigDir()+"'") || !strings.Contains(ps, "Remove-Item Env:ANTHROPIC_AUTH_TOKEN") {
		t.Errorf("powershell snippet: %s", ps)
	}
	sh, _ := Snippet(p, "/usr/bin/claude", ShellPOSIX)
	if !strings.HasPrefix(sh, "env -u ANTHROPIC_AUTH_TOKEN -u CLAUDE_CODE_OAUTH_TOKEN ") || !strings.Contains(sh, "'be brief'") {
		t.Errorf("posix snippet: %s", sh)
	}
}

func TestCustomTerminal(t *testing.T) {
	cmd, err := customTerminalCommand(`kitty --title {title} -e {script}`, "/tmp/x y.sh", "Claude · A", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kitty", "--title", "Claude · A", "-e", "/tmp/x y.sh"}
	if strings.Join(cmd.Args, "|") != strings.Join(want, "|") {
		t.Errorf("args = %q", cmd.Args)
	}
	cmd, _ = customTerminalCommand(`alacritty -e`, "/s.sh", "t", "/")
	if cmd.Args[len(cmd.Args)-1] != "/s.sh" {
		t.Errorf("script should be appended when {script} is absent: %q", cmd.Args)
	}
}

func TestCompareVersions(t *testing.T) {
	if compareVersions("0.9.10", "0.10.0") >= 0 || compareVersions("1.2", "1.2.0") > 0 || compareVersions("2.0.0", "1.99") <= 0 {
		t.Error("version ordering is wrong")
	}
	dir := t.TempDir()
	for _, n := range []string{"app-0.9.3", "app-0.10.1", "app-0.10.0", "packages"} {
		_ = os.MkdirAll(filepath.Join(dir, n), 0o755)
	}
	if got := filepath.Base(newestVersionDir(dir, "app-")); got != "app-0.10.1" {
		t.Errorf("newest = %s", got)
	}
}

func TestDesktopLaunchMarker(t *testing.T) {
	root := t.TempDir()
	if _, ok := TakeRecentDesktopLaunch(root); ok {
		t.Fatal("no marker yet")
	}
	_ = RecordDesktopLaunch(root, "abc")
	if id, ok := TakeRecentDesktopLaunch(root); !ok || id != "abc" {
		t.Fatalf("got %q %v", id, ok)
	}
	if _, ok := TakeRecentDesktopLaunch(root); ok {
		t.Error("marker must be single-use")
	}
}

func TestDeliverURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake Claude Desktop")
	}
	p := newProfile(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "args.txt")
	fake := filepath.Join(dir, "claude-desktop")
	_ = os.WriteFile(fake, []byte("#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done > "+out+"\n"), 0o755)
	l := &Launcher{Root: t.TempDir(), Settings: &settings.Settings{ClaudeDesktopPath: fake}}
	if err := l.DeliverURL(p, "https://example.com"); err == nil {
		t.Error("non-claude URLs must be rejected")
	}
	url := "claude://login/google-auth?code=secret"
	if err := l.DeliverURL(p, url); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for i := 0; i < 50 && len(got) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		got, _ = os.ReadFile(out)
	}
	want := "--user-data-dir=" + p.DesktopDir() + "\n" + url + "\n"
	if string(got) != want {
		t.Errorf("fake desktop got:\n%s\nwant:\n%s", got, want)
	}
	log, _ := os.ReadFile(filepath.Join(l.Root, "link-router.log"))
	if strings.Contains(string(log), "secret") || !strings.Contains(string(log), "Test Profile") {
		t.Errorf("router log should name the profile and omit the query: %s", log)
	}
}
