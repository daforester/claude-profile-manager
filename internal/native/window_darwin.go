//go:build darwin && cgo

package native

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdint.h>
int cpmWindowRect(uintptr_t w, int r[4]);
void cpmMoveWindow(uintptr_t w, int x, int y);
void cpmSetTopmost(uintptr_t w, int on);
void cpmSkipWindowLists(uintptr_t w);
void cpmStartDrag(uintptr_t w);
*/
import "C"

// BorderlessSupported reports whether a window without a title bar can
// still be moved, via StartWindowDrag.
func BorderlessSupported() bool { return true }

// StartWindowDrag moves the window with the mouse until the button is
// released. Call it from a mouse-down handler (on the main thread, via
// RunNative).
func StartWindowDrag(ctx any) error {
	w, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	C.cpmStartDrag(C.uintptr_t(w))
	return nil
}

// WindowRect returns the window's outer rectangle (title bar included) in
// screen points; ok is false while it is hidden or minimised.
func WindowRect(ctx any) (Rect, bool) {
	w, ok := windowHandle(ctx)
	if !ok {
		return Rect{}, false
	}
	var r [4]C.int
	if C.cpmWindowRect(C.uintptr_t(w), &r[0]) == 0 {
		return Rect{}, false
	}
	return Rect{int(r[0]), int(r[1]), int(r[2]), int(r[3])}, true
}

// MoveWindow puts the window's outer top-left corner at x, y.
func MoveWindow(ctx any, x, y int) error {
	w, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	C.cpmMoveWindow(C.uintptr_t(w), C.int(x), C.int(y))
	return nil
}

// TopmostSupported reports whether SetTopmost works on this OS.
func TopmostSupported() bool { return true }

// SetTopmost floats the window above normal windows (or stops doing so).
func SetTopmost(ctx any, on bool) error {
	w, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	v := C.int(0)
	if on {
		v = 1
	}
	C.cpmSetTopmost(C.uintptr_t(w), v)
	return nil
}

// SetSkipTaskbar keeps the window out of the Window and Dock menus and out
// of Cmd-` cycling, the macOS counterparts of taskbar entries.
func SetSkipTaskbar(ctx any) error {
	w, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	C.cpmSkipWindowLists(C.uintptr_t(w))
	return nil
}
