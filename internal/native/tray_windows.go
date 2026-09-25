//go:build windows

package native

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW         = user32.NewProc("RegisterClassExW")
	pCreateWindowExW          = user32.NewProc("CreateWindowExW")
	pDefWindowProcW           = user32.NewProc("DefWindowProcW")
	pGetMessageW              = user32.NewProc("GetMessageW")
	pTranslateMessage         = user32.NewProc("TranslateMessage")
	pDispatchMessageW         = user32.NewProc("DispatchMessageW")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pDestroyIcon              = user32.NewProc("DestroyIcon")
	pCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	pAppendMenuW              = user32.NewProc("AppendMenuW")
	pTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	pDestroyMenu              = user32.NewProc("DestroyMenu")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	pGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pShellNotifyIconW         = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmNull        = 0x0000
	wmApp         = 0x8000
	wmRun         = wmApp + 1
	wmTray        = wmApp + 2
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString    = 0x0
	mfSeparator = 0x800

	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	smCxSmIcon = 49
)

type point struct{ X, Y int32 }

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

// notifyIconData mirrors NOTIFYICONDATAW (976 bytes on 64-bit Windows).
type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

// MenuItem is an entry in a tray icon's right-click menu.
type MenuItem struct {
	ID    string
	Label string // empty = separator
}

// TrayIcons manages any number of native tray icons.
type TrayIcons struct {
	onClick func(key string)
	onMenu  func(key, action string)
	menu    []MenuItem

	mu    sync.Mutex
	queue []func()
	hwnd  uintptr
	ready chan struct{}

	// Owned by the tray thread.
	icons          map[string]*trayIcon
	byUID          map[uint32]string
	nextUID        uint32
	taskbarCreated uint32
}

type trayIcon struct {
	uid   uint32
	hicon uintptr
	tip   string
	added bool
}

var (
	activeTray *TrayIcons
	wndProcCB  = syscall.NewCallback(wndProc)
)

// TraySupported reports whether per-profile tray icons are available.
func TraySupported() bool { return true }

// IconSize is the pixel size tray icons should be rendered at.
func IconSize() int {
	n, _, _ := pGetSystemMetrics.Call(smCxSmIcon)
	if n < 16 {
		return 16
	}
	return int(n)
}

// NewTrayIcons starts the tray thread. onClick fires on left-click, onMenu
// when a right-click menu entry is chosen. Callbacks run on the tray thread.
func NewTrayIcons(menu []MenuItem, onClick func(key string), onMenu func(key, action string)) *TrayIcons {
	t := &TrayIcons{
		onClick: onClick, onMenu: onMenu, menu: menu,
		ready: make(chan struct{}), icons: map[string]*trayIcon{}, byUID: map[uint32]string{}, nextUID: 1,
	}
	activeTray = t
	go t.loop()
	<-t.ready
	return t
}

func (t *TrayIcons) loop() {
	runtime.LockOSThread()
	inst, _, _ := pGetModuleHandleW.Call(0)
	cls, _ := syscall.UTF16PtrFromString("ClaudeProfileManagerTrayIcons")
	wc := wndClassEx{WndProc: wndProcCB, Instance: inst, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	title, _ := syscall.UTF16PtrFromString("Claude Profile Manager tray")
	hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	tb, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	tc, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(tb)))
	t.mu.Lock()
	t.hwnd = hwnd
	t.taskbarCreated = uint32(tc)
	t.mu.Unlock()
	close(t.ready)

	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	t := activeTray
	if t != nil {
		switch {
		case msg == wmRun:
			t.mu.Lock()
			q := t.queue
			t.queue = nil
			t.mu.Unlock()
			for _, f := range q {
				f()
			}
			return 0
		case msg == wmTray:
			key := t.byUID[uint32(wParam)]
			switch uint32(lParam & 0xFFFF) {
			case wmLButtonUp:
				if key != "" && t.onClick != nil {
					t.onClick(key)
				}
			case wmRButtonUp, wmContextMenu:
				if key != "" {
					t.showMenu(key)
				}
			}
			return 0
		case t.taskbarCreated != 0 && msg == t.taskbarCreated:
			// Explorer restarted: every icon must be added again.
			for _, ic := range t.icons {
				ic.added = false
				t.apply(ic)
			}
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func (t *TrayIcons) run(f func()) {
	t.mu.Lock()
	t.queue = append(t.queue, f)
	hwnd := t.hwnd
	t.mu.Unlock()
	pPostMessageW.Call(hwnd, wmRun, 0, 0)
}

// Set adds or updates the icon for key with PNG image data and a tooltip.
func (t *TrayIcons) Set(key string, pngData []byte, tip string) {
	if len(pngData) == 0 {
		return
	}
	data := append([]byte(nil), pngData...)
	t.run(func() {
		ic := t.icons[key]
		if ic == nil {
			ic = &trayIcon{uid: t.nextUID}
			t.nextUID++
			t.icons[key] = ic
			t.byUID[ic.uid] = key
		}
		size := IconSize()
		h, _, _ := pCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, uintptr(size), uintptr(size), 0)
		if h != 0 {
			if ic.hicon != 0 {
				pDestroyIcon.Call(ic.hicon)
			}
			ic.hicon = h
		}
		ic.tip = tip
		t.apply(ic)
	})
}

// Remove deletes the icon for key.
func (t *TrayIcons) Remove(key string) {
	t.run(func() {
		ic := t.icons[key]
		if ic == nil {
			return
		}
		nid := t.nid(ic)
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
		if ic.hicon != 0 {
			pDestroyIcon.Call(ic.hicon)
		}
		delete(t.byUID, ic.uid)
		delete(t.icons, key)
	})
}

// Close removes all icons.
func (t *TrayIcons) Close() {
	done := make(chan struct{})
	t.run(func() {
		for k, ic := range t.icons {
			nid := t.nid(ic)
			pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
			delete(t.icons, k)
		}
		close(done)
	})
	<-done
}

func (t *TrayIcons) nid(ic *trayIcon) notifyIconData {
	var n notifyIconData
	n.Size = uint32(unsafe.Sizeof(n))
	n.Wnd = t.hwnd
	n.ID = ic.uid
	n.Flags = nifMessage | nifIcon | nifTip
	n.CallbackMessage = wmTray
	n.Icon = ic.hicon
	tip, _ := syscall.UTF16FromString(ic.tip)
	if len(tip) > len(n.Tip) {
		tip = tip[:len(n.Tip)]
		tip[len(tip)-1] = 0
	}
	copy(n.Tip[:], tip)
	return n
}

func (t *TrayIcons) apply(ic *trayIcon) {
	nid := t.nid(ic)
	op := uintptr(nimModify)
	if !ic.added {
		op = nimAdd
	}
	r, _, _ := pShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&nid)))
	if r != 0 {
		ic.added = true
	} else if ic.added {
		// Modify failed (icon lost?) — try adding again.
		if r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r != 0 {
			ic.added = true
		}
	}
}

func (t *TrayIcons) showMenu(key string) {
	if len(t.menu) == 0 {
		return
	}
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	for i, it := range t.menu {
		if it.Label == "" {
			pAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		label, _ := syscall.UTF16PtrFromString(it.Label)
		pAppendMenuW.Call(menu, mfString, uintptr(i+1), uintptr(unsafe.Pointer(label)))
	}
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Required so the menu closes when clicking elsewhere.
	pSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	pPostMessageW.Call(t.hwnd, wmNull, 0, 0)
	if cmd > 0 && int(cmd) <= len(t.menu) && t.onMenu != nil {
		t.onMenu(key, t.menu[cmd-1].ID)
	}
}

// SetTopmost pins or unpins a native window above all others.
func SetTopmost(ctx any, on bool) error {
	hwnd, ok := windowHandle(ctx)
	if !ok {
		return errUnsupported
	}
	after := ^uintptr(1) // HWND_NOTOPMOST (-2)
	if on {
		after = ^uintptr(0) // HWND_TOPMOST (-1)
	}
	const swpNoSize, swpNoMove, swpNoActivate = 0x1, 0x2, 0x10
	r, _, err := pSetWindowPos.Call(hwnd, after, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate)
	if r == 0 {
		return err
	}
	return nil
}

// TopmostSupported reports whether SetTopmost works on this OS.
func TopmostSupported() bool { return true }
