package ui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/appdir"
	"claude-profile-manager/internal/profile"
)

var colourNames = []string{"Clay", "Sky", "Olive", "Orchid", "Mustard", "Teal", "Cocoa", "Coral"}

func colourLabel(hex string) string {
	for i, h := range profile.Palette {
		if strings.EqualFold(h, hex) && i < len(colourNames) {
			return colourNames[i]
		}
	}
	return hex
}

func colourHex(label string) string {
	for i, n := range colourNames {
		if n == label && i < len(profile.Palette) {
			return profile.Palette[i]
		}
	}
	return label
}

// editProfile opens the create (p == nil) or edit dialog.
func (g *gui) editProfile(existing *profile.Profile) {
	creating := existing == nil
	var p profile.Profile
	if creating {
		p = *g.store.New("")
	} else {
		p = *existing
	}

	name := widget.NewEntry()
	name.SetText(p.Name)
	name.SetPlaceHolder("e.g. Personal, Work, Acme client")
	desc := widget.NewEntry()
	desc.SetText(p.Description)
	desc.SetPlaceHolder("Optional note, e.g. which account this is")

	colour := widget.NewSelect(colourNames, nil)
	colour.SetSelected(colourLabel(p.Color))

	workDir := widget.NewEntry()
	workDir.SetText(p.WorkingDir)
	workDir.SetPlaceHolder("Default: your home folder")

	args := widget.NewEntry()
	args.SetText(p.ExtraArgs)
	args.SetPlaceHolder("e.g. --model opus --permission-mode plan")

	env := widget.NewMultiLineEntry()
	env.SetText(profile.FormatEnv(p.Env))
	env.SetPlaceHolder("KEY=VALUE, one per line\ne.g. CLAUDE_CODE_USE_BEDROCK=1")
	env.SetMinRowsVisible(3)

	clearAuth := widget.NewCheck("Clear inherited ANTHROPIC_API_KEY / auth tokens (recommended)", nil)
	clearAuth.SetChecked(!p.KeepInheritedAuth)
	isoHome := widget.NewCheck("Also give Claude Code a separate home folder (hides your git/ssh config)", nil)
	isoHome.SetChecked(p.IsolateHome)

	cfgDir := widget.NewEntry()
	cfgDir.SetText(p.ClaudeConfigDir)
	cfgDir.SetPlaceHolder("Default: " + p.Dir() + string(os.PathSeparator) + "claude-code")
	deskDir := widget.NewEntry()
	deskDir.SetText(p.DesktopDataDir)
	deskDir.SetPlaceHolder("Default: " + p.Dir() + string(os.PathSeparator) + "claude-desktop")

	advanced := widget.NewForm(
		widget.NewFormItem("", clearAuth),
		widget.NewFormItem("", isoHome),
		widget.NewFormItem("Config dir", g.withBrowse(cfgDir)),
		widget.NewFormItem("Desktop data", g.withBrowse(deskDir)),
	)
	advanced.Items[2].HintText = "Point at an existing folder (e.g. ~/.claude) to adopt an existing login"

	items := []*widget.FormItem{
		widget.NewFormItem("Name", name),
		widget.NewFormItem("Description", desc),
		widget.NewFormItem("Colour", colour),
		widget.NewFormItem("Start in", g.withBrowse(workDir)),
		widget.NewFormItem("Extra CLI args", args),
		widget.NewFormItem("Environment", env),
	}
	items[4].HintText = "Appended to every Claude Code launch"

	var copyDefault *widget.Check
	defaultDir := appdir.DefaultClaudeConfigDir()
	if creating {
		if st, err := os.Stat(defaultDir); err == nil && st.IsDir() {
			copyDefault = widget.NewCheck("Copy settings, CLAUDE.md, agents, commands & skills from "+defaultDir, nil)
			copyDefault.SetChecked(true)
			items = append(items, widget.NewFormItem("", copyDefault))
		}
	}
	form := widget.NewForm(items...)
	acc := widget.NewAccordion(widget.NewAccordionItem("Advanced", advanced))
	if !creating && (p.IsolateHome || p.KeepInheritedAuth || p.ClaudeConfigDir != "" || p.DesktopDataDir != "") {
		acc.Open(0)
	}
	content := vscroll(container.NewVBox(form, acc))

	title := "Edit profile"
	confirm := "Save"
	if creating {
		title, confirm = "New profile", "Create"
	}
	var d *dialog.ConfirmDialog
	d = dialog.NewCustomConfirm(title, confirm, "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		envMap, err := profile.ParseEnv(env.Text)
		if err != nil {
			g.reopenWithError(d, err)
			return
		}
		if len(envMap) == 0 {
			envMap = nil
		}
		p.Name = name.Text
		p.Description = strings.TrimSpace(desc.Text)
		p.Color = colourHex(colour.Selected)
		p.WorkingDir = strings.TrimSpace(workDir.Text)
		p.ExtraArgs = strings.TrimSpace(args.Text)
		p.Env = envMap
		p.KeepInheritedAuth = !clearAuth.Checked
		p.IsolateHome = isoHome.Checked
		p.ClaudeConfigDir = strings.TrimSpace(cfgDir.Text)
		p.DesktopDataDir = strings.TrimSpace(deskDir.Text)

		saved := p
		if err := g.store.Save(&saved); err != nil {
			g.reopenWithError(d, err)
			return
		}
		if err := saved.EnsureDirs(); err != nil {
			dialog.ShowError(err, g.win)
		}
		if copyDefault != nil && copyDefault.Checked {
			if _, err := saved.CopyConfigFrom(defaultDir); err != nil {
				dialog.ShowError(fmt.Errorf("copying from %s: %w", defaultDir, err), g.win)
			}
		}
		g.selected = saved.ID
		g.reload()
	}, g.win)
	d.Resize(fyne.NewSize(720, 600))
	d.Show()
	g.win.Canvas().Focus(name)
}

// reopenWithError shows err, then brings the still-populated dialog back.
func (g *gui) reopenWithError(d *dialog.ConfirmDialog, err error) {
	e := dialog.NewError(err, g.win)
	e.SetOnClosed(func() { d.Show() })
	e.Show()
}

// withBrowse pairs an entry with a folder picker.
func (g *gui) withBrowse(e *widget.Entry) fyne.CanvasObject {
	btn := widget.NewButtonWithIcon("", theme.FolderOpenIcon(), func() {
		fd := dialog.NewFolderOpen(func(u fyne.ListableURI, err error) {
			if err == nil && u != nil {
				e.SetText(u.Path())
			}
		}, g.win)
		fd.Show() // must be shown before resizing
		fd.Resize(fyne.NewSize(760, 520))
	})
	return container.NewBorder(nil, nil, nil, btn, e)
}

// withFileBrowse pairs an entry with a file picker.
func (g *gui) withFileBrowse(e *widget.Entry) fyne.CanvasObject {
	btn := widget.NewButtonWithIcon("", theme.FileIcon(), func() {
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err == nil && r != nil {
				e.SetText(r.URI().Path())
				_ = r.Close()
			}
		}, g.win)
		fd.Show() // must be shown before resizing
		fd.Resize(fyne.NewSize(760, 520))
	})
	return container.NewBorder(nil, nil, nil, btn, e)
}

func sortedStrings(s []string) []string {
	sort.Strings(s)
	return s
}
