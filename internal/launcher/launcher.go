package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
	"claude-profile-manager/internal/shellwords"
)

// Launcher starts Claude for profiles using the app settings.
type Launcher struct {
	Root     string // manager data root
	Settings *settings.Settings
}

// CLIPath resolves the Claude Code executable.
func (l *Launcher) CLIPath() (string, error) { return FindCLI(l.Settings.ClaudeCLIPath) }

// OpenCLI opens a new terminal window running Claude Code for p in dir
// (empty = the profile's working directory).
func (l *Launcher) OpenCLI(p *profile.Profile, dir string, extra ...string) error {
	cli, err := l.CLIPath()
	if err != nil {
		return err
	}
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	if dir == "" {
		dir = p.StartDir()
	}
	script, err := WriteScript(p, cli, dir, extra)
	if err != nil {
		return err
	}
	return openTerminal(l.Settings, p, script, dir)
}

// RunCLI runs Claude Code for p in the current terminal (stdio attached)
// and returns its exit code. Used by the `run` subcommand.
func (l *Launcher) RunCLI(p *profile.Profile, dir string, extra ...string) (int, error) {
	cli, err := l.CLIPath()
	if err != nil {
		return 1, err
	}
	if err := p.EnsureDirs(); err != nil {
		return 1, err
	}
	args, err := p.Args()
	if err != nil {
		return 1, err
	}
	cmd := exec.Command(cli, append(args, extra...)...)
	cmd.Env = ProcessEnv(p)
	cmd.Dir = dir
	if cmd.Dir == "" {
		if wd, err := os.Getwd(); err == nil {
			cmd.Dir = wd
		}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// OpenDesktop starts a Claude Desktop instance bound to p's data directory.
func (l *Launcher) OpenDesktop(p *profile.Profile) error {
	exe, err := FindDesktop(l.Root, l.Settings.ClaudeDesktopPath)
	if err != nil {
		return err
	}
	if err := p.EnsureDirs(); err != nil {
		return err
	}
	cmd := exec.Command(exe, "--user-data-dir="+p.DesktopDir())
	cmd.Env = ProcessEnv(p)
	cmd.Dir = p.StartDir()
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting Claude Desktop (%s): %w", exe, err)
	}
	// Arm claude:// routing so this instance receives its sign-in callback.
	_ = RecordDesktopLaunch(l.Root, p.ID)
	return cmd.Process.Release()
}

// customTerminalCommand expands a user template such as
// `kitty --title {title} {script}`.
func customTerminalCommand(tmpl, script, title, dir string) (*exec.Cmd, error) {
	parts, err := shellwords.Split(tmpl)
	if err != nil {
		return nil, fmt.Errorf("custom terminal command: %w", err)
	}
	if len(parts) == 0 {
		return nil, errors.New("custom terminal command is empty — set it in Settings")
	}
	hasScript := false
	for i, s := range parts {
		r := replacePlaceholders(s, script, title)
		if r != s && containsScript(s) {
			hasScript = true
		}
		parts[i] = r
	}
	if !hasScript {
		parts = append(parts, script)
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = dir
	return cmd, nil
}

func containsScript(s string) bool { return strings.Contains(s, "{script}") }

func replacePlaceholders(s, script, title string) string {
	return strings.NewReplacer("{script}", script, "{title}", title).Replace(s)
}

func startDetached(cmd *exec.Cmd) error {
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
