//go:build windows

package launcher

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"claude-profile-manager/internal/shellwords"
)

const (
	schemeKey      = `Software\Classes\claude`
	progID         = "ClaudeProfileManager.Link"
	progIDKey      = `Software\Classes\` + progID
	capabilities   = `Software\ClaudeProfileManager\Capabilities`
	registeredApps = `Software\RegisteredApplications`
	appName        = "Claude Profile Manager"
	userChoiceKey  = `Software\Microsoft\Windows\Shell\Associations\UrlAssociations\claude\UserChoice`
)

// ProtocolSupported reports whether claude:// routing is available here.
func ProtocolSupported() bool { return true }

func handlerCommand(exe string) string { return `"` + exe + `" handle-url "%1"` }

func isOurCommand(cmd string) bool { return strings.Contains(cmd, " handle-url ") }

func readDefault(path string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, _ := k.GetStringValue("")
	return s
}

func setValues(path string, values map[string]string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for name, v := range values {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	return nil
}

func deleteTree(path string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	subs, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, s := range subs {
		if err := deleteTree(path + `\` + s); err != nil {
			return err
		}
	}
	return registry.DeleteKey(registry.CURRENT_USER, path)
}

func userChoice() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, userChoiceKey, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, _ := k.GetStringValue("ProgId")
	return s
}

// ProtocolStatus reports the current claude:// handler.
func ProtocolStatus(root string) ProtocolState {
	cur := readDefault(schemeKey + `\shell\open\command`)
	st := ProtocolState{Ours: isOurCommand(cur), Handler: cur}
	if uc := userChoice(); uc != "" && uc != progID {
		st.Ours = false
		st.Warning = "Windows is set to open claude:// links with another app (" + uc + "). Choose \"" + appName + "\" for CLAUDE in Default apps."
	} else if eff := effectiveCommand(1); st.Ours && !strings.HasPrefix(eff, "(") && !isOurCommand(eff) {
		st.Ours = false
		st.Warning = "Windows actually opens claude:// links with: " + eff + ". Choose \"" + appName + "\" for CLAUDE in Default apps."
	}
	return st
}

// EnsureProtocol makes Profile Manager the claude:// handler. It is cheap
// when nothing changed and is called repeatedly, because every Claude
// Desktop start re-registers Claude itself. The handler it replaces is
// remembered so it can be restored and used for the main install.
func EnsureProtocol(root string) (bool, error) {
	exe, err := selfExe()
	if err != nil {
		return false, err
	}
	want := handlerCommand(exe)
	cur := readDefault(schemeKey + `\shell\open\command`)
	if cur == want {
		return false, nil
	}
	// Remember the main install's handler, but not our own portable copy
	// (which also registers itself when a profile instance starts).
	if cur != "" && !isOurCommand(cur) &&
		!strings.Contains(strings.ToLower(cur), strings.ToLower(PortableDesktopDir(root))) {
		_ = writeBackup(root, cur)
	}
	icon := `"` + exe + `",0`
	steps := []struct {
		path string
		vals map[string]string
	}{
		{schemeKey, map[string]string{"": "URL:claude", "URL Protocol": ""}},
		{schemeKey + `\DefaultIcon`, map[string]string{"": icon}},
		{schemeKey + `\shell\open\command`, map[string]string{"": want}},
		// A ProgID + Capabilities entry lists us under Settings > Default
		// apps, so the user can pick us if Windows has a UserChoice set.
		{progIDKey, map[string]string{"": "Claude link (" + appName + ")", "URL Protocol": ""}},
		{progIDKey + `\DefaultIcon`, map[string]string{"": icon}},
		{progIDKey + `\shell\open\command`, map[string]string{"": want}},
		{capabilities, map[string]string{"ApplicationName": appName, "ApplicationDescription": "Routes Claude Desktop sign-in links to the right profile"}},
		{capabilities + `\URLAssociations`, map[string]string{"claude": progID}},
		{registeredApps, map[string]string{appName: capabilities}},
	}
	for _, s := range steps {
		if err := setValues(s.path, s.vals); err != nil {
			return false, err
		}
	}
	return true, nil
}

// UnregisterProtocol hands claude:// back to the handler we replaced.
func UnregisterProtocol(root string) error {
	backup := readBackup(root)
	var err error
	if backup != "" {
		err = setValues(schemeKey+`\shell\open\command`, map[string]string{"": backup})
	} else if isOurCommand(readDefault(schemeKey + `\shell\open\command`)) {
		err = deleteTree(schemeKey)
	}
	_ = deleteTree(progIDKey)
	_ = deleteTree(`Software\ClaudeProfileManager`)
	if k, e := registry.OpenKey(registry.CURRENT_USER, registeredApps, registry.SET_VALUE); e == nil {
		_ = k.DeleteValue(appName)
		k.Close()
	}
	return err
}

// OpenDefaultAppsSettings opens Windows' default-apps page for this app.
func OpenDefaultAppsSettings() error {
	return exec.Command("explorer.exe", "ms-settings:defaultapps?registeredAppUser="+strings.ReplaceAll(appName, " ", "%20")).Start()
}

// mainHandlerCommand builds the command line of the handler we replaced.
func mainHandlerCommand(root, url string) []string {
	b := readBackup(root)
	if b == "" || isOurCommand(b) {
		return nil
	}
	args, err := shellwords.Split(b)
	if err != nil || len(args) == 0 {
		return nil
	}
	replaced := false
	for i, a := range args {
		for _, ph := range []string{"%1", "%l", "%L"} {
			if strings.Contains(a, ph) {
				args[i] = strings.ReplaceAll(a, ph, url)
				replaced = true
			}
		}
	}
	if !replaced {
		args = append(args, url)
	}
	return args
}

var (
	shlwapi            = syscall.NewLazyDLL("shlwapi.dll")
	pAssocQueryStringW = shlwapi.NewProc("AssocQueryStringW")
)

// effectiveCommand asks Windows which command it will actually run for a
// claude:// link — this accounts for UserChoice, packaged apps and HKLM.
func effectiveCommand(what uintptr) string {
	const assocfIsProtocol = 0x00001000
	scheme, _ := syscall.UTF16PtrFromString("claude")
	verb, _ := syscall.UTF16PtrFromString("open")
	buf := make([]uint16, 2048)
	n := uint32(len(buf))
	r, _, _ := pAssocQueryStringW.Call(assocfIsProtocol, what, uintptr(unsafe.Pointer(scheme)),
		uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r != 0 { // S_OK == 0
		return fmt.Sprintf("(unavailable, HRESULT 0x%08X)", uint32(r))
	}
	return syscall.UTF16ToString(buf)
}

func platformDiagnostics() string {
	const assocstrCommand, assocstrFriendlyAppName = 1, 4
	var b strings.Builder
	fmt.Fprintf(&b, "Windows will run:         %s\n", effectiveCommand(assocstrCommand))
	fmt.Fprintf(&b, "Windows app name:         %s\n", effectiveCommand(assocstrFriendlyAppName))
	uc := userChoice()
	if uc == "" {
		uc = "(none)"
	}
	fmt.Fprintf(&b, "UserChoice ProgId:        %s\n", uc)
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, schemeKey+`\shell\open\command`, registry.QUERY_VALUE); err == nil {
		v, _, _ := k.GetStringValue("")
		k.Close()
		fmt.Fprintf(&b, "HKLM handler:             %s\n", v)
	}
	return b.String()
}

// WatchProtocol re-claims claude:// the moment anything (Claude Desktop, on
// start-up) rewrites the registration, instead of waiting for the next poll.
// It blocks until enabled() returns false after a change; run it in a
// goroutine.
func WatchProtocol(root string, enabled func() bool) {
	const notifyName, notifyLastSet = 0x1, 0x4
	for enabled() {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, schemeKey, registry.NOTIFY|registry.QUERY_VALUE)
		if err != nil {
			return
		}
		// Blocks until the key or a subkey changes (or is deleted).
		err = windows.RegNotifyChangeKeyValue(windows.Handle(k), true, notifyName|notifyLastSet, 0, false)
		k.Close()
		if err != nil {
			return
		}
		if enabled() {
			_, _ = EnsureProtocol(root)
		}
	}
}
