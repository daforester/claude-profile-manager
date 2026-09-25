//go:build !windows

// Package console lets the windowed (-H windowsgui) build print CLI output
// when it is started from a terminal. It is a no-op outside Windows.
package console

// Attach is a no-op outside Windows.
func Attach() {}
