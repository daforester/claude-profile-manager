//go:build windows

package native

import "unsafe"

var (
	pReleaseCapture = user32.NewProc("ReleaseCapture")
	pSendMessageW   = user32.NewProc("SendMessageW")
	pGetWindowRect  = user32.NewProc("GetWindowRect")
)

// BorderlessSupported reports whether a window without a title bar can
// still be moved, via StartWindowDrag.
func BorderlessSupported() bool { return true }

// StartWindowDrag lets the user move the window with the mouse button that
// is currently held down, as if they had grabbed its title bar. Call it
// from a mouse-down handler (on the main thread, via RunNative).
func StartWindowDrag(ctx any) error {
	hwnd, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	const wmNCLButtonDown, htCaption = 0x00A1, 2
	pReleaseCapture.Call()
	// Runs Windows' move loop and returns when the button is released.
	pSendMessageW.Call(hwnd, wmNCLButtonDown, htCaption, 0)
	return nil
}

// WindowPos returns the window's top-left corner in screen pixels.
func WindowPos(ctx any) (x, y int, ok bool) {
	hwnd, ok := windowHandle(ctx)
	if !ok {
		return 0, 0, false
	}
	var r struct{ Left, Top, Right, Bottom int32 }
	if ret, _, _ := pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return 0, 0, false
	}
	return int(r.Left), int(r.Top), true
}

// MoveWindow moves the window's top-left corner to x, y in screen pixels.
func MoveWindow(ctx any, x, y int) error {
	hwnd, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	const swpNoSize, swpNoZOrder, swpNoActivate = 0x1, 0x4, 0x10
	r, _, err := pSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	if r == 0 {
		return err
	}
	return nil
}
