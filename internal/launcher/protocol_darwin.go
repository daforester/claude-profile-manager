//go:build darwin

package launcher

import "errors"

// On macOS URL schemes are bound to app bundles through LaunchServices and
// delivered as Apple Events rather than arguments, so automatic routing is
// not available; use "Paste sign-in link" instead.

// ProtocolSupported reports whether claude:// routing is available here.
func ProtocolSupported() bool { return false }

// ProtocolStatus is not available on macOS.
func ProtocolStatus(string) ProtocolState { return ProtocolState{} }

// EnsureProtocol is not available on macOS.
func EnsureProtocol(string) (bool, error) { return false, errors.New("not supported on macOS") }

// UnregisterProtocol is not available on macOS.
func UnregisterProtocol(string) error { return nil }

// OpenDefaultAppsSettings is Windows-only.
func OpenDefaultAppsSettings() error { return errors.New("not supported on macOS") }

func mainHandlerCommand(root, url string) []string { return nil }

func platformDiagnostics() string {
	return "Automatic routing is not available on macOS; use Paste sign-in link.\n"
}

// WatchProtocol is Windows-only; other platforms rely on polling.
func WatchProtocol(root string, enabled func() bool) {}
