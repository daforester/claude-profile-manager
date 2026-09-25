package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/native"
)

// popout is a small always-available window listing usage per profile.
type popout struct {
	g       *gui
	win     fyne.Window
	rows    *fyne.Container
	pin     *widget.Check
	visible bool
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
	p.win = g.app.NewWindow("Claude usage")
	p.win.SetIcon(fyne.NewStaticResource("icon.png", iconPNG))
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
	header := container.NewHBox(p.pin, layout.NewSpacer(), refresh, choose)

	p.win.SetContent(container.NewPadded(container.NewBorder(header, nil, nil, nil, container.NewVScroll(p.rows))))
	p.win.Resize(fyne.NewSize(320, 200))
	p.win.SetCloseIntercept(func() { p.close() })
	return p
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
	p.rows.RemoveAll()
	shown := 0
	for _, prof := range g.profiles {
		if !g.settings.PopoutShows(prof.ID) {
			continue
		}
		shown++
		u := g.usageOf(prof.ID)
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
		p.rows.Add(widget.NewLabel("No profiles selected — use the list button above."))
	}
	p.rows.Refresh()
	h := float32(60 + 86*max(shown, 1))
	if h > 640 {
		h = 640
	}
	if p.win.Canvas().Size().Height < h-1 || !p.visible {
		p.win.Resize(fyne.NewSize(max(p.win.Canvas().Size().Width, 320), h))
	}
}

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
	sc := container.NewVScroll(checks)
	sc.SetMinSize(fyne.NewSize(220, float32(min(40*len(g.profiles), 240))))
	dialog.NewCustom("Profiles in pop-out", "Done", sc, p.win).Show()
}
