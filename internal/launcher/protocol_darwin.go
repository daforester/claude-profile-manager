//go:build darwin && cgo

package launcher

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation
#include <stdlib.h>
#include <CoreServices/CoreServices.h>

static char *cpmCString(CFStringRef s) {
	if (s == NULL) return NULL;
	CFIndex n = CFStringGetMaximumSizeForEncoding(CFStringGetLength(s), kCFStringEncodingUTF8) + 1;
	char *buf = malloc(n);
	if (buf != NULL && !CFStringGetCString(s, buf, n, kCFStringEncodingUTF8)) {
		free(buf);
		buf = NULL;
	}
	return buf;
}

static CFStringRef cpmCFString(const char *s) {
	return CFStringCreateWithCString(NULL, s, kCFStringEncodingUTF8);
}

// cpmBundleID is the running app's bundle identifier, or NULL when the
// binary is not inside an .app bundle.
static char *cpmBundleID(void) {
	CFBundleRef b = CFBundleGetMainBundle();
	return b ? cpmCString(CFBundleGetIdentifier(b)) : NULL;
}

static char *cpmBundlePath(void) {
	CFBundleRef b = CFBundleGetMainBundle();
	if (b == NULL) return NULL;
	CFURLRef u = CFBundleCopyBundleURL(b);
	if (u == NULL) return NULL;
	CFStringRef p = CFURLCopyFileSystemPath(u, kCFURLPOSIXPathStyle);
	char *r = cpmCString(p);
	if (p) CFRelease(p);
	CFRelease(u);
	return r;
}

// cpmDeclaresScheme reports whether Info.plist lists scheme under
// CFBundleURLTypes; LaunchServices ignores apps that do not.
static int cpmDeclaresScheme(const char *scheme) {
	CFBundleRef b = CFBundleGetMainBundle();
	if (b == NULL) return 0;
	CFTypeRef types = CFBundleGetValueForInfoDictionaryKey(b, CFSTR("CFBundleURLTypes"));
	if (types == NULL || CFGetTypeID(types) != CFArrayGetTypeID()) return 0;
	CFStringRef want = cpmCFString(scheme);
	int found = 0;
	for (CFIndex i = 0; !found && i < CFArrayGetCount(types); i++) {
		CFTypeRef t = CFArrayGetValueAtIndex(types, i);
		if (CFGetTypeID(t) != CFDictionaryGetTypeID()) continue;
		CFTypeRef schemes = CFDictionaryGetValue(t, CFSTR("CFBundleURLSchemes"));
		if (schemes == NULL || CFGetTypeID(schemes) != CFArrayGetTypeID()) continue;
		for (CFIndex j = 0; j < CFArrayGetCount(schemes); j++) {
			CFTypeRef s = CFArrayGetValueAtIndex(schemes, j);
			if (CFGetTypeID(s) == CFStringGetTypeID() &&
				CFStringCompare(s, want, kCFCompareCaseInsensitive) == kCFCompareEqualTo) {
				found = 1;
				break;
			}
		}
	}
	CFRelease(want);
	return found;
}

static char *cpmDefaultHandler(const char *scheme) {
	CFStringRef s = cpmCFString(scheme);
	CFStringRef h = LSCopyDefaultHandlerForURLScheme(s);
	CFRelease(s);
	char *r = cpmCString(h);
	if (h) CFRelease(h);
	return r;
}

static int cpmSetDefaultHandler(const char *scheme, const char *bundleID) {
	CFStringRef s = cpmCFString(scheme);
	CFStringRef b = cpmCFString(bundleID);
	OSStatus st = LSSetDefaultHandlerForURLScheme(s, b);
	CFRelease(s);
	CFRelease(b);
	return (int)st;
}

// cpmRegisterSelf makes sure LaunchServices knows this bundle (and its URL
// types), e.g. right after it was copied to /Applications.
static void cpmRegisterSelf(void) {
	CFBundleRef b = CFBundleGetMainBundle();
	if (b == NULL) return;
	CFURLRef u = CFBundleCopyBundleURL(b);
	if (u == NULL) return;
	LSRegisterURL(u, true);
	CFRelease(u);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// On macOS a URL scheme belongs to an app bundle: Info.plist declares it
// (CFBundleURLTypes, added when the .app is packaged) and LaunchServices
// keeps one default handler per scheme, identified by bundle ID. The link
// itself arrives as an Apple Event in the running app (see package
// urlevent), not as a new process as on Windows and Linux.

const scheme = "claude"

func goString(p *C.char) string {
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

func cstr(s string) (*C.char, func()) {
	p := C.CString(s)
	return p, func() { C.free(unsafe.Pointer(p)) }
}

func bundleID() string   { return goString(C.cpmBundleID()) }
func bundlePath() string { return goString(C.cpmBundlePath()) }

func declaresScheme() bool {
	s, free := cstr(scheme)
	defer free()
	return C.cpmDeclaresScheme(s) != 0
}

func defaultHandler() string {
	s, free := cstr(scheme)
	defer free()
	return goString(C.cpmDefaultHandler(s))
}

func setDefaultHandler(id string) error {
	s, freeS := cstr(scheme)
	defer freeS()
	b, freeB := cstr(id)
	defer freeB()
	if st := C.cpmSetDefaultHandler(s, b); st != 0 {
		return fmt.Errorf("LaunchServices refused to set the claude:// handler to %s (OSStatus %d)", id, int(st))
	}
	return nil
}

var registerOnce sync.Once

// ProtocolSupported reports whether claude:// routing is available here:
// only when running from the packaged .app, whose Info.plist declares the
// scheme.
func ProtocolSupported() bool { return bundleID() != "" && declaresScheme() }

// ProtocolStatus reports the current claude:// handler (a bundle ID).
func ProtocolStatus(root string) ProtocolState {
	cur := defaultHandler()
	self := bundleID()
	return ProtocolState{Ours: self != "" && strings.EqualFold(cur, self), Handler: cur}
}

// EnsureProtocol makes Profile Manager the claude:// handler. Claude Desktop
// claims the scheme again whenever it starts, so this is called repeatedly.
func EnsureProtocol(root string) (bool, error) {
	self := bundleID()
	if self == "" || !declaresScheme() {
		return false, errors.New("claude:// routing needs the packaged Claude Profile Manager.app")
	}
	cur := defaultHandler()
	if strings.EqualFold(cur, self) {
		return false, nil
	}
	registerOnce.Do(func() { C.cpmRegisterSelf() })
	if cur != "" {
		_ = writeBackup(root, cur)
	}
	if err := setDefaultHandler(self); err != nil {
		return false, err
	}
	return true, nil
}

// UnregisterProtocol hands claude:// back to the app we replaced.
func UnregisterProtocol(root string) error {
	b := readBackup(root)
	if b == "" || !ProtocolStatus(root).Ours {
		return nil
	}
	return setDefaultHandler(b)
}

// OpenDefaultAppsSettings is Windows-only.
func OpenDefaultAppsSettings() error { return errors.New("not supported on macOS") }

// mainHandlerCommand opens the link with the app that handled claude://
// before us (normally Claude.app), bypassing the default-handler lookup.
func mainHandlerCommand(root, url string) []string {
	if b := readBackup(root); b != "" {
		return []string{"/usr/bin/open", "-b", b, url}
	}
	return nil
}

func platformDiagnostics(string) string {
	id := bundleID()
	if id == "" {
		id = "(none: not running from the .app bundle)"
	}
	return fmt.Sprintf("App bundle:               %s\nBundle ID:                %s\nInfo.plist has claude://: %v\n",
		bundlePath(), id, declaresScheme())
}

// WatchProtocol is Windows-only; other platforms rely on polling.
func WatchProtocol(root string, enabled func() bool) {}
