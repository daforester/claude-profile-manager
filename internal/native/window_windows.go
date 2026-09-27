//go:build windows

package native

import "unsafe"

var (
	pReleaseCapture  = user32.NewProc("ReleaseCapture")
	pSendMessageW    = user32.NewProc("SendMessageW")
	pGetWindowRect   = user32.NewProc("GetWindowRect")
	pIsWindowVisible = user32.NewProc("IsWindowVisible")
	pIsIconic        = user32.NewProc("IsIconic")
	pShowWindow      = user32.NewProc("ShowWindow")
	pGetWindowLongW  = user32.NewProc("GetWindowLongW")
	pSetWindowLongW  = user32.NewProc("SetWindowLongW")
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

// WindowRect returns the window's outer rectangle in screen pixels; ok is
// false while it is hidden or minimised.
func WindowRect(ctx any) (Rect, bool) {
	hwnd, ok := windowHandle(ctx)
	if !ok {
		return Rect{}, false
	}
	if v, _, _ := pIsWindowVisible.Call(hwnd); v == 0 {
		return Rect{}, false
	}
	if m, _, _ := pIsIconic.Call(hwnd); m != 0 {
		return Rect{}, false
	}
	var r struct{ Left, Top, Right, Bottom int32 }
	if ret, _, _ := pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return Rect{}, false
	}
	return Rect{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}, true
}

// MoveWindow moves the window's outer top-left corner to x, y in screen
// pixels.
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

// SetSkipTaskbar makes the window a tool window, which keeps it off the
// taskbar and out of Alt+Tab. Windows only applies the change when the
// window is shown, so a visible window is hidden and shown again.
func SetSkipTaskbar(ctx any) error {
	hwnd, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	const gwlExStyle = ^uintptr(19) // -20
	const wsExToolWindow, wsExAppWindow = 0x80, 0x40000
	const swHide, swShowNA = 0, 8
	style, _, _ := pGetWindowLongW.Call(hwnd, gwlExStyle)
	want := (style | wsExToolWindow) &^ wsExAppWindow
	if want == style {
		return nil
	}
	visible, _, _ := pIsWindowVisible.Call(hwnd)
	if visible != 0 {
		pShowWindow.Call(hwnd, swHide)
	}
	pSetWindowLongW.Call(hwnd, gwlExStyle, want)
	if visible != 0 {
		pShowWindow.Call(hwnd, swShowNA)
	}
	return nil
}
