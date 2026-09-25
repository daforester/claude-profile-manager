// Package ui is the Fyne desktop interface.
package ui

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/cli"
	"claude-profile-manager/internal/launcher"
	"claude-profile-manager/internal/native"
	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
	"claude-profile-manager/internal/usage"
)

//go:embed icon.png
var iconPNG []byte

// AppID identifies the app to the OS (preferences, notifications).
const AppID = "io.github.claudeprofilemanager"

type gui struct {
	app      fyne.App
	win      fyne.Window
	root     string
	store    *profile.Store
	settings *settings.Settings
	launcher *launcher.Launcher

	profiles []*profile.Profile
	accounts map[string]profile.Account
	selected string // profile ID

	list   *widget.List
	detail *fyne.Container

	routeLinks atomic.Bool // mirrors settings.RouteLinks for the background router

	trayOn      bool // main system-tray icon active this session
	monitor     *usage.Monitor
	refreshMins atomic.Int64
	trayIcons   *native.TrayIcons
	trayShown   map[string]bool // profile IDs with a native tray icon
	pop         *popout
	detailUsage func() // refreshes the usage card of the visible profile

	routingSetupShown bool // routing-setup reminder shown this session
}

// Run starts the GUI and blocks until it exits.
func Run(root string, store *profile.Store, st *settings.Settings) {
	a := app.NewWithID(AppID)
	a.Settings().SetTheme(newTheme())
	icon := fyne.NewStaticResource("icon.png", iconPNG)
	a.SetIcon(icon)

	g := &gui{
		app:      a,
		root:     root,
		store:    store,
		settings: st,
		launcher: &launcher.Launcher{Root: root, Settings: st},
		accounts: map[string]profile.Account{},
	}
	g.win = a.NewWindow("Claude Profile Manager")
	g.win.SetIcon(icon)
	g.win.Resize(fyne.NewSize(1000, 660))
	g.win.SetContent(g.build())
	g.win.SetMaster()
	g.trayOn = !g.settings.HideTray && g.hasTray()
	if g.trayOn {
		g.refreshTray()
		// Left-click on the tray icon shows the window.
		g.app.(desktop.App).SetSystemTrayWindow(g.win)
	}
	// With the tray on, closing the window keeps the app resident; quit
	// from the tray menu.
	g.win.SetCloseIntercept(func() {
		if g.trayOn {
			g.win.Hide()
			return
		}
		g.quit()
	})
	// Pick up logins that happened in a launched Claude while we were in
	// the background.
	a.Lifecycle().SetOnEnteredForeground(func() { g.reload(); g.refreshNewLogins() })
	g.startLinkRouter()
	g.startUsage()

	g.reload()
	if len(g.profiles) > 0 {
		g.selectID(g.profiles[0].ID)
	}
	// Native windows only exist once the event loop runs, so restore the
	// pop-out (and its always-on-top state) after start-up.
	a.Lifecycle().SetOnStarted(func() {
		if g.settings.PopoutOpen {
			g.showPopout()
		}
	})
	g.win.ShowAndRun()
	if g.trayIcons != nil {
		g.trayIcons.Close()
	}
}

func (g *gui) hasTray() bool { _, ok := g.app.(desktop.App); return ok }

func (g *gui) build() fyne.CanvasObject {
	g.list = widget.NewList(
		func() int { return len(g.profiles) },
		func() fyne.CanvasObject {
			dot := canvas.NewCircle(accent)
			dotBox := container.NewGridWrap(fyne.NewSize(14, 14), dot)
			name := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			name.Truncation = fyne.TextTruncateEllipsis
			sub := widget.NewLabel("")
			sub.Truncation = fyne.TextTruncateEllipsis
			sub.SizeName = theme.SizeNameCaptionText
			bar := newUsageBar(4)
			return container.NewBorder(nil, nil, container.NewCenter(dotBox), nil,
				container.New(layout.NewCustomPaddedVBoxLayout(-8), name, sub, container.NewPadded(bar)))
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			if id >= len(g.profiles) {
				return
			}
			p := g.profiles[id]
			row := o.(*fyne.Container)
			text := row.Objects[0].(*fyne.Container)
			dot := row.Objects[1].(*fyne.Container).Objects[0].(*fyne.Container).Objects[0].(*canvas.Circle)
			dot.FillColor = parseHex(p.Color)
			dot.Refresh()
			text.Objects[0].(*widget.Label).SetText(p.Name)
			text.Objects[1].(*widget.Label).SetText(g.accounts[p.ID].Summary())
			barBox := text.Objects[2].(*fyne.Container)
			bar := barBox.Objects[0].(*usageBar)
			if u := g.usageOf(p.ID); u.Session != nil {
				bar.Set(u.Session.Utilization, true)
				bar.SetStale(u.Stale())
				barBox.Show()
			} else {
				barBox.Hide()
			}
		},
	)
	g.list.OnSelected = func(id widget.ListItemID) {
		if id < len(g.profiles) {
			g.selected = g.profiles[id].ID
			g.showDetail()
		}
	}

	toolbar := widget.NewToolbar(
		widget.NewToolbarAction(theme.ContentAddIcon(), func() { g.editProfile(nil) }),
		widget.NewToolbarAction(theme.ViewRefreshIcon(), g.reload),
		widget.NewToolbarSpacer(),
		widget.NewToolbarAction(theme.GridIcon(), g.togglePopout),
		widget.NewToolbarAction(theme.SettingsIcon(), g.showSettings),
		widget.NewToolbarAction(theme.HelpIcon(), g.showAbout),
	)
	heading := widget.NewLabelWithStyle("Profiles", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	left := container.NewBorder(container.NewVBox(toolbar, heading), nil, nil, nil, g.list)

	g.detail = container.NewStack()
	split := container.NewHSplit(left, container.NewPadded(g.detail))
	split.Offset = 0.3
	return split
}

// reload re-reads profiles and account state from disk.
func (g *gui) reload() {
	g.profiles = g.store.List()
	for _, p := range g.profiles {
		g.accounts[p.ID] = p.ReadAccount()
	}
	g.list.Refresh()
	g.syncSelection()
	g.showDetail()
	g.refreshTray()
	g.refreshProfileTrays()
	g.pop.refresh()
}

func (g *gui) syncSelection() {
	for i, p := range g.profiles {
		if p.ID == g.selected {
			g.list.Select(i)
			return
		}
	}
	g.selected = ""
	g.list.UnselectAll()
}

func (g *gui) selectID(id string) {
	g.selected = id
	g.syncSelection()
	g.showDetail()
}

func (g *gui) current() *profile.Profile {
	for _, p := range g.profiles {
		if p.ID == g.selected {
			return p
		}
	}
	return nil
}

func (g *gui) showDetail() {
	if g.detail == nil {
		return
	}
	p := g.current()
	if p == nil {
		g.detail.Objects = []fyne.CanvasObject{g.emptyState()}
		g.detail.Refresh()
		return
	}
	g.detail.Objects = []fyne.CanvasObject{g.profileView(p)}
	g.detail.Refresh()
}

func (g *gui) emptyState() fyne.CanvasObject {
	if len(g.profiles) > 0 {
		return container.NewCenter(widget.NewLabel("Select a profile"))
	}
	title := widget.NewLabelWithStyle("No profiles yet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	body := widget.NewLabelWithStyle(
		"A profile is an isolated Claude login. Create one per account\n(e.g. Personal, Work, Client) and launch Claude Code or\nClaude Desktop in it — side by side, without signing out.",
		fyne.TextAlignCenter, fyne.TextStyle{})
	btn := widget.NewButtonWithIcon("Create your first profile", theme.ContentAddIcon(), func() { g.editProfile(nil) })
	btn.Importance = widget.HighImportance
	return container.NewCenter(container.NewVBox(title, body, container.NewCenter(btn)))
}

func (g *gui) profileView(p *profile.Profile) fyne.CanvasObject {
	acct := g.accounts[p.ID]

	swatch := canvas.NewRectangle(parseHex(p.Color))
	swatch.CornerRadius = 4
	swatchBox := container.NewGridWrap(fyne.NewSize(8, 40), swatch)
	name := widget.NewRichTextFromMarkdown("# " + escapeMD(p.Name))
	header := container.NewBorder(nil, nil, swatchBox, nil, name)
	var top []fyne.CanvasObject
	top = append(top, header)
	if p.Description != "" {
		d := widget.NewLabel(p.Description)
		d.Wrapping = fyne.TextWrapWord
		top = append(top, d)
	}

	// Launch actions.
	codeBtn := widget.NewButtonWithIcon("Claude Code", theme.MediaPlayIcon(), func() { g.launchCLI(p, "") })
	codeBtn.Importance = widget.HighImportance
	folderBtn := widget.NewButtonWithIcon("Claude Code in folder…", theme.FolderOpenIcon(), func() {
		d := dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, g.win)
				return
			}
			if u != nil {
				g.launchCLI(p, u.Path())
			}
		}, g.win)
		d.Show() // must be shown before resizing
		d.Resize(fyne.NewSize(760, 520))
	})
	desktopBtn := widget.NewButtonWithIcon("Claude Desktop", theme.ComputerIcon(), func() { g.launchDesktop(p) })
	launchRow := container.NewGridWithColumns(3, codeBtn, folderBtn, desktopBtn)

	// Status.
	codeStatus := "Not signed in yet — launch Claude Code and complete the login (or run /login)."
	if acct.SignedIn() {
		codeStatus = "Signed in: " + acct.Summary()
		if acct.Email == "" {
			codeStatus = "Signed in (account name appears after Claude Code has run once)"
		}
		if acct.DisplayName != "" && acct.Email != "" {
			codeStatus = fmt.Sprintf("Signed in as %s <%s>", acct.DisplayName, acct.Email)
			if acct.Organization != "" {
				codeStatus += " · " + acct.Organization
			}
		}
	}
	desktopStatus := "Never opened — you will be asked to sign in on first launch."
	if acct.DesktopInitialised {
		desktopStatus = "Has its own data directory (sign-in state is kept there)."
	}
	pasteLink := widget.NewButtonWithIcon("Paste sign-in link…", theme.ContentPasteIcon(), func() { g.pasteSignInLink(p) })
	pasteLink.Importance = widget.LowImportance
	status := widget.NewForm(
		widget.NewFormItem("Claude Code", wrapLabel(codeStatus)),
		widget.NewFormItem("Claude Desktop", container.NewVBox(wrapLabel(desktopStatus), container.NewHBox(pasteLink))),
	)

	// Configuration.
	env := "—"
	if len(p.Env) > 0 {
		keys := make([]string, 0, len(p.Env))
		for k := range p.Env {
			keys = append(keys, k)
		}
		env = strings.Join(sortedStrings(keys), ", ")
	}
	isolation := "CLAUDE_CONFIG_DIR"
	if p.IsolateHome {
		isolation += " + separate HOME"
	}
	if !p.KeepInheritedAuth {
		isolation += "; inherited API keys cleared"
	}
	last := "never"
	if !p.LastUsedAt.IsZero() {
		last = p.LastUsedAt.Local().Format("Mon 2 Jan 2006, 15:04")
	}
	cfg := widget.NewForm(
		widget.NewFormItem("Config dir", pathLabel(p.ConfigDir())),
		widget.NewFormItem("Desktop data", pathLabel(p.DesktopDir())),
		widget.NewFormItem("Starts in", pathLabel(p.StartDir())),
		widget.NewFormItem("Extra args", wrapLabel(orDash(p.ExtraArgs))),
		widget.NewFormItem("Env vars", wrapLabel(env)),
		widget.NewFormItem("Isolation", wrapLabel(isolation)),
		widget.NewFormItem("Last used", wrapLabel(last)),
	)

	// Secondary actions.
	copyBtn := widget.NewButtonWithIcon("Copy command", theme.ContentCopyIcon(), func() { g.copyCommand(p) })
	openBtn := widget.NewButtonWithIcon("Open folder", theme.FolderIcon(), func() {
		if err := p.EnsureDirs(); err == nil {
			g.openPath(p.Dir())
		}
	})
	editBtn := widget.NewButtonWithIcon("Edit", theme.DocumentCreateIcon(), func() { g.editProfile(p) })
	dupBtn := widget.NewButtonWithIcon("Duplicate", theme.ContentAddIcon(), func() { g.duplicate(p) })
	delBtn := widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() { g.deleteProfile(p) })
	delBtn.Importance = widget.DangerImportance
	actions := container.NewHBox(copyBtn, openBtn, editBtn, dupBtn, layout.NewSpacer(), delBtn)

	tip := widget.NewLabel(g.signInTip())
	tip.Wrapping = fyne.TextWrapWord
	tip.Importance = widget.LowImportance
	tip.SizeName = theme.SizeNameCaptionText

	body := container.NewVBox(
		container.NewVBox(top...),
		launchRow,
		widget.NewCard("", "Usage", g.usageCard(p)),
		widget.NewCard("", "Account", status),
		widget.NewCard("", "Configuration", cfg),
		actions,
		tip,
	)
	return container.NewVScroll(body)
}

func (g *gui) launchCLI(p *profile.Profile, dir string) {
	if err := g.launcher.OpenCLI(p, dir); err != nil {
		g.showLaunchError(err)
		return
	}
	_ = g.store.Touch(p)
	g.showDetail()
}

func (g *gui) launchDesktop(p *profile.Profile) {
	start := func() {
		if err := g.launcher.OpenDesktop(p); err != nil {
			g.showLaunchError(err)
			return
		}
		// The new instance re-registers itself as the claude:// handler
		// while starting up; take it back promptly.
		g.reassertLinksSoon()
		_ = g.store.Touch(p)
		g.showDetail()
	}
	if launcher.ProtocolSupported() && !g.settings.RouteLinks && !g.settings.RouteLinksAsked {
		g.offerLinkRouting(start)
		return
	}
	start()
	// Routing is on but Windows still sends links elsewhere: show the fix
	// (once per session) so the sign-in about to happen works.
	if g.settings.RouteLinks && !g.routingSetupShown && launcher.ProtocolStatus(g.root).Warning != "" {
		g.routingSetupShown = true
		g.showRoutingSetup()
	}
}

func (g *gui) showLaunchError(err error) {
	d := dialog.NewCustomConfirm("Could not launch", "Open Settings", "Close", wrapLabelWidth(err.Error(), 420),
		func(ok bool) {
			if ok {
				g.showSettings()
			}
		}, g.win)
	d.Show()
}

func (g *gui) copyCommand(p *profile.Profile) {
	cli, err := g.launcher.CLIPath()
	if err != nil {
		cli = "claude"
	}
	shells := []string{launcher.ShellPOSIX}
	if isWindows() {
		shells = []string{launcher.ShellPowerShell, launcher.ShellCmd}
	}
	sel := widget.NewRadioGroup(shells, nil)
	sel.Horizontal = true
	out := widget.NewMultiLineEntry()
	out.Wrapping = fyne.TextWrapBreak
	out.SetMinRowsVisible(5)
	render := func(sh string) {
		s, err := launcher.Snippet(p, cli, sh)
		if err != nil {
			s = err.Error()
		}
		out.SetText(s)
	}
	sel.OnChanged = render
	sel.SetSelected(shells[0])
	content := container.NewVBox(
		wrapLabelWidth("Paste this into a terminal to start Claude Code with this profile in the current directory. It contains your custom environment values.", 520),
		sel, out)
	d := dialog.NewCustomConfirm("Command for "+p.Name, "Copy", "Close", content, func(ok bool) {
		if ok {
			g.app.Clipboard().SetContent(out.Text)
		}
	}, g.win)
	d.Resize(fyne.NewSize(620, 320))
	d.Show()
}

func (g *gui) duplicate(src *profile.Profile) {
	name := src.Name + " copy"
	for i := 2; ; i++ {
		if _, err := g.store.Get(name); err != nil {
			break
		}
		name = fmt.Sprintf("%s copy %d", src.Name, i)
	}
	p := g.store.New(name)
	p.Description = src.Description
	p.WorkingDir = src.WorkingDir
	p.ExtraArgs = src.ExtraArgs
	p.IsolateHome = src.IsolateHome
	p.KeepInheritedAuth = src.KeepInheritedAuth
	if len(src.Env) > 0 {
		p.Env = map[string]string{}
		for k, v := range src.Env {
			p.Env[k] = v
		}
	}
	if err := g.store.Save(p); err != nil {
		dialog.ShowError(err, g.win)
		return
	}
	// Copy customisations (not the login) so the duplicate starts signed out.
	if _, err := p.CopyConfigFrom(src.ConfigDir()); err != nil {
		dialog.ShowError(err, g.win)
	}
	g.selected = p.ID
	g.reload()
	g.editProfile(p)
}

func (g *gui) deleteProfile(p *profile.Profile) {
	purge := widget.NewCheck("Also delete its data (Claude Code login, history, Claude Desktop data)", nil)
	purge.SetChecked(true)
	msg := wrapLabelWidth(fmt.Sprintf("Remove the profile %q?", p.Name), 440)
	content := container.NewVBox(msg, purge)
	if p.ClaudeConfigDir != "" || p.DesktopDataDir != "" {
		content.Add(wrapLabelWidth("Custom directories you chose yourself are never deleted.", 440))
	}
	dialog.NewCustomConfirm("Delete profile", "Delete", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		if err := g.store.Delete(p.ID, purge.Checked); err != nil {
			dialog.ShowError(err, g.win)
		}
		g.selected = ""
		g.reload()
		if len(g.profiles) > 0 {
			g.selectID(g.profiles[0].ID)
		}
	}, g.win).Show()
}

func (g *gui) openPath(path string) {
	if err := launcher.OpenPath(path); err != nil {
		dialog.ShowError(err, g.win)
	}
}

func (g *gui) showAbout() {
	text := fmt.Sprintf(`Claude Profile Manager %s

Each profile keeps its own Claude Code configuration directory (CLAUDE_CONFIG_DIR) and its own Claude Desktop data directory (--user-data-dir), so each can be signed in to a different Claude account and they can run at the same time.

Data folder: %s

Command line: run "cpm help" (or this app with "help") for list / run / open / desktop / env commands, handy for shortcuts and scripts.`,
		cli.Version, g.root)
	open := widget.NewButtonWithIcon("Open data folder", theme.FolderOpenIcon(), func() { g.openPath(g.root) })
	d := dialog.NewCustom("About", "Close", container.NewVBox(wrapLabelWidth(text, 480), open), g.win)
	d.Show()
}

// refreshTray rebuilds the main system-tray menu.
func (g *gui) refreshTray() {
	desk, ok := g.app.(desktop.App)
	if !ok || !g.trayOn {
		return
	}
	var items []*fyne.MenuItem
	for _, p := range g.profiles {
		p := p
		label := p.Name
		if u := g.usageOf(p.ID); u.Session != nil {
			label += fmt.Sprintf("  —  %.0f%%", u.Session.Utilization)
			if u.Stale() {
				label += " (" + staleReason(u) + ")"
			}
		}
		item := fyne.NewMenuItem(label, nil)
		item.ChildMenu = fyne.NewMenu("",
			fyne.NewMenuItem("Claude Code", func() { g.launchCLI(p, "") }),
			fyne.NewMenuItem("Claude Desktop", func() { g.launchDesktop(p) }),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Show in Profile Manager", func() { g.showWindow(p.ID) }),
		)
		items = append(items, item)
	}
	if len(items) > 0 {
		items = append(items, fyne.NewMenuItemSeparator())
	}
	quit := fyne.NewMenuItem("Quit Claude Profile Manager", g.quit)
	quit.IsQuit = true
	items = append(items,
		fyne.NewMenuItem("Usage pop-out", g.togglePopout),
		fyne.NewMenuItem("Show Profile Manager", func() { g.showWindow("") }),
		fyne.NewMenuItemSeparator(),
		quit,
	)
	desk.SetSystemTrayMenu(fyne.NewMenu("Claude Profiles", items...))
}

// showWindow brings the main window back, optionally selecting a profile.
func (g *gui) showWindow(profileID string) {
	g.win.Show()
	g.win.RequestFocus()
	if profileID != "" {
		g.selectID(profileID)
	}
}

func wrapLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// wrapLabelWidth is a wrapped label with a minimum width, for dialogs.
func wrapLabelWidth(s string, w float32) fyne.CanvasObject {
	l := wrapLabel(s)
	return container.NewStack(container.NewGridWrap(fyne.NewSize(w, 0)), l)
}

func pathLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapBreak
	l.Selectable = true
	return l
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func escapeMD(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "#", `\#`, "`", "\\`", "[", `\[`, "]", `\]`)
	return r.Replace(s)
}

func isWindows() bool { return os.PathSeparator == '\\' }
