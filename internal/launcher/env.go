// Package launcher starts Claude Code and Claude Desktop inside a profile's
// isolated context.
//
// Isolation works like this:
//
//   - Claude Code reads CLAUDE_CONFIG_DIR. Everything it stores — settings,
//     history, projects, .claude.json and (on Windows/Linux)
//     .credentials.json — moves there. On macOS the OAuth token lives in the
//     Keychain under an entry keyed by a hash of CLAUDE_CONFIG_DIR, so each
//     profile gets its own login there too.
//   - Claude Desktop is an Electron app and honours --user-data-dir, which
//     relocates its whole state, including its session. A separate data
//     directory also gives a separate single-instance lock, so several
//     profiles can run side by side.
//   - Inherited ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN /
//     CLAUDE_CODE_OAUTH_TOKEN are cleared (unless the profile opts out)
//     because they would silently override the profile's own login.
package launcher

import (
	"os"
	"runtime"
	"sort"
	"strings"

	"claude-profile-manager/internal/profile"
)

// AuthEnvVars override interactive logins when present in the environment.
var AuthEnvVars = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"}

// EnvVar is one change applied on top of the inherited environment.
// Unset means the variable is removed.
type EnvVar struct {
	Key   string
	Value string
	Unset bool
}

// Overrides lists the environment changes for p, in a stable order:
// removals first, then CLAUDE_CONFIG_DIR, home overrides and custom vars.
func Overrides(p *profile.Profile) []EnvVar {
	var out []EnvVar
	set := map[string]bool{}
	add := func(k, v string) {
		out = append(out, EnvVar{Key: k, Value: v})
		set[strings.ToUpper(k)] = true
	}

	var vars []EnvVar
	add("CLAUDE_CONFIG_DIR", p.ConfigDir())
	if p.IsolateHome {
		add("HOME", p.HomeDir())
		if runtime.GOOS == "windows" {
			add("USERPROFILE", p.HomeDir())
		}
	}
	keys := make([]string, 0, len(p.Env))
	for k := range p.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !set[strings.ToUpper(k)] {
			add(k, p.Env[k])
		}
	}
	vars, out = out, nil

	if !p.KeepInheritedAuth {
		for _, k := range AuthEnvVars {
			if !set[k] {
				out = append(out, EnvVar{Key: k, Unset: true})
			}
		}
	}
	return append(out, vars...)
}

// Environ applies Overrides(p) to base (normally os.Environ()).
func Environ(p *profile.Profile, base []string) []string {
	ov := Overrides(p)
	drop := map[string]bool{}
	for _, v := range ov {
		drop[normKey(v.Key)] = true
	}
	out := make([]string, 0, len(base)+len(ov))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if k == "" || !drop[normKey(k)] {
			out = append(out, kv)
		}
	}
	for _, v := range ov {
		if !v.Unset {
			out = append(out, v.Key+"="+v.Value)
		}
	}
	return out
}

// ProcessEnv is Environ applied to the current process environment.
func ProcessEnv(p *profile.Profile) []string { return Environ(p, os.Environ()) }

func normKey(k string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k)
	}
	return k
}
