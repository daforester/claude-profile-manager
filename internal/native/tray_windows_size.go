//go:build windows && (amd64 || arm64)

package native

import "unsafe"

// Compile-time check that notifyIconData matches NOTIFYICONDATAW (976 bytes).
var (
	_ [976 - unsafe.Sizeof(notifyIconData{})]byte
	_ [unsafe.Sizeof(notifyIconData{}) - 976]byte
)
