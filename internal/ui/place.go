package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"

	"claude-profile-manager/internal/native"
	"claude-profile-manager/internal/settings"
)

// Window placement: each tracked window reopens where it was last shown,
// moved (and if need be shrunk) to stay on a screen that still exists. This
// needs native window positions, so it only works where the native package
// provides them (Windows and X11); elsewhere the system places windows.

// placeInterval is how often visible windows' positions are recorded.
// Fyne reports no move events, and the tray can hide the main window
// without telling us, so positions are sampled rather than caught on close.
const placeInterval = 2 * time.Second

// placed is a window whose position is remembered.
type placed struct {
	key  string
	win  func() fyne.Window // current window, or nil
	size bool               // remember its size as well
	// ready is set once the window has been restored; until then it sits
	// wherever the system put it, which must not be recorded.
	ready bool
}

// trackPlace remembers where the window returned by win is shown, under key.
func (g *gui) trackPlace(key string, win func() fyne.Window, size bool) {
	g.places = append(g.places, &placed{key: key, win: win, size: size})
}

func (g *gui) placeOf(key string) *placed {
	for _, pl := range g.places {
		if pl.key == key {
			return pl
		}
	}
	return nil
}

// savedSize is the remembered size of a window that tracks its size.
func (g *gui) savedSize(key string) (fyne.Size, bool) {
	p, ok := g.settings.Windows[key]
	if !ok || p.W <= 0 || p.H <= 0 {
		return fyne.Size{}, false
	}
	return fyne.NewSize(p.W, p.H), true
}

// restorePlace moves a window that has just been shown to where it was last
// seen, keeping it inside a monitor's work area; a window too big for the
// area is shrunk. With nothing saved it is only kept on screen. Call it on
// the main thread after Show; it waits for the window to be mapped.
func (g *gui) restorePlace(key string) {
	pl := g.placeOf(key)
	if pl == nil {
		return
	}
	pl.ready = false
	saved, have := g.settings.Windows[key]
	tries := 0
	var try func()
	try = func() {
		w := pl.win()
		nw, ok := w.(driver.NativeWindow)
		if !ok {
			return
		}
		var r native.Rect
		var mapped bool
		nw.RunNative(func(ctx any) { r, mapped = native.WindowRect(ctx) })
		if !mapped {
			// Showing is asynchronous. Give up after a few seconds: the
			// window was hidden again, or positions aren't available here.
			if tries++; tries < 30 {
				time.AfterFunc(100*time.Millisecond, func() { fyne.Do(try) })
			}
			return
		}
		want := r
		if have {
			want.X, want.Y = saved.X, saved.Y
		}
		fit := native.Fit(want, native.WorkAreas())
		if fit.W < r.W || fit.H < r.H {
			cs := w.Canvas().Size()
			w.Resize(fyne.NewSize(cs.Width*float32(fit.W)/float32(r.W), cs.Height*float32(fit.H)/float32(r.H)))
		}
		if fit.X != r.X || fit.Y != r.Y {
			nw.RunNative(func(ctx any) { _ = native.MoveWindow(ctx, fit.X, fit.Y) })
		}
		pl.ready = true
	}
	try()
}

// recordPlaces saves where each visible, restored window is. Main thread.
func (g *gui) recordPlaces() {
	changed := false
	for _, pl := range g.places {
		if !pl.ready {
			continue
		}
		w := pl.win()
		nw, ok := w.(driver.NativeWindow)
		if !ok {
			continue
		}
		var r native.Rect
		var visible bool
		nw.RunNative(func(ctx any) { r, visible = native.WindowRect(ctx) })
		if !visible {
			continue
		}
		p := settings.WindowPlace{X: r.X, Y: r.Y}
		if pl.size {
			s := w.Canvas().Size()
			p.W, p.H = s.Width, s.Height
		}
		if old, ok := g.settings.Windows[pl.key]; ok && old == p {
			continue
		}
		if g.settings.Windows == nil {
			g.settings.Windows = map[string]settings.WindowPlace{}
		}
		g.settings.Windows[pl.key] = p
		changed = true
	}
	if changed {
		_ = g.settings.Save(g.root)
	}
}

// recordPlacesPeriodically samples window positions in the background.
func (g *gui) recordPlacesPeriodically() {
	go func() {
		for range time.Tick(placeInterval) {
			fyne.Do(g.recordPlaces)
		}
	}()
}
