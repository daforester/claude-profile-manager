//go:build windows

package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"claude-profile-manager/internal/profile"
	"claude-profile-manager/internal/settings"
)

const (
	createNewConsole      = 0x00000010
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
	createNoWindow        = 0x08000000
)

// TerminalChoices lists the terminal options offered on this OS.
func TerminalChoices() []string {
	return []string{settings.TerminalAuto, settings.TerminalWindowsTerminal, settings.TerminalPowerShell, settings.TerminalCmd, settings.TerminalCustom}
}

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

// hidden runs helper processes (PowerShell queries) without flashing a console.
func hidden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// newConsole starts cmd in its own console window using a raw command line,
// avoiding Go's argument re-quoting which cmd.exe does not understand.
func newConsole(exe, cmdline, dir string) error {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline, CreationFlags: createNewConsole | createNewProcessGroup}
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func openTerminal(s *settings.Settings, p *profile.Profile, script, dir string) error {
	choice := s.Terminal
	if choice == "" || choice == settings.TerminalAuto {
		if _, err := exec.LookPath("wt.exe"); err == nil {
			choice = settings.TerminalWindowsTerminal
		} else {
			choice = settings.TerminalCmd
		}
	}
	title := Title(p)
	switch choice {
	case settings.TerminalWindowsTerminal:
		wt, err := exec.LookPath("wt.exe")
		if err != nil {
			return errors.New("Windows Terminal (wt.exe) not found — choose another terminal in Settings")
		}
		// The script sets the environment itself, because a new tab may be
		// hosted by an already-running Windows Terminal process that would
		// not inherit ours.
		// A trailing backslash (e.g. `E:\`) would escape the closing quote.
		wtDir := dir
		if strings.HasSuffix(wtDir, `\`) {
			wtDir += "."
		}
		line := fmt.Sprintf(`"%s" -w new new-tab --title "%s" -d "%s" cmd.exe /k "%s"`,
			wt, strings.NewReplacer(`"`, `'`, ";", ",").Replace(title), wtDir, script)
		cmd := exec.Command(wt)
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
		cmd.Dir = dir
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	case settings.TerminalPowerShell:
		ps, err := exec.LookPath("pwsh.exe")
		if err != nil {
			ps, err = exec.LookPath("powershell.exe")
			if err != nil {
				return errors.New("PowerShell not found")
			}
		}
		line := fmt.Sprintf(`"%s" -NoLogo -NoExit -ExecutionPolicy Bypass -File "%s"`, ps, PowerShellScriptPath(p))
		return newConsole(ps, line, dir)
	case settings.TerminalCustom:
		cmd, err := customTerminalCommand(s.CustomTerminal, script, title, dir)
		if err != nil {
			return err
		}
		return startDetached(cmd)
	default:
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = `C:\Windows\System32\cmd.exe`
		}
		return newConsole(comspec, fmt.Sprintf(`"%s" /k "%s"`, comspec, script), dir)
	}
}

// ErrDesktopMSIX means Claude Desktop is installed as an MSIX package, whose
// executable Windows will not start directly with custom arguments. A
// portable copy (see MakePortableDesktop) solves this.
var ErrDesktopMSIX = errors.New("Claude Desktop is installed from the Microsoft Store/MSIX package, which cannot be started with a custom data directory. Open Settings and click \"Create portable copy\" once, then try again")

// ErrDesktopNotFound means no Claude Desktop installation was detected.
var ErrDesktopNotFound = errors.New("Claude Desktop not found — install it or set its path in Settings")

// FindDesktop locates a Claude Desktop executable that accepts arguments.
func FindDesktop(root, configured string) (string, error) {
	if configured != "" {
		if fileExists(configured) {
			return configured, nil
		}
		return "", errors.New("configured Claude Desktop path does not exist: " + configured)
	}
	if exe := findExe(PortableDesktopDir(root)); exe != "" {
		return exe, nil
	}
	// Classic (Squirrel) installer: %LOCALAPPDATA%\AnthropicClaude\app-<ver>\claude.exe
	squirrel := filepath.Join(os.Getenv("LOCALAPPDATA"), "AnthropicClaude")
	if v := newestVersionDir(squirrel, "app-"); v != "" {
		if exe := findExe(v); exe != "" {
			return exe, nil
		}
	}
	if exe := findExe(squirrel); exe != "" {
		return exe, nil
	}
	if pkg, err := findMSIX(); err == nil && pkg.InstallLocation != "" {
		return "", ErrDesktopMSIX
	}
	return "", ErrDesktopNotFound
}

func findExe(dir string) string {
	for _, n := range []string{"claude.exe", "Claude.exe"} {
		if p := filepath.Join(dir, n); fileExists(p) {
			return p
		}
	}
	return ""
}

// MSIXPackage describes an installed Claude Desktop package.
type MSIXPackage struct {
	InstallLocation string
	Version         string
}

func findMSIX() (*MSIXPackage, error) {
	script := `$p = Get-AppxPackage | Where-Object { $_.Name -like '*Claude*' -and $_.Publisher -like '*Anthropic*' } | Sort-Object Version -Descending | Select-Object -First 1; ` +
		`if (-not $p) { $p = Get-AppxPackage -Name '*Claude*' | Sort-Object Version -Descending | Select-Object -First 1 }; ` +
		`if ($p) { @{ InstallLocation = $p.InstallLocation; Version = $p.Version.ToString() } | ConvertTo-Json -Compress }`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	hidden(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 {
		return nil, errors.New("no Claude MSIX package installed")
	}
	var pkg MSIXPackage
	if err := json.Unmarshal(out, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

// PortableSupported reports whether this OS needs/supports a portable copy.
func PortableSupported() bool { return true }

// MSIXVersion returns the installed MSIX Claude Desktop version, if any.
func MSIXVersion() (string, error) {
	pkg, err := findMSIX()
	if err != nil {
		return "", err
	}
	return pkg.Version, nil
}

// MakePortableDesktop copies the MSIX-installed Claude Desktop into
// <root>\desktop-portable so it can be launched with --user-data-dir. The
// copy does not auto-update; run it again after Claude Desktop updates.
func MakePortableDesktop(root string, progress func(string)) (string, error) {
	pkg, err := findMSIX()
	if err != nil {
		return "", fmt.Errorf("locating the Claude Desktop package: %w", err)
	}
	src := filepath.Join(pkg.InstallLocation, "app")
	if !dirExists(src) {
		src = pkg.InstallLocation
	}
	dst := PortableDesktopDir(root)
	tmp := dst + ".new"
	_ = os.RemoveAll(tmp)
	if progress != nil {
		progress("Copying " + src)
	}
	if err := copyDir(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if findExe(tmp) == "" {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("copied %s but found no claude.exe in it", src)
	}
	old := dst + ".old"
	_ = os.RemoveAll(old)
	if dirExists(dst) {
		if err := os.Rename(dst, old); err != nil {
			_ = os.RemoveAll(tmp)
			return "", fmt.Errorf("replacing the previous copy (is Claude Desktop still running from it?): %w", err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	_ = os.RemoveAll(old)
	return pkg.Version, nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyRegular(path, target, info.Mode().Perm())
	})
}
