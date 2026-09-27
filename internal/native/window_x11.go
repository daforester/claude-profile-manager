//go:build (linux || freebsd || netbsd || openbsd) && ((!x11 && !wayland) || x11) && cgo

package native

/*
#cgo LDFLAGS: -lX11
#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>

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

// cpmSetState adds or removes up to two _NET_WM_STATE flags on a mapped
// window by asking the window manager, as EWMH requires.
static void cpmSetState(Display *d, Window w, int add, const char *a, const char *b) {
	XEvent ev = {0};
	ev.xclient.type = ClientMessage;
	ev.xclient.window = w;
	ev.xclient.message_type = XInternAtom(d, "_NET_WM_STATE", False);
	ev.xclient.format = 32;
	ev.xclient.data.l[0] = add ? 1 : 0; // _NET_WM_STATE_ADD / _REMOVE
	ev.xclient.data.l[1] = XInternAtom(d, a, False);
	ev.xclient.data.l[2] = b ? XInternAtom(d, b, False) : 0;
	ev.xclient.data.l[3] = 1; // normal application
	XSendEvent(d, DefaultRootWindow(d), False,
		SubstructureRedirectMask | SubstructureNotifyMask, &ev);
	XFlush(d);
}

// cpmFrame reads _NET_FRAME_EXTENTS: the window manager's decorations
// (title bar, borders) around the window, in pixels. Zero if there are none.
static void cpmFrame(Display *d, Window w, long ext[4]) {
	ext[0] = ext[1] = ext[2] = ext[3] = 0;
	Atom type;
	int format;
	unsigned long n, left;
	unsigned char *data = NULL;
	if (XGetWindowProperty(d, w, XInternAtom(d, "_NET_FRAME_EXTENTS", False), 0, 4, False,
			XA_CARDINAL, &type, &format, &n, &left, &data) == Success && data) {
		if (type == XA_CARDINAL && format == 32 && n == 4) {
			for (int i = 0; i < 4; i++) ext[i] = ((long *)data)[i];
		}
		XFree(data);
	}
}

// cpmWindowRect reports the window's outer rectangle (decorations included)
// if it is on screen.
static int cpmWindowRect(Display *d, Window w, int r[4]) {
	XWindowAttributes a;
	Window child;
	int x, y;
	long ext[4];
	if (!XGetWindowAttributes(d, w, &a) || a.map_state != IsViewable) return 0;
	if (!XTranslateCoordinates(d, w, DefaultRootWindow(d), 0, 0, &x, &y, &child)) return 0;
	cpmFrame(d, w, ext); // left, right, top, bottom
	r[0] = x - ext[0];
	r[1] = y - ext[2];
	r[2] = a.width + ext[0] + ext[1];
	r[3] = a.height + ext[2] + ext[3];
	return 1;
}

// cpmMoveWindow puts the window's outer top-left corner at x, y. GLFW
// windows use static gravity, so XMoveWindow places the window itself;
// step in by the decorations.
static void cpmMoveWindow(Display *d, Window w, int x, int y) {
	long ext[4];
	cpmFrame(d, w, ext);
	XMoveWindow(d, w, x + ext[0], y + ext[2]);
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

// WindowRect returns the window's outer rectangle (title bar and borders
// included) in screen pixels; ok is false while it is hidden.
func WindowRect(ctx any) (r Rect, ok bool) {
	d, w, ok := x11(ctx)
	if !ok {
		return Rect{}, false
	}
	var c [4]C.int
	if C.cpmWindowRect(d, w, &c[0]) == 0 {
		return Rect{}, false
	}
	return Rect{int(c[0]), int(c[1]), int(c[2]), int(c[3])}, true
}

// MoveWindow asks the window manager to put the window's outer top-left
// corner at x, y.
func MoveWindow(ctx any, x, y int) error {
	d, w, ok := x11(ctx)
	if !ok {
		return errUnsupported
	}
	C.cpmMoveWindow(d, w, C.int(x), C.int(y))
	return nil
}

// TopmostSupported reports whether SetTopmost can work: on X11 it can.
func TopmostSupported() bool { return BorderlessSupported() }

// SetTopmost keeps the window above others (or stops doing so). The window
// must be mapped; call it again after showing.
func SetTopmost(ctx any, on bool) error {
	d, w, ok := x11(ctx)
	if !ok {
		return errUnsupported
	}
	above := C.CString("_NET_WM_STATE_ABOVE")
	defer C.free(unsafe.Pointer(above))
	add := C.int(0)
	if on {
		add = 1
	}
	C.cpmSetState(d, w, add, above, nil)
	return nil
}

// SetSkipTaskbar keeps the window out of taskbars, docks and window
// switchers that honour EWMH. The window must be mapped.
func SetSkipTaskbar(ctx any) error {
	d, w, ok := x11(ctx)
	if !ok {
		return errUnsupported
	}
	tb, pg := C.CString("_NET_WM_STATE_SKIP_TASKBAR"), C.CString("_NET_WM_STATE_SKIP_PAGER")
	defer C.free(unsafe.Pointer(tb))
	defer C.free(unsafe.Pointer(pg))
	C.cpmSetState(d, w, 1, tb, pg)
	return nil
}
