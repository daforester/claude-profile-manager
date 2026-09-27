//go:build !windows && !((linux || freebsd || netbsd || openbsd) && ((!x11 && !wayland) || x11) && cgo)

package native

// BorderlessSupported is false here: without a title bar the window could
// not be moved.
func BorderlessSupported() bool { return false }

// StartWindowDrag is not supported on this platform.
func StartWindowDrag(any) error { return errUnsupported }

// WindowPos is not supported on this platform.
func WindowPos(any) (x, y int, ok bool) { return 0, 0, false }

// MoveWindow is not supported on this platform.
func MoveWindow(any, int, int) error { return errUnsupported }
