//go:build windows

// Package console lets the windowed (-H windowsgui) build print CLI output
// when it is started from a terminal.
package console

import (
	"os"
	"syscall"
)

const attachParentProcess = ^uint32(0) // (DWORD)-1

// Attach connects stdout/stderr to the parent console, if there is one.
func Attach() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := proc.Call(uintptr(attachParentProcess)); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
}
