//go:build !(cgo && (windows || darwin || ((linux || freebsd || netbsd || openbsd) && ((!x11 && !wayland) || x11))))

package native

// WorkAreas is not available here; windows are placed by the system.
func WorkAreas() []Rect { return nil }
