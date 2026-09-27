package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/native"
	"claude-profile-manager/internal/usage"
)

// popout is a small always-available window listing usage per profile.
// In compact mode it drops the toolbar and shows one line per profile, and
// where the window can still be dragged without one, drops the title bar.
type popout struct {
	g       *gui
	win     fyne.Window
	rows    *fyne.Container
	pin     *widget.Check
	header  fyne.CanvasObject
	side    *fyne.Container // compact mode's expand and close icons (see arrangeSide)
	expand  fyne.CanvasObject
	closeX  fyne.CanvasObject // nil when the window keeps its title bar
	visible bool
	// borderless records whether win was created without a title bar.
	borderless bool
	// resize forces the next refresh to size the window to its content,
	// shrinking it if needed (set when switching modes).
	resize bool
}

func (g *gui) togglePopout() {
	if g.pop != nil && g.pop.visible {
		g.pop.close()
		return
	}
	g.showPopout()
}

func (g *gui) showPopout() {
	if g.pop == nil {
		g.pop = g.newPopout()
	}
	g.pop.refresh()
	g.pop.win.Show()
	g.pop.visible = true
	g.pop.applyPin()
	if !g.settings.PopoutOpen {
		g.settings.PopoutOpen = true
		_ = g.settings.Save(g.root)
	}
}

func (g *gui) newPopout() *popout {
	p := &popout{g: g}
	p.rows = container.NewVBox()

	p.pin = widget.NewCheck("Pin on top", func(on bool) {
		g.settings.PopoutPinned = on
		_ = g.settings.Save(g.root)
		p.applyPin()
	})
	p.pin.SetChecked(g.settings.PopoutPinned)
	if !native.TopmostSupported() {
		p.pin.Hide()
	}
	choose := widget.NewButtonWithIcon("", theme.ListIcon(), p.chooseProfiles)
	choose.Importance = widget.LowImportance
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() { g.monitor.Refresh("") })
	refresh.Importance = widget.LowImportance
	compact := widget.NewButtonWithIcon("", theme.ViewRestoreIcon(), p.toggleCompact)
	compact.Importance = widget.LowImportance
	p.header = container.NewHBox(p.pin, layout.NewSpacer(), refresh, choose, compact)

	p.expand = newTinyButton(theme.ViewFullScreenIcon(), p.toggleCompact)
	if native.BorderlessSupported() {
		p.closeX = newTinyButton(theme.CancelIcon(), p.close)
	}
	p.side = container.NewHBox()

	p.newWindow()
	return p
}

// wantBorderless reports whether the current mode should have no title bar.
func (p *popout) wantBorderless() bool {
	return p.g.settings.PopoutCompact && native.BorderlessSupported()
}

// newWindow creates the pop-out's window for the current mode. Fyne can
// only drop the title bar when a window is created, so switching to or
// from a borderless window replaces it.
func (p *popout) newWindow() {
	p.borderless = false
	if drv, ok := p.g.app.Driver().(desktop.Driver); ok && p.wantBorderless() {
		p.win = drv.CreateSplashWindow()
		p.borderless = true
		// A window that can't be resized is also one the window manager
		// won't tile or maximize when it is dragged to a screen edge. Our
		// own Resize calls still work.
		p.win.SetFixedSize(true)
	} else {
		p.win = p.g.app.NewWindow("Claude usage")
	}
	p.win.SetTitle("Claude usage")
	p.win.SetIcon(fyne.NewStaticResource("icon.png", iconPNG))
	p.win.SetPadded(false) // content brings its own padding
	p.win.SetCloseIntercept(func() { p.close() })
	p.setContent()
	p.resize = true
}

// setContent lays the window out for the current mode.
func (p *popout) setContent() {
	var body fyne.CanvasObject
	if p.g.settings.PopoutCompact {
		var rows fyne.CanvasObject = container.NewVScroll(p.rows)
		if p.borderless {
			rows = newDragArea(rows, p.startDrag)
		}
		body = container.NewBorder(nil, nil, nil, p.side, rows)
	} else {
		body = container.NewBorder(p.header, nil, nil, nil, vscroll(p.rows))
	}
	p.win.SetContent(container.NewPadded(body))
}

func (p *popout) toggleCompact() {
	s := p.g.settings
	s.PopoutCompact = !s.PopoutCompact
	_ = s.Save(p.g.root)
	if p.wantBorderless() == p.borderless {
		p.setContent()
		p.resize = true
		p.refresh()
		return
	}

	// Replace the window, keeping its place on screen where we can.
	old := p.win
	x, y, havePos := 0, 0, false
	if nw, ok := old.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) { x, y, havePos = native.WindowPos(ctx) })
	}
	old.SetContent(canvas.NewRectangle(color.Transparent)) // release rows and header
	p.newWindow()
	p.refresh()
	p.win.Show()
	p.applyPin()
	old.Close()
	if nw, ok := p.win.(driver.NativeWindow); ok && havePos {
		// The new window is mapped asynchronously; move it once it is.
		go func() {
			time.Sleep(150 * time.Millisecond)
			fyne.Do(func() { nw.RunNative(func(ctx any) { _ = native.MoveWindow(ctx, x, y) }) })
		}()
	}
}

// startDrag moves the borderless window with the mouse, as its title bar
// would.
func (p *popout) startDrag() {
	if nw, ok := p.win.(driver.NativeWindow); ok {
		nw.RunNative(func(ctx any) { _ = native.StartWindowDrag(ctx) })
	}
}

func (p *popout) close() {
	p.win.Hide()
	p.visible = false
	p.g.settings.PopoutOpen = false
	_ = p.g.settings.Save(p.g.root)
}

// applyPin sets or clears always-on-top on the native window.
func (p *popout) applyPin() {
	nw, ok := p.win.(driver.NativeWindow)
	if !ok || !native.TopmostSupported() {
		return
	}
	on := p.g.settings.PopoutPinned
	apply := func() {
		nw.RunNative(func(ctx any) {
			// Errors are expected before the window is mapped; the
			// delayed call below covers that.
			_ = native.SetTopmost(ctx, on)
		})
	}
	apply()
	// Showing a window is asynchronous; apply again once it is mapped.
	go func() {
		time.Sleep(400 * time.Millisecond)
		fyne.Do(apply)
	}()
}

// refresh rebuilds the rows. Safe to call on a nil popout.
func (p *popout) refresh() {
	if p == nil {
		return
	}
	g := p.g
	compact := g.settings.PopoutCompact
	p.rows.RemoveAll()
	if compact {
		p.rows.Layout = layout.NewCustomPaddedVBoxLayout(2)
	} else {
		p.rows.Layout = layout.NewVBoxLayout()
	}
	shown := 0
	for _, prof := range g.profiles {
		if !g.settings.PopoutShows(prof.ID) {
			continue
		}
		shown++
		u := g.usageOf(prof.ID)
		if compact {
			p.rows.Add(compactRow(prof.Color, u))
			continue
		}
		dot := canvas.NewCircle(parseHex(prof.Color))
		name := widget.NewLabelWithStyle(prof.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		name.Truncation = fyne.TextTruncateEllipsis
		bar := newUsageBar(10)
		pct := widget.NewLabelWithStyle("—", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true, Monospace: true})
		detail := widget.NewLabel("")
		detail.SizeName = theme.SizeNameCaptionText
		detail.Truncation = fyne.TextTruncateEllipsis
		switch {
		case u.Session != nil:
			bar.Set(u.Session.Utilization, true)
			bar.SetStale(u.Stale())
			pct.SetText(fmt.Sprintf("%.0f%%", u.Session.Utilization))
			d := "5-hour"
			if u.Stale() {
				d = "As of " + ago(u.FetchedAt) + " · " + staleReason(u)
			} else if !u.Session.ResetsAt.IsZero() {
				d += " · resets " + resetText(u.Session.ResetsAt)
			}
			if u.Weekly != nil {
				d += fmt.Sprintf(" · week %.0f%%", u.Weekly.Utilization)
			}
			detail.SetText(d)
		case u.NoLogin:
			detail.SetText("No Claude Code login")
		case u.Err != "":
			detail.SetText(u.Err)
		default:
			detail.SetText("Loading…")
		}
		top := container.NewBorder(nil, nil,
			container.NewCenter(container.NewGridWrap(fyne.NewSize(10, 10), dot)),
			container.NewGridWrap(fyne.NewSize(52, 30), pct), name)
		p.rows.Add(container.New(layout.NewCustomPaddedVBoxLayout(-6), top, container.NewPadded(bar), detail))
	}
	if shown == 0 {
		if compact {
			p.rows.Add(widget.NewLabel("No profiles selected."))
		} else {
			p.rows.Add(widget.NewLabel("No profiles selected — use the list button above."))
		}
	}
	p.rows.Refresh()

	// Fit the rows exactly: the scroller's own minimum height is tiny, so
	// add up the parts around it (see setContent).
	pad := theme.Padding()
	var w, h float32
	if compact {
		p.arrangeSide(shown)
		w = 160
		h = max(p.rows.MinSize().Height, p.side.MinSize().Height)
	} else {
		w = max(p.win.Canvas().Size().Width, 320)
		h = p.header.MinSize().Height + pad + p.rows.MinSize().Height
	}
	h = min(h+2*pad, 640)
	cur := p.win.Canvas().Size()
	switch {
	case p.resize || !p.visible:
		p.win.Resize(fyne.NewSize(w, h))
	case cur.Height < h-1:
		p.win.Resize(fyne.NewSize(max(cur.Width, w), h))
	}
	p.resize = false
}

// compactRow is one line: a split bar in the profile's colour (5-hour window
// above, weekly limit below) and the 5-hour percentage, coloured by level
// so a near-full limit still stands out.
// Stale, missing or failed readings show a dimmed value; the full view
// has the details.
func compactRow(colour string, u usage.Usage) fyne.CanvasObject {
	size := theme.CaptionTextSize() + 1
	dim := theme.Color(theme.ColorNameDisabled)

	// A split track: 5-hour window on top, weekly limit underneath.
	session, weekly := newUsageBar(4), newUsageBar(4)
	for _, b := range []*usageBar{session, weekly} {
		b.SetFill(parseHex(colour))
		b.SetStale(u.Stale())
	}
	pct := canvas.NewText("—", dim)
	pct.TextSize = size
	pct.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	pct.Alignment = fyne.TextAlignTrailing
	if u.Session != nil {
		session.Set(u.Session.Utilization, true)
		pct.Text = fmt.Sprintf("%.0f%%", u.Session.Utilization)
		if !u.Stale() {
			pct.Color = native.LevelColor(u.Session.Utilization)
		}
	}
	if u.Weekly != nil {
		weekly.Set(u.Weekly.Utilization, true)
	}
	bars := container.NewVBox(layout.NewSpacer(),
		container.New(layout.NewCustomPaddedVBoxLayout(1), session, weekly),
		layout.NewSpacer())

	rowH := pct.MinSize().Height
	// Fixed width so bars line up whatever the percentage.
	pctW := fyne.MeasureText("100%", size, pct.TextStyle).Width + 4
	right := container.NewGridWrap(fyne.NewSize(pctW, rowH), pct)
	return container.NewBorder(nil, nil, nil, right, bars)
}

// tinyButton is a bare icon that runs fn when tapped, for places where a
// full button's padding would cost too much space.
type tinyButton struct {
	widget.BaseWidget
	res fyne.Resource
	fn  func()
}

func newTinyButton(res fyne.Resource, fn func()) *tinyButton {
	b := &tinyButton{res: res, fn: fn}
	b.ExtendBaseWidget(b)
	return b
}

func (b *tinyButton) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewGridWrap(fyne.NewSize(14, 14), widget.NewIcon(b.res)))
}

func (b *tinyButton) Tapped(*fyne.PointEvent) { b.fn() }

func (b *tinyButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (p *popout) chooseProfiles() {
	g := p.g
	checks := container.NewVBox()
	for _, prof := range g.profiles {
		prof := prof
		c := widget.NewCheck(prof.Name, func(on bool) {
			g.settings.SetPopoutShows(prof.ID, on)
			_ = g.settings.Save(g.root)
			p.refresh()
			if g.selected == prof.ID {
				g.showDetail()
			}
		})
		c.SetChecked(g.settings.PopoutShows(prof.ID))
		checks.Add(c)
	}
	sc := vscroll(checks)
	sc.SetMinSize(fyne.NewSize(220, float32(min(40*len(g.profiles), 240))))
	dialog.NewCustom("Profiles in pop-out", "Done", sc, p.win).Show()
}

// dragArea wraps content so that pressing the primary mouse button on it
// starts moving the window (a borderless window's stand-in title bar).
type dragArea struct {
	widget.BaseWidget
	content fyne.CanvasObject
	start   func()
}

func newDragArea(content fyne.CanvasObject, start func()) *dragArea {
	d := &dragArea{content: content, start: start}
	d.ExtendBaseWidget(d)
	return d
}

func (d *dragArea) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(d.content) }

func (d *dragArea) MouseDown(ev *desktop.MouseEvent) {
	if ev.Button == desktop.MouseButtonPrimary {
		d.start()
	}
}

func (d *dragArea) MouseUp(*desktop.MouseEvent) {}

// arrangeSide lays out compact mode's icons: side by side for a single
// row, so they don't make the window taller, or stacked with close on top
// when there are several rows to fill.
func (p *popout) arrangeSide(rows int) {
	switch {
	case p.closeX == nil:
		p.side.Objects = []fyne.CanvasObject{p.expand}
	case rows > 1:
		p.side.Layout = layout.NewVBoxLayout()
		p.side.Objects = []fyne.CanvasObject{p.closeX, p.expand}
	default:
		p.side.Layout = layout.NewHBoxLayout()
		p.side.Objects = []fyne.CanvasObject{p.expand, p.closeX}
	}
	p.side.Refresh()
}
