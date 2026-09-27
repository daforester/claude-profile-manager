//go:build (linux || freebsd || netbsd || openbsd) && ((!x11 && !wayland) || x11) && cgo

package native

/*
#cgo LDFLAGS: -lX11
#include <X11/Xlib.h>

// cpmStartMove hands a window move to the window manager
// (_NET_WM_MOVERESIZE), as a title-bar drag would.
static void cpmStartMove(Display *d, Window w) {
	Window root, child;
	int rx, ry, wx, wy;
	unsigned int mask;
	if (!XQueryPointer(d, w, &root, &child, &rx, &ry, &wx, &wy, &mask)) {
		return;
	}
	// The window manager can only take the pointer once we let go of it.
	XUngrabPointer(d, CurrentTime);
	XEvent ev = {0};
	ev.xclient.type = ClientMessage;
	ev.xclient.window = w;
	ev.xclient.message_type = XInternAtom(d, "_NET_WM_MOVERESIZE", False);
	ev.xclient.format = 32;
	ev.xclient.data.l[0] = rx;
	ev.xclient.data.l[1] = ry;
	ev.xclient.data.l[2] = 8; // _NET_WM_MOVERESIZE_MOVE
	ev.xclient.data.l[3] = Button1;
	ev.xclient.data.l[4] = 1; // normal application
	XSendEvent(d, root, False, SubstructureRedirectMask | SubstructureNotifyMask, &ev);
	XFlush(d);
}

static int cpmWindowPos(Display *d, Window w, int *x, int *y) {
	Window child;
	return XTranslateCoordinates(d, w, DefaultRootWindow(d), 0, 0, x, y, &child);
}

static void cpmMoveWindow(Display *d, Window w, int x, int y) {
	XMoveWindow(d, w, x, y);
	XFlush(d);
}
*/
import "C"

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// BorderlessSupported reports whether a window without a title bar can
// still be moved, via StartWindowDrag. That needs X11 (including XWayland);
// native Wayland only lets the toolkit start a move, and GLFW doesn't.
func BorderlessSupported() (ok bool) {
	defer func() {
		if recover() != nil { // GLFW not initialised
			ok = false
		}
	}()
	return glfw.GetPlatform() == glfw.PlatformX11
}

// x11 returns GLFW's own X connection (the one holding the pointer grab
// during a click) and the window from a RunNative context.
func x11(ctx any) (*C.Display, C.Window, bool) {
	id, ok := windowHandle(ctx)
	if !ok || !BorderlessSupported() {
		return nil, 0, false
	}
	d := (*C.Display)(unsafe.Pointer(glfw.GetX11Display()))
	return d, C.Window(id), d != nil
}

// StartWindowDrag lets the user move the window with the mouse button that
// is currently held down, as if they had grabbed its title bar. Call it
// from a mouse-down handler (on the main thread, via RunNative).
func StartWindowDrag(ctx any) error {
	d, w, ok := x11(ctx)
	if !ok {
		return errUnsupported
	}
	C.cpmStartMove(d, w)
	return nil
}

// WindowPos returns the top-left corner of the window's content in screen
// pixels.
func WindowPos(ctx any) (x, y int, ok bool) {
	d, w, ok := x11(ctx)
	if !ok {
		return 0, 0, false
	}
	var cx, cy C.int
	if C.cpmWindowPos(d, w, &cx, &cy) == 0 {
		return 0, 0, false
	}
	return int(cx), int(cy), true
}

// MoveWindow asks the window manager to move the window to x, y.
func MoveWindow(ctx any, x, y int) error {
	d, w, ok := x11(ctx)
	if !ok {
		return errUnsupported
	}
	C.cpmMoveWindow(d, w, C.int(x), C.int(y))
	return nil
}
