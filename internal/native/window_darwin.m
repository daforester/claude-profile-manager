// Window helpers for macOS. Rectangles use GLFW's screen coordinates:
// points, origin at the top-left of the primary screen, y growing down.
// Cocoa's origin is the bottom-left, so y is flipped against the primary
// screen's height.

#import <Cocoa/Cocoa.h>
#include <stdint.h>

static CGFloat cpmPrimaryHeight(void) {
	NSArray<NSScreen *> *screens = [NSScreen screens];
	return screens.count ? [screens[0] frame].size.height : 0;
}

int cpmWindowRect(uintptr_t w, int r[4]) {
	NSWindow *win = (NSWindow *)w;
	if (![win isVisible] || [win isMiniaturized]) {
		return 0;
	}
	NSRect f = [win frame];
	r[0] = (int)f.origin.x;
	r[1] = (int)(cpmPrimaryHeight() - (f.origin.y + f.size.height));
	r[2] = (int)f.size.width;
	r[3] = (int)f.size.height;
	return 1;
}

void cpmMoveWindow(uintptr_t w, int x, int y) {
	[(NSWindow *)w setFrameTopLeftPoint:NSMakePoint(x, cpmPrimaryHeight() - y)];
}

void cpmSetTopmost(uintptr_t w, int on) {
	[(NSWindow *)w setLevel:on ? NSFloatingWindowLevel : NSNormalWindowLevel];
}

// cpmSkipWindowLists keeps the window out of the Window and Dock menus and
// out of Cmd-` window cycling (macOS has no per-window taskbar entries).
void cpmSkipWindowLists(uintptr_t w) {
	NSWindow *win = (NSWindow *)w;
	[win setExcludedFromWindowsMenu:YES];
	[win setCollectionBehavior:[win collectionBehavior] | NSWindowCollectionBehaviorIgnoresCycle];
}

// cpmStartDrag moves the window with the mouse until the button is released,
// like dragging a title bar (but without the system's edge tiling). The
// button-up event is put back so the app still sees the click end.
void cpmStartDrag(uintptr_t w) {
	@autoreleasepool {
		NSWindow *win = (NSWindow *)w;
		if (!([NSEvent pressedMouseButtons] & 1)) {
			return;
		}
		NSPoint start = [NSEvent mouseLocation];
		NSPoint origin = [win frame].origin;
		for (;;) {
			NSEvent *e = [NSApp nextEventMatchingMask:(NSEventMaskLeftMouseDragged | NSEventMaskLeftMouseUp)
			                                untilDate:[NSDate distantFuture]
			                                   inMode:NSEventTrackingRunLoopMode
			                                  dequeue:YES];
			if (e == nil) {
				break;
			}
			if ([e type] == NSEventTypeLeftMouseUp) {
				[NSApp postEvent:e atStart:YES];
				break;
			}
			NSPoint p = [NSEvent mouseLocation];
			[win setFrameOrigin:NSMakePoint(origin.x + p.x - start.x, origin.y + p.y - start.y)];
		}
	}
}
