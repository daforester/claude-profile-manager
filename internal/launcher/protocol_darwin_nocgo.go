//go:build darwin && !cgo

package launcher

import "errors"

// Without cgo (the console-only cpm build) LaunchServices cannot be reached,
// so automatic routing is not available there; the app bundle build has it
// (protocol_darwin.go).

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

func platformDiagnostics(string) string {
	return "Automatic routing is not available on macOS; use Paste sign-in link.\n"
}

// WatchProtocol is Windows-only; other platforms rely on polling.
func WatchProtocol(root string, enabled func() bool) {}
