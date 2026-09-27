//go:build !windows

package native

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
