// Claude Profile Manager is a desktop app for keeping several Claude
// accounts signed in at once. Each profile gets its own Claude Code config
// directory and Claude Desktop data directory, and can be launched from the
// GUI, the system tray or the command line.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"claude-profile-manager/internal/appdir"
	"claude-profile-manager/internal/cli"
	"claude-profile-manager/internal/console"
	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
	"claude-profile-manager/internal/ui"
)

func main() {
	args := os.Args[1:]
	// macOS passes -psn_* when launched from Finder on older systems.
	if len(args) > 0 && strings.HasPrefix(args[0], "-psn_") {
		args = args[1:]
	}
	// The OS invokes us as `handle-url <claude://…>` once we own the scheme.
	if len(args) >= 2 && args[0] == "handle-url" {
		handleURL(args[1])
		return
	}
	if cli.IsCommand(args) {
		console.Attach()
		prog := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
		os.Exit(cli.Main(cli.Env{Prog: prog, Stdout: os.Stdout, Stderr: os.Stderr, GUI: true}, args))
	}

	root, err := appdir.Root()
	if err != nil {
		fatal(err)
	}
	// The windowed build has no console, so send any crash trace to a file.
	if f, err := os.OpenFile(filepath.Join(root, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	}
	store, err := profile.Open(root)
	if err != nil {
		fatal(err)
	}
	st, err := settings.Load(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: settings:", err)
	}
	ui.Run(root, store, st)
}

func fatal(err error) {
	console.Attach()
	fmt.Fprintln(os.Stderr, "claude-profile-manager:", err)
	os.Exit(1)
}

func handleURL(url string) {
	root, err := appdir.Root()
	if err != nil {
		fatal(err)
	}
	store, err := profile.Open(root)
	if err != nil {
		fatal(err)
	}
	st, _ := settings.Load(root)
	ui.HandleURL(root, store, st, url)
}
