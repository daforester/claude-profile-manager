package ui

import (
	"fmt"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"claude-profile-manager/internal/launcher"
	"claude-profile-manager/internal/settings"
)

var terminalLabels = map[string]string{
	settings.TerminalAuto:            "Automatic",
	settings.TerminalWindowsTerminal: "Windows Terminal",
	settings.TerminalPowerShell:      "PowerShell",
	settings.TerminalCmd:             "Command Prompt",
	settings.TerminalMacTerminal:     "Terminal",
	settings.TerminalITerm:           "iTerm2",
	settings.TerminalCustom:          "Custom command",
}

func (g *gui) showSettings() {
	s := g.settings

	detectedCLI, cliErr := launcher.FindCLI("")
	cliPath := widget.NewEntry()
	cliPath.SetText(s.ClaudeCLIPath)
	if cliErr == nil {
		cliPath.SetPlaceHolder("Detected: " + detectedCLI)
	} else {
		cliPath.SetPlaceHolder("Not detected — enter the path to claude")
	}

	deskPath := widget.NewEntry()
	deskPath.SetText(s.ClaudeDesktopPath)
	detectedDesk, deskErr := launcher.FindDesktop(g.root, "")
	switch {
	case deskErr == nil:
		deskPath.SetPlaceHolder("Detected: " + detectedDesk)
	case runtime.GOOS == "darwin":
		deskPath.SetPlaceHolder("Path to Claude.app")
	default:
		deskPath.SetPlaceHolder("Not detected")
	}

	choices := launcher.TerminalChoices()
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = terminalLabels[c]
	}
	custom := widget.NewEntry()
	custom.SetText(s.CustomTerminal)
	custom.SetPlaceHolder(customPlaceholder())
	term := widget.NewSelect(labels, func(l string) {
		if l == terminalLabels[settings.TerminalCustom] {
			custom.Enable()
		} else {
			custom.Disable()
		}
	})
	current := s.Terminal
	if current == "" {
		current = settings.TerminalAuto
	}
	term.SetSelected(terminalLabels[current])
	if term.Selected == "" {
		term.SetSelected(terminalLabels[settings.TerminalAuto])
	}

	tray := widget.NewCheck("Show system tray icon (closing the window keeps Profile Manager running; quit from the tray menu)", nil)
	tray.SetChecked(!s.HideTray)

	items := []*widget.FormItem{
		widget.NewFormItem("Claude Code CLI", g.withFileBrowse(cliPath)),
		widget.NewFormItem("Claude Desktop", g.withFileBrowse(deskPath)),
		widget.NewFormItem("Terminal", term),
		widget.NewFormItem("Custom command", custom),
		widget.NewFormItem("", tray),
	}
	items[3].HintText = "{script} = launch script, {title} = window title"
	if deskErr != nil && deskErr != launcher.ErrDesktopNotFound {
		items[1].HintText = firstLine(deskErr.Error())
	}
	content := container.NewVBox(widget.NewForm(items...))

	usageBox, applyUsage := g.usageSettings()
	content.Add(widget.NewSeparator())
	content.Add(usageBox)

	linkBox, applyLinks := g.linkSettings()
	if linkBox != nil {
		content.Add(widget.NewSeparator())
		content.Add(linkBox)
	}

	if launcher.PortableSupported() {
		content.Add(widget.NewSeparator())
		content.Add(g.portableSection())
	}

	d := dialog.NewCustomConfirm("Settings", "Save", "Cancel", container.NewVScroll(content), func(ok bool) {
		if !ok {
			return
		}
		s.ClaudeCLIPath = strings.TrimSpace(cliPath.Text)
		s.ClaudeDesktopPath = strings.TrimSpace(deskPath.Text)
		for c, l := range terminalLabels {
			if l == term.Selected {
				s.Terminal = c
			}
		}
		s.CustomTerminal = strings.TrimSpace(custom.Text)
		trayWas := !s.HideTray
		s.HideTray = !tray.Checked
		applyUsage()
		if err := s.Save(g.root); err != nil {
			dialog.ShowError(err, g.win)
		}
		applyLinks()
		g.showDetail()
		switch {
		case tray.Checked && !g.trayOn && g.hasTray():
			// Turning the tray on takes effect immediately.
			g.trayOn = true
			g.refreshTray()
			g.app.(desktop.App).SetSystemTrayWindow(g.win)
			g.win.SetCloseIntercept(g.win.Hide)
		case !tray.Checked && trayWas && g.trayOn:
			g.trayOn = false
			g.win.SetCloseIntercept(g.app.Quit)
			dialog.ShowInformation("System tray", "The tray icon will be removed the next time Profile Manager starts. Closing the window now quits the app.", g.win)
		}
	}, g.win)
	d.Resize(fyne.NewSize(780, 680))
	d.Show()
}

// portableSection manages the Windows portable copy of an MSIX install.
func (g *gui) portableSection() fyne.CanvasObject {
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	update := func() {
		installed, err := launcher.MSIXVersion()
		have := g.settings.PortableDesktopVersion
		switch {
		case err != nil && have == "":
			status.SetText("Portable copy: not needed (no Microsoft Store/MSIX install of Claude Desktop found).")
		case err != nil:
			status.SetText("Portable copy: version " + have + ".")
		case have == "":
			status.SetText(fmt.Sprintf("Claude Desktop %s is installed as an MSIX package, which Windows will not start with a separate data directory. Create a portable copy to run profiles side by side.", installed))
		case have != installed:
			status.SetText(fmt.Sprintf("Portable copy is %s but Claude Desktop %s is installed — refresh it.", have, installed))
		default:
			status.SetText("Portable copy is up to date (" + have + ").")
		}
	}
	update()
	btn := widget.NewButton("Create / refresh portable copy", nil)
	btn.OnTapped = func() {
		btn.Disable()
		prog := dialog.NewCustomWithoutButtons("Copying Claude Desktop…", widget.NewProgressBarInfinite(), g.win)
		prog.Show()
		go func() {
			ver, err := launcher.MakePortableDesktop(g.root, nil)
			fyne.Do(func() {
				prog.Hide()
				btn.Enable()
				if err != nil {
					dialog.ShowError(err, g.win)
					return
				}
				g.settings.PortableDesktopVersion = ver
				_ = g.settings.Save(g.root)
				update()
				dialog.ShowInformation("Portable copy ready", "Claude Desktop "+ver+" was copied to\n"+launcher.PortableDesktopDir(g.root)+"\n\nRefresh it after Claude Desktop updates.", g.win)
			})
		}()
	}
	title := widget.NewLabelWithStyle("Claude Desktop (Windows Store / MSIX installs)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(title, status, container.NewHBox(btn))
}

func customPlaceholder() string {
	switch runtime.GOOS {
	case "windows":
		return `e.g. "C:\Program Files\Alacritty\alacritty.exe" -e cmd /k {script}`
	case "darwin":
		return "e.g. open -a WezTerm {script}"
	default:
		return "e.g. kitty --title {title} {script}"
	}
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return s[:i+1]
	}
	return s
}
