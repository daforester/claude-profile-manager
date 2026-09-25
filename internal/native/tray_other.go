//go:build !windows

package native

import (
	"fmt"
	"os/exec"
	"runtime"
)

// MenuItem is an entry in a tray icon's right-click menu.
type MenuItem struct {
	ID    string
	Label string
}

// TrayIcons is a no-op outside Windows: the systray library Fyne uses can
// only show one icon, so per-profile icons are Windows-only. Usage is shown
// in the main tray menu instead.
type TrayIcons struct{}

// TraySupported reports whether per-profile tray icons are available.
func TraySupported() bool { return false }

// IconSize is the pixel size tray icons should be rendered at.
func IconSize() int { return 32 }

// NewTrayIcons returns a no-op manager.
func NewTrayIcons([]MenuItem, func(string), func(string, string)) *TrayIcons { return &TrayIcons{} }

// Set is a no-op.
func (*TrayIcons) Set(string, []byte, string) {}

// Remove is a no-op.
func (*TrayIcons) Remove(string) {}

// Close is a no-op.
func (*TrayIcons) Close() {}

// SetTopmost pins a window above others. On X11 it uses wmctrl.
func SetTopmost(ctx any, on bool) error {
	if runtime.GOOS == "darwin" {
		return errUnsupported
	}
	id, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	path, err := exec.LookPath("wmctrl")
	if err != nil {
		return fmt.Errorf("install wmctrl to pin windows on top")
	}
	op := "remove"
	if on {
		op = "add"
	}
	return exec.Command(path, "-i", "-r", fmt.Sprintf("0x%x", id), "-b", op+",above").Run()
}

// TopmostSupported reports whether SetTopmost can work on this OS.
func TopmostSupported() bool { return runtime.GOOS != "darwin" }
