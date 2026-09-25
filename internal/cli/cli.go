// Package cli implements the command-line interface shared by the GUI
// binary and the console-only `cpm` binary.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"claude-profile-manager/internal/appdir"
	"claude-profile-manager/internal/launcher"
	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
)

// Version is set at build time with -ldflags "-X claude-profile-manager/internal/cli.Version=v1.2.3".
var Version = "dev"

const usage = `Claude Profile Manager — isolated Claude Code / Claude Desktop logins

Usage:
  %[1]s                                  start the GUI (GUI build only)
  %[1]s list                             list profiles and their accounts
  %[1]s run <profile> [--dir D] [-- args] run Claude Code here, in this terminal
  %[1]s open <profile> [--dir D]          open Claude Code in a new terminal window
  %[1]s desktop <profile>                 start Claude Desktop for the profile
  %[1]s env <profile> [--shell S]         print a command that starts Claude Code
                                          (S = powershell | cmd | posix)
  %[1]s create <name> [--copy-default]    create a profile (optionally copying
                                          settings/agents/commands from ~/.claude)
  %[1]s paths                             show where data and tools live
  %[1]s version

<profile> may be the profile name, its slug or its ID.
Data lives in %[2]s (override with $CPM_HOME).
`

// Env bundles what commands need.
type Env struct {
	Prog   string
	Stdout io.Writer
	Stderr io.Writer
	// GUI is true for the windowed build, where `run` cannot own a console.
	GUI bool
}

// ErrUsage signals a bad invocation; usage has already been printed.
var ErrUsage = errors.New("usage")

// IsCommand reports whether args[0] names a CLI command.
func IsCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "list", "ls", "run", "open", "desktop", "env", "create", "paths", "version", "help", "-h", "--help", "-v", "--version":
		return true
	}
	return false
}

// Main runs a CLI command and returns the process exit code.
func Main(e Env, args []string) int {
	code, err := run(e, args)
	if err != nil && !errors.Is(err, ErrUsage) {
		fmt.Fprintln(e.Stderr, "error:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func run(e Env, args []string) (int, error) {
	root, err := appdir.Root()
	if err != nil {
		return 1, err
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintf(e.Stdout, usage, e.Prog, root)
		return 0, nil
	}
	store, err := profile.Open(root)
	if err != nil {
		return 1, err
	}
	st, err := settings.Load(root)
	if err != nil {
		return 1, fmt.Errorf("reading settings: %w", err)
	}
	l := &launcher.Launcher{Root: root, Settings: st}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "version", "-v", "--version":
		fmt.Fprintln(e.Stdout, "claude-profile-manager", Version)
		return 0, nil

	case "list", "ls":
		tw := tabwriter.NewWriter(e.Stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tSLUG\tACCOUNT\tLAST USED")
		for _, p := range store.List() {
			last := "never"
			if !p.LastUsedAt.IsZero() {
				last = p.LastUsedAt.Format("2006-01-02 15:04")
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.Name, p.Slug(), p.ReadAccount().Summary(), last)
		}
		return 0, tw.Flush()

	case "paths":
		fmt.Fprintln(e.Stdout, "data:          ", root)
		if cli, err := l.CLIPath(); err == nil {
			fmt.Fprintln(e.Stdout, "claude (CLI):  ", cli)
		} else {
			fmt.Fprintln(e.Stdout, "claude (CLI):   not found")
		}
		if d, err := launcher.FindDesktop(root, st.ClaudeDesktopPath); err == nil {
			fmt.Fprintln(e.Stdout, "Claude Desktop:", d)
		} else {
			fmt.Fprintln(e.Stdout, "Claude Desktop:", err)
		}
		return 0, nil

	case "create":
		fs := flag.NewFlagSet("create", flag.ContinueOnError)
		fs.SetOutput(e.Stderr)
		copyDefault := fs.Bool("copy-default", false, "copy settings, agents, commands and skills from ~/.claude")
		name, err := positional(fs, rest)
		if err != nil {
			return 2, err
		}
		p := store.New(name)
		if err := store.Save(p); err != nil {
			return 1, err
		}
		if err := p.EnsureDirs(); err != nil {
			return 1, err
		}
		if *copyDefault {
			copied, err := p.CopyConfigFrom(appdir.DefaultClaudeConfigDir())
			if err != nil {
				return 1, err
			}
			if len(copied) > 0 {
				fmt.Fprintln(e.Stdout, "copied:", strings.Join(copied, ", "))
			}
		}
		fmt.Fprintf(e.Stdout, "created %q (%s)\n", p.Name, p.ConfigDir())
		return 0, nil
	}

	// Remaining commands take a profile.
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	dir := fs.String("dir", "", "working directory (default: current directory for run, profile setting for open)")
	shell := fs.String("shell", launcher.DefaultShell(), "shell syntax for env: powershell, cmd or posix")
	key, err := positional(fs, rest)
	if err != nil {
		return 2, err
	}
	p, err := store.Get(key)
	if err != nil {
		return 1, err
	}
	switch cmd {
	case "run":
		if e.GUI {
			return 1, errors.New("`run` needs a console — use the cpm command-line binary, or `open` for a new window")
		}
		_ = store.Touch(p)
		return l.RunCLI(p, *dir, fs.Args()...)
	case "open":
		_ = store.Touch(p)
		return 0, l.OpenCLI(p, *dir, fs.Args()...)
	case "desktop":
		_ = store.Touch(p)
		return 0, l.OpenDesktop(p)
	case "env":
		cli, err := l.CLIPath()
		if err != nil {
			cli = "claude"
		}
		s, err := launcher.Snippet(p, cli, *shell)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(e.Stdout, s)
		return 0, nil
	}
	fmt.Fprintf(e.Stderr, usage, e.Prog, root)
	return 2, ErrUsage
}

// positional extracts exactly one leading positional argument, allowing
// flags before or after it. Anything after `--` stays in fs.Args().
func positional(fs *flag.FlagSet, args []string) (string, error) {
	var pos string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if pos == "" {
		if fs.NArg() == 0 {
			return "", fmt.Errorf("%s: missing profile name", fs.Name())
		}
		pos = fs.Arg(0)
		rest := fs.Args()[1:]
		if err := fs.Parse(rest); err != nil {
			return "", err
		}
	}
	return pos, nil
}
