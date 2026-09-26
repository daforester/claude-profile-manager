package ui

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/native"
	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
	"claude-profile-manager/internal/usage"
)

// ---- usage bar widget -------------------------------------------------

// usageBar is a thin rounded progress bar coloured by level.
type usageBar struct {
	widget.BaseWidget
	pct    float64
	has    bool
	stale  bool
	height float32
}

func newUsageBar(height float32) *usageBar {
	b := &usageBar{height: height}
	b.ExtendBaseWidget(b)
	return b
}

// Set updates the value (0–100).
func (b *usageBar) Set(pct float64, has bool) {
	if b.pct == pct && b.has == has {
		return
	}
	b.pct, b.has = pct, has
	b.Refresh()
}

// SetStale fades the fill to show the value is out of date.
func (b *usageBar) SetStale(stale bool) {
	if b.stale == stale {
		return
	}
	b.stale = stale
	b.Refresh()
}

func (b *usageBar) fillColor() color.NRGBA {
	c := native.LevelColor(b.pct)
	if b.stale {
		c.A = 0x55
	}
	return c
}

func (b *usageBar) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	fill := canvas.NewRectangle(b.fillColor())
	r := &usageBarRenderer{b: b, bg: bg, fill: fill}
	r.Refresh()
	return r
}

type usageBarRenderer struct {
	b        *usageBar
	bg, fill *canvas.Rectangle
}

func (r *usageBarRenderer) Layout(size fyne.Size) {
	h := r.b.height
	y := (size.Height - h) / 2
	r.bg.Move(fyne.NewPos(0, y))
	r.bg.Resize(fyne.NewSize(size.Width, h))
	w := size.Width * float32(math.Max(0, math.Min(100, r.b.pct))) / 100
	if r.b.has && r.b.pct > 0 && w < h {
		w = h
	}
	r.fill.Move(fyne.NewPos(0, y))
	r.fill.Resize(fyne.NewSize(w, h))
}

func (r *usageBarRenderer) MinSize() fyne.Size { return fyne.NewSize(40, r.b.height) }

func (r *usageBarRenderer) Refresh() {
	r.bg.FillColor = theme.Color(theme.ColorNameInputBackground)
	r.bg.CornerRadius = r.b.height / 2
	r.fill.FillColor = r.b.fillColor()
	r.fill.CornerRadius = r.b.height / 2
	if r.b.has {
		r.fill.Show()
	} else {
		r.fill.Hide()
	}
	r.Layout(r.b.Size())
	r.bg.Refresh()
	r.fill.Refresh()
}

func (r *usageBarRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.bg, r.fill} }
func (r *usageBarRenderer) Destroy()                     {}

// ---- monitor wiring ---------------------------------------------------

func (g *gui) startUsage() {
	g.refreshMins.Store(int64(g.settings.UsageRefreshMinutes))
	g.trayShown = map[string]bool{}
	if native.TraySupported() {
		g.trayIcons = native.NewTrayIcons([]native.MenuItem{
			{ID: "code", Label: "Open Claude Code"},
			{ID: "desktop", Label: "Open Claude Desktop"},
			{},
			{ID: "refresh", Label: "Refresh usage"},
			{ID: "popout", Label: "Usage pop-out"},
			{ID: "show", Label: "Show Profile Manager"},
		}, func(id string) {
			fyne.Do(func() { g.showWindow(id) })
		}, func(id, action string) {
			fyne.Do(func() { g.profileTrayAction(id, action) })
		})
	}
	g.monitor = usage.NewMonitor(g.root, g.store,
		func() time.Duration { return time.Duration(g.refreshMins.Load()) * time.Minute },
		func(id string, _ usage.Usage) { fyne.Do(g.onUsageChanged) })
	go g.monitor.Run(context.Background())
	g.refreshProfileTrays()
}

func (g *gui) usageOf(id string) usage.Usage {
	if g.monitor == nil {
		return usage.Usage{}
	}
	return g.monitor.Get(id)
}

// onUsageChanged updates every place that shows usage (main thread).
func (g *gui) onUsageChanged() {
	g.list.Refresh()
	if g.detailUsage != nil {
		g.detailUsage()
	}
	g.refreshTray()
	g.refreshProfileTrays()
	g.pop.refresh()
}

// refreshNewLogins polls straight away for profiles that just signed in.
func (g *gui) refreshNewLogins() {
	if g.monitor == nil {
		return
	}
	for _, p := range g.profiles {
		if g.usageOf(p.ID).NoLogin && g.accounts[p.ID].SignedIn() {
			g.monitor.Refresh(p.ID)
		}
	}
}

func (g *gui) profileTrayAction(id, action string) {
	p, err := g.store.Get(id)
	if err != nil {
		return
	}
	switch action {
	case "code":
		g.launchCLI(p, "")
	case "desktop":
		g.launchDesktop(p)
	case "refresh":
		g.monitor.Refresh(p.ID)
	case "popout":
		g.showPopout()
	case "show":
		g.showWindow(p.ID)
	}
}

// refreshProfileTrays adds, updates or removes the per-profile tray icons.
func (g *gui) refreshProfileTrays() {
	if g.trayIcons == nil {
		return
	}
	want := map[string]bool{}
	size := native.IconSize()
	for _, p := range g.profiles {
		if !p.TrayUsage {
			continue
		}
		want[p.ID] = true
		u := g.usageOf(p.ID)
		v, has := u.Metric(g.settings.TrayMetric)
		img := native.RenderIcon(g.settings.TrayIconStyle, v, has, parseHex(p.Color), size)
		if u.Stale() {
			native.Fade(img)
		}
		g.trayIcons.Set(p.ID, native.PNG(img), trayTooltip(p, u))
		g.trayShown[p.ID] = true
	}
	for id := range g.trayShown {
		if !want[id] {
			g.trayIcons.Remove(id)
			delete(g.trayShown, id)
		}
	}
}

func trayTooltip(p *profile.Profile, u usage.Usage) string {
	var parts []string
	if u.Session != nil {
		parts = append(parts, fmt.Sprintf("5h %.0f%%", u.Session.Utilization))
	}
	if u.Weekly != nil {
		parts = append(parts, fmt.Sprintf("week %.0f%%", u.Weekly.Utilization))
	}
	switch {
	case len(parts) > 0 && u.Stale():
		return fmt.Sprintf("%s — %s as of %s (%s)", p.Name, strings.Join(parts, ", "), ago(u.FetchedAt), staleReason(u))
	case len(parts) > 0 && u.Session != nil && !u.Session.ResetsAt.IsZero():
		return fmt.Sprintf("%s — %s (resets %s)", p.Name, strings.Join(parts, ", "), u.Session.ResetsAt.Local().Format("15:04"))
	case len(parts) > 0:
		return p.Name + " — " + strings.Join(parts, ", ")
	case u.NoLogin:
		return p.Name + " — sign in to Claude Code to see usage"
	case u.Err != "":
		return p.Name + " — " + u.Err
	default:
		return p.Name + " — usage loading…"
	}
}

// ---- usage card in the profile view ------------------------------------

func (g *gui) usageCard(p *profile.Profile) fyne.CanvasObject {
	sessBar, weekBar := newUsageBar(10), newUsageBar(10)
	sessText, weekText := widget.NewLabel(""), widget.NewLabel("")
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	status.SizeName = theme.SizeNameCaptionText

	update := func() {
		u := g.usageOf(p.ID)
		setWindow(sessBar, sessText, u.Session)
		setWindow(weekBar, weekText, u.Weekly)
		sessBar.SetStale(u.Stale())
		weekBar.SetStale(u.Stale())
		status.SetText(usageStatus(u))
	}
	g.detailUsage = update
	update()

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		status.SetText("Refreshing…")
		g.monitor.Refresh(p.ID)
	})
	refresh.Importance = widget.LowImportance

	opts := container.NewHBox()
	if native.TraySupported() {
		tray := widget.NewCheck("Tray icon", func(on bool) {
			if on == p.TrayUsage {
				return
			}
			p.TrayUsage = on
			if err := g.store.Save(p); err != nil {
				dialog.ShowError(err, g.win)
			}
			g.refreshProfileTrays()
		})
		tray.SetChecked(p.TrayUsage)
		opts.Add(tray)
	}
	inPop := widget.NewCheck("In pop-out", func(on bool) {
		g.settings.SetPopoutShows(p.ID, on)
		_ = g.settings.Save(g.root)
		g.pop.refresh()
	})
	inPop.SetChecked(g.settings.PopoutShows(p.ID))
	opts.Add(inPop)

	row := func(label string, bar *usageBar, text *widget.Label) fyne.CanvasObject {
		l := widget.NewLabelWithStyle(label, fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
		return container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(110, 36), l),
			container.NewGridWrap(fyne.NewSize(230, 36), text), bar)
	}
	return container.NewVBox(
		row("5-hour", sessBar, sessText),
		row("Weekly", weekBar, weekText),
		container.NewBorder(nil, nil, nil, container.NewHBox(opts, refresh), status),
	)
}

func setWindow(bar *usageBar, text *widget.Label, w *usage.Window) {
	if w == nil {
		bar.Set(0, false)
		text.SetText("—")
		return
	}
	bar.Set(w.Utilization, true)
	s := fmt.Sprintf("%.0f%%", w.Utilization)
	if !w.ResetsAt.IsZero() {
		s += " · resets " + resetText(w.ResetsAt)
	}
	text.SetText(s)
}

func resetText(t time.Time) string {
	t = t.Local()
	d := time.Until(t)
	switch {
	case d <= 0:
		return "now"
	case d < 24*time.Hour && t.Day() == time.Now().Day():
		return t.Format("15:04") + " (" + shortDuration(d) + ")"
	case d < 7*24*time.Hour:
		return t.Format("Mon 15:04")
	default:
		return t.Format("2 Jan 15:04")
	}
}

func shortDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func usageStatus(u usage.Usage) string {
	switch {
	case u.NoLogin:
		return "No Claude Code login in this profile — usage appears after you sign in to Claude Code here."
	case u.LoginExpired && u.Has():
		return "Login expired and couldn't be renewed — sign in to Claude Code in this profile again. Showing values from " + ago(u.FetchedAt) + "."
	case u.Err != "" && u.Has():
		return "Last refresh failed: " + u.Err + " (showing " + ago(u.FetchedAt) + " values)"
	case u.Err != "":
		return u.Err
	case u.FetchedAt.IsZero():
		return "Loading usage…"
	}
	s := "Updated " + ago(u.FetchedAt)
	if u.Plan != "" {
		s = "Plan: " + strings.ToUpper(u.Plan[:1]) + u.Plan[1:] + " · " + s
	}
	return s
}

// staleReason is a short note on why usage isn't updating.
func staleReason(u usage.Usage) string {
	if u.LoginExpired {
		return "login expired"
	}
	return "not updating"
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "earlier"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	default:
		return t.Local().Format("15:04")
	}
}

// usageSettings is the Usage section of the Settings dialog.
func (g *gui) usageSettings() (fyne.CanvasObject, func()) {
	intervals := []string{"2 minutes", "5 minutes", "10 minutes", "15 minutes", "30 minutes"}
	mins := []int{2, 5, 10, 15, 30}
	interval := widget.NewSelect(intervals, nil)
	interval.SetSelected("5 minutes")
	for i, m := range mins {
		if m == g.settings.UsageRefreshMinutes {
			interval.SetSelected(intervals[i])
		}
	}
	styles := map[string]string{settings.TrayStyleBar: "Usage bar", settings.TrayStylePercent: "Percentage number"}
	style := widget.NewSelect([]string{"Usage bar", "Percentage number"}, nil)
	style.SetSelected(styles[g.settings.TrayIconStyle])
	metrics := map[string]string{settings.MetricSession: "5-hour session", settings.MetricWeekly: "Weekly", settings.MetricMax: "Whichever is higher"}
	metric := widget.NewSelect([]string{"5-hour session", "Weekly", "Whichever is higher"}, nil)
	metric.SetSelected(metrics[g.settings.TrayMetric])

	items := []*widget.FormItem{widget.NewFormItem("Refresh every", interval)}
	if native.TraySupported() {
		items = append(items,
			widget.NewFormItem("Profile tray icons", style),
			widget.NewFormItem("Tray icons track", metric))
		items[1].HintText = "Turn a profile's icon on with “Tray icon” in its Usage section"
	}
	box := container.NewVBox(
		widget.NewLabelWithStyle("Usage", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewForm(items...))
	apply := func() {
		for i, l := range intervals {
			if l == interval.Selected {
				g.settings.UsageRefreshMinutes = mins[i]
				g.refreshMins.Store(int64(mins[i]))
			}
		}
		for k, v := range styles {
			if v == style.Selected {
				g.settings.TrayIconStyle = k
			}
		}
		for k, v := range metrics {
			if v == metric.Selected {
				g.settings.TrayMetric = k
			}
		}
		g.refreshProfileTrays()
	}
	return box, apply
}
