package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/systray"
)

// quitTimeout is how long Fyne gets to shut down before the process exits
// anyway.
const quitTimeout = 3 * time.Second

// quit exits the app. Fyne's shutdown can deadlock: a lifecycle event that
// queues work for the main loop after the loop has drained its queue waits
// forever, so Run never returns and the process lingers with its tray icons.
// Tray icons are therefore removed up front, and a watchdog exits the process
// (logging where it was stuck) if Fyne hasn't finished in time.
func (g *gui) quit() {
	if g.trayIcons != nil {
		g.trayIcons.Close()
	}
	if runtime.GOOS == "windows" {
		// Fyne only removes its tray icon when a window has focus, and
		// Windows keeps a dead process's icon until the mouse passes over it.
		systray.Quit()
	}
	go func() {
		time.Sleep(quitTimeout)
		g.logQuitHang()
		os.Exit(0)
	}()
	g.app.Quit()
}

// logQuitHang records every goroutine's stack so a stuck shutdown can be
// diagnosed.
func (g *gui) logQuitHang() {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	msg := time.Now().Format(time.RFC3339) + ": shutdown did not finish within " + quitTimeout.String() + "; goroutines:\n\n"
	_ = os.WriteFile(filepath.Join(g.root, "quit-hang.log"), append([]byte(msg), buf[:n]...), 0o600)
}
