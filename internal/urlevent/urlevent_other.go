//go:build !darwin || !cgo

// Package urlevent receives the URLs macOS asks this app to open. Other
// systems start a new process with the URL as an argument instead.
package urlevent

// Events returns nil: URL events are macOS-only.
func Events() <-chan string { return nil }
