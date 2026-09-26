//go:build darwin && cgo

// Package urlevent receives the URLs macOS asks this app to open. macOS
// delivers a claude:// link to the handler app as an Apple Event (starting
// the app first if needed) rather than as a command-line argument.
package urlevent

/*
#cgo LDFLAGS: -framework Cocoa
void cpmInstallURLHandler(void);
*/
import "C"

var events = make(chan string, 16)

// The handler must be in place before the event loop starts, or the link
// that launched the app is lost. Package init runs on the main thread,
// before Fyne initialises Cocoa.
func init() { C.cpmInstallURLHandler() }

//export cpmURLOpened
func cpmURLOpened(u *C.char) {
	select {
	case events <- C.GoString(u):
	default: // nobody is reading; drop rather than block the main thread
	}
}

// Events returns the URLs the OS asks the app to open.
func Events() <-chan string { return events }
