package native

import (
	"errors"

	"fyne.io/fyne/v2/driver"
)

var errUnsupported = errors.New("not supported on this system")

// windowHandle extracts a native handle from a Fyne RunNative context.
func windowHandle(ctx any) (uintptr, bool) {
	var h uintptr
	switch c := ctx.(type) {
	case driver.WindowsWindowContext:
		h = c.HWND
	case *driver.WindowsWindowContext:
		h = c.HWND
	case driver.X11WindowContext:
		h = c.WindowHandle
	case *driver.X11WindowContext:
		h = c.WindowHandle
	}
	return h, h != 0
}
