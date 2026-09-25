package ui

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/launcher"
	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
)

// linkReassertInterval is how often the running GUI checks that it still
// owns claude:// — every Claude Desktop start takes it back.
const linkReassertInterval = 3 * time.Second

// startLinkRouter keeps the claude:// registration while routing is on.
func (g *gui) startLinkRouter() {
	if !launcher.ProtocolSupported() {
		return
	}
	g.routeLinks.Store(g.settings.RouteLinks)
	if g.settings.RouteLinks {
		_, _ = launcher.EnsureProtocol(g.root)
	}
	// Instant re-claim when the registration changes (Windows); the ticker
	// below is the fallback. The watcher restarts if routing is re-enabled.
	go func() {
		for {
			if g.routeLinks.Load() {
				launcher.WatchProtocol(g.root, g.routeLinks.Load)
			}
			time.Sleep(linkReassertInterval)
		}
	}()
	go func() {
		for range time.Tick(linkReassertInterval) {
			if g.routeLinks.Load() {
				_, _ = launcher.EnsureProtocol(g.root)
			}
		}
	}()
}

// reassertLinksSoon re-claims claude:// shortly after a Desktop launch,
// covering the window in which the new instance registers itself.
func (g *gui) reassertLinksSoon() {
	if !g.routeLinks.Load() {
		return
	}
	go func() {
		for _, d := range []time.Duration{2, 5, 10, 20} {
			time.Sleep(d * time.Second)
			_, _ = launcher.EnsureProtocol(g.root)
		}
	}()
}

// setRouteLinks turns routing on or off and persists the choice.
func (g *gui) setRouteLinks(on bool) error {
	g.settings.RouteLinks = on
	g.settings.RouteLinksAsked = true
	g.routeLinks.Store(on)
	var err error
	if on {
		_, err = launcher.EnsureProtocol(g.root)
	} else {
		err = launcher.UnregisterProtocol(g.root)
	}
	if serr := g.settings.Save(g.root); err == nil {
		err = serr
	}
	return err
}

// offerLinkRouting asks once, before the first Desktop launch, whether to
// route sign-in links; then continues with next.
func (g *gui) offerLinkRouting(next func()) {
	msg := "Claude Desktop signs in through your browser, which sends the result back as a claude:// link. " +
		"Windows gives every such link to your main Claude install, so a profile window never receives its sign-in.\n\n" +
		"Let Claude Profile Manager handle claude:// links? Each link then goes to the profile you launched most recently " +
		"(or asks you which one). Keep Profile Manager running while you sign in. You can turn this off in Settings, which restores the original handler."
	if runtime.GOOS != "windows" {
		msg = strings.ReplaceAll(msg, "Windows gives", "Your system gives")
	}
	dialog.NewCustomConfirm("Route sign-in links to profiles?", "Enable (recommended)", "Not now",
		wrapLabelWidth(msg, 480), func(ok bool) {
			if ok {
				if err := g.setRouteLinks(true); err != nil {
					dialog.ShowError(err, g.win)
				} else if st := launcher.ProtocolStatus(g.root); st.Warning != "" {
					g.showRoutingSetup()
				}
			} else {
				g.settings.RouteLinksAsked = true
				_ = g.settings.Save(g.root)
			}
			next()
		}, g.win).Show()
}

// showRoutingSetup walks through the one-time Windows step that makes
// Profile Manager the default app for claude:// links. Windows protects that
// choice, so only the user can make it — either in the "How do you want to
// open this?" picker (triggered by the test link) or in Default apps.
func (g *gui) showRoutingSetup() {
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	status.TextStyle = fyne.TextStyle{Bold: true}
	check := func() bool {
		st := launcher.ProtocolStatus(g.root)
		if st.Ours && st.Warning == "" {
			status.Importance = widget.SuccessImportance
			status.SetText("✓ Profile Manager now opens claude:// links. Sign-ins will reach the right profile.")
			return true
		}
		status.Importance = widget.WarningImportance
		switch {
		case st.Warning != "":
			status.SetText("Not set yet: " + st.Warning)
		case !g.settings.RouteLinks:
			status.SetText("Not set yet: turn on “Route Claude Desktop sign-in links” in Settings first.")
		default:
			status.SetText("Not set yet: claude:// links currently open " + st.Handler)
		}
		return false
	}
	check()

	steps := wrapLabelWidth("Windows has more than one app for claude:// links (your Microsoft Store Claude and Profile Manager), "+
		"so it asks which to use, or picks Claude. Windows only lets you make this choice, once:\n\n"+
		"1.  Click “Test link”.\n"+
		"2.  In “How do you want to open this?”, choose Claude Profile Manager and tick “Always use this app” (or click “Always”).\n"+
		"3.  A “Link routing works” window confirms it.\n\n"+
		"If no picker appears, use “Default apps…”, find CLAUDE and choose Claude Profile Manager.", 520)
	test := widget.NewButtonWithIcon("Test link", theme.MediaPlayIcon(), func() {
		if err := launcher.OpenTestLink(); err != nil {
			dialog.ShowError(err, g.win)
		}
	})
	test.Importance = widget.HighImportance
	buttons := container.NewHBox(test)
	if runtime.GOOS == "windows" {
		buttons.Add(widget.NewButtonWithIcon("Default apps…", theme.SettingsIcon(), func() { _ = launcher.OpenDefaultAppsSettings() }))
	}
	d := dialog.NewCustom("Make Profile Manager open sign-in links", "Close",
		container.NewVBox(steps, buttons, status), g.win)
	// Re-check while the dialog is open so the user sees it succeed.
	done := make(chan struct{})
	d.SetOnClosed(func() { close(done) })
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				fyne.Do(func() { check() })
			}
		}
	}()
	d.Show()
}

// pasteSignInLink lets the user hand a claude:// link to a profile manually
// (e.g. copied from the browser's "Open Claude" page).
func (g *gui) pasteSignInLink(p *profile.Profile) {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("claude://…")
	if clip := strings.TrimSpace(g.app.Clipboard().Content()); launcher.IsClaudeURL(clip) {
		entry.SetText(clip)
	}
	help := wrapLabelWidth("If the browser's “Open Claude” prompt goes to the wrong window, cancel it, copy the claude:// link from the sign-in page (right-click the “Open Claude” button → Copy link), and paste it here. Claude Desktop for this profile must be running.", 500)
	dialog.NewCustomConfirm("Send sign-in link to "+p.Name, "Send", "Cancel", container.NewVBox(help, entry), func(ok bool) {
		if !ok {
			return
		}
		if err := g.launcher.DeliverURL(p, entry.Text); err != nil {
			dialog.ShowError(err, g.win)
		}
	}, g.win).Show()
}

func (g *gui) signInTip() string {
	switch {
	case g.settings.RouteLinks:
		return "Sign-in: claude:// links go to the profile whose Claude Desktop you launched in the last 10 minutes; otherwise you'll be asked which profile should get them. Keep Profile Manager running while signing in."
	case launcher.ProtocolSupported():
		return "Tip: Claude Desktop sign-in comes back through a claude:// link, which normally goes to your main install. Turn on link routing in Settings, or use “Paste sign-in link…”."
	default:
		return "Tip: Claude Desktop sign-in comes back through a claude:// link, which goes to the main install. Use “Paste sign-in link…” to send it to this profile."
	}
}

// linkSettings is the routing section of the Settings dialog; apply saves it.
func (g *gui) linkSettings() (fyne.CanvasObject, func()) {
	if !launcher.ProtocolSupported() {
		return nil, func() {}
	}
	check := widget.NewCheck("Route Claude Desktop sign-in links (claude://) to the right profile", nil)
	check.SetChecked(g.settings.RouteLinks)
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	status.Importance = widget.LowImportance
	st := launcher.ProtocolStatus(g.root)
	switch {
	case st.Warning != "":
		status.SetText(st.Warning)
	case st.Ours:
		status.SetText("claude:// links are currently handled by Profile Manager.")
	case st.Handler != "":
		status.SetText("claude:// links currently open: " + st.Handler)
	default:
		status.SetText("No claude:// handler is registered.")
	}
	box := container.NewVBox(
		widget.NewLabelWithStyle("Sign-in links", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		check, status)
	buttons := container.NewHBox(
		widget.NewButtonWithIcon("Set up / test…", theme.MediaPlayIcon(), g.showRoutingSetup),
		widget.NewButtonWithIcon("Diagnostics…", theme.InfoIcon(), g.showLinkDiagnostics))
	box.Add(buttons)
	apply := func() {
		if check.Checked != g.settings.RouteLinks || !g.settings.RouteLinksAsked {
			if err := g.setRouteLinks(check.Checked); err != nil {
				dialog.ShowError(err, g.win)
				return
			}
			if check.Checked && launcher.ProtocolStatus(g.root).Warning != "" {
				g.showRoutingSetup()
			}
		}
	}
	return box, apply
}

// HandleURL is the entry point when the OS opens a claude:// link with this
// executable. It forwards the link to the profile whose Desktop was launched
// most recently, or asks which Claude Desktop should receive it.
func HandleURL(root string, store *profile.Store, st *settings.Settings, url string) {
	l := &launcher.Launcher{Root: root, Settings: st}
	launcher.LogRoute(root, "(received)", url)
	if launcher.IsRoutingTest(url) {
		showRoutingConfirmed()
		return
	}
	if id, ok := launcher.TakeRecentDesktopLaunch(root); ok {
		if p, err := store.Get(id); err == nil {
			if err := l.DeliverURL(p, url); err == nil {
				return
			}
		}
	}
	profiles := store.List()
	if len(profiles) == 0 {
		_ = l.DeliverURL(nil, url)
		return
	}

	a := app.NewWithID(AppID)
	a.Settings().SetTheme(newTheme())
	icon := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(icon)
	w := a.NewWindow("Open Claude link")
	w.SetIcon(icon)

	// Most recently used first.
	sort.SliceStable(profiles, func(i, j int) bool { return profiles[i].LastUsedAt.After(profiles[j].LastUsedAt) })
	errLabel := widget.NewLabel("")
	errLabel.Importance = widget.DangerImportance
	errLabel.Wrapping = fyne.TextWrapWord
	send := func(p *profile.Profile) {
		if err := l.DeliverURL(p, url); err != nil {
			errLabel.SetText(err.Error())
			return
		}
		a.Quit()
	}
	buttons := container.NewVBox()
	for _, p := range profiles {
		p := p
		label := p.Name
		if acct := p.ReadAccount(); acct.Email != "" {
			label += "  ·  " + acct.Email
		}
		b := widget.NewButton(label, func() { send(p) })
		b.Alignment = widget.ButtonAlignLeading
		buttons.Add(b)
	}
	mainBtn := widget.NewButtonWithIcon("Main Claude Desktop (not a profile)", theme.ComputerIcon(), func() { send(nil) })
	cancel := widget.NewButton("Ignore link", func() { a.Quit() })
	header := wrapLabelWidth(fmt.Sprintf("Which Claude Desktop should receive this link?\n%s", shortURL(url)), 420)
	w.SetContent(container.NewPadded(container.NewVBox(header, buttons, widget.NewSeparator(), mainBtn, errLabel, container.NewHBox(cancel))))
	w.Resize(fyne.NewSize(460, 0))
	w.CenterOnScreen()
	w.ShowAndRun()
}

func shortURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if len(u) > 60 {
		u = u[:57] + "…"
	}
	return u
}

// showLinkDiagnostics shows who handles claude:// links and recent routing.
func (g *gui) showLinkDiagnostics() {
	report := launcher.LinkDiagnostics(g.root)
	text := widget.NewMultiLineEntry()
	text.SetText(report)
	text.Wrapping = fyne.TextWrapBreak
	text.TextStyle = fyne.TextStyle{Monospace: true}
	d := dialog.NewCustomConfirm("Sign-in link diagnostics", "Copy", "Close", text, func(ok bool) {
		if ok {
			g.app.Clipboard().SetContent(report)
		}
	}, g.win)
	d.Resize(fyne.NewSize(760, 520))
	d.Show()
}

// showRoutingConfirmed is shown when the setup test link reaches us.
func showRoutingConfirmed() {
	a := app.NewWithID(AppID)
	a.Settings().SetTheme(newTheme())
	icon := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(icon)
	w := a.NewWindow("Link routing works")
	w.SetIcon(icon)
	msg := widget.NewLabel("✓ Claude Profile Manager received the test link.\n\nClaude Desktop sign-ins will now go to the profile you launched.")
	msg.Wrapping = fyne.TextWrapWord
	msg.Importance = widget.SuccessImportance
	ok := widget.NewButton("OK", func() { a.Quit() })
	ok.Importance = widget.HighImportance
	w.SetContent(container.NewPadded(container.NewVBox(msg, container.NewCenter(ok))))
	w.Resize(fyne.NewSize(420, 0))
	w.CenterOnScreen()
	w.ShowAndRun()
}
