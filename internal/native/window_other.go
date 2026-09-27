//go:build !windows && !(darwin && cgo) && !((linux || freebsd || netbsd || openbsd) && ((!x11 && !wayland) || x11) && cgo)

package native

// BorderlessSupported is false here: without a title bar the window could
// not be moved.
func BorderlessSupported() bool { return false }

// StartWindowDrag is not supported on this platform.
func StartWindowDrag(any) error { return errUnsupported }

// WindowRect is not supported on this platform.
func WindowRect(any) (Rect, bool) { return Rect{}, false }

// MoveWindow is not supported on this platform.
func MoveWindow(any, int, int) error { return errUnsupported }

// TopmostSupported is false here.
func TopmostSupported() bool { return false }

// SetTopmost is not supported on this platform.
func SetTopmost(any, bool) error { return errUnsupported }

// SetSkipTaskbar is not supported on this platform.
func SetSkipTaskbar(any) error { return errUnsupported }
