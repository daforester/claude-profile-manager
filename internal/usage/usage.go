// Package usage reads each profile's Claude plan usage (the same numbers as
// Claude Code's /usage: the 5-hour session window and the weekly window).
//
// It uses the profile's own Claude Code OAuth access token, read from
// <config dir>/.credentials.json (Windows/Linux) or the macOS Keychain, and
// calls the endpoint Claude Code itself uses. Tokens are never refreshed or
// written here — if a token has expired, the next Claude Code run in that
// profile refreshes it.
package usage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"claude-profile-manager/internal/appdir"
	"claude-profile-manager/internal/profile"
)

// Endpoint is the OAuth usage API used by Claude Code's /usage.
// CPM_USAGE_ENDPOINT overrides it (for testing).
var Endpoint = envOr("CPM_USAGE_ENDPOINT", "https://api.anthropic.com/api/oauth/usage")

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Window is one rate-limit window.
type Window struct {
	Utilization float64   `json:"utilization"` // percent, 0–100
	ResetsAt    time.Time `json:"resetsAt,omitempty"`
}

// Usage is a snapshot for one profile.
type Usage struct {
	Session   *Window   `json:"session,omitempty"` // five_hour
	Weekly    *Window   `json:"weekly,omitempty"`  // seven_day
	Plan      string    `json:"plan,omitempty"`    // e.g. "max", "pro"
	FetchedAt time.Time `json:"fetchedAt,omitempty"`
	// Err describes why the latest refresh failed (the previous numbers,
	// if any, are kept).
	Err string `json:"err,omitempty"`
	// NoLogin means the profile has no Claude Code login to read usage with.
	NoLogin bool `json:"noLogin,omitempty"`
	// LoginExpired means the stored token has expired or was rejected;
	// polling resumes once Claude Code writes a new one.
	LoginExpired bool `json:"loginExpired,omitempty"`
}

// Has reports whether any numbers are available.
func (u Usage) Has() bool { return u.Session != nil || u.Weekly != nil }

// Stale reports whether the numbers shown are left over from an earlier
// refresh because the latest one failed.
func (u Usage) Stale() bool { return u.Err != "" && u.Has() }

// Metric picks the value tray icons track: "session", "weekly" or "max".
func (u Usage) Metric(which string) (float64, bool) {
	s, w := u.Session, u.Weekly
	switch which {
	case "weekly":
		if w != nil {
			return w.Utilization, true
		}
	case "max":
		switch {
		case s != nil && w != nil:
			if w.Utilization > s.Utilization {
				return w.Utilization, true
			}
			return s.Utilization, true
		case w != nil:
			return w.Utilization, true
		}
	}
	if s != nil {
		return s.Utilization, true
	}
	return 0, false
}

// Credentials is the subset of Claude Code's stored OAuth login we use.
type Credentials struct {
	AccessToken      string
	ExpiresAt        time.Time
	SubscriptionType string
}

type credFile struct {
	ClaudeAiOauth struct {
		AccessToken      string `json:"accessToken"`
		ExpiresAt        int64  `json:"expiresAt"` // unix ms
		SubscriptionType string `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// ErrNoLogin means the profile has no Claude Code login.
var ErrNoLogin = errors.New("no Claude Code login in this profile")

// ReadCredentials loads the profile's Claude Code OAuth token.
func ReadCredentials(p *profile.Profile) (*Credentials, error) {
	var raw []byte
	var err error
	if runtime.GOOS == "darwin" {
		raw, err = keychain(p.ConfigDir())
	}
	if runtime.GOOS != "darwin" || err != nil {
		raw, err = os.ReadFile(filepath.Join(p.ConfigDir(), ".credentials.json"))
	}
	if err != nil {
		return nil, ErrNoLogin
	}
	var f credFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}
	if f.ClaudeAiOauth.AccessToken == "" {
		return nil, ErrNoLogin
	}
	c := &Credentials{AccessToken: f.ClaudeAiOauth.AccessToken, SubscriptionType: f.ClaudeAiOauth.SubscriptionType}
	if f.ClaudeAiOauth.ExpiresAt > 0 {
		c.ExpiresAt = time.UnixMilli(f.ClaudeAiOauth.ExpiresAt)
	}
	return c, nil
}

// Expired reports whether the token is past its expiry time.
func (c *Credentials) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !now.Before(c.ExpiresAt)
}

// fingerprint identifies a token without keeping the token itself around.
func fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// KeychainService is the macOS Keychain service Claude Code uses for a
// config dir: plain for ~/.claude when CLAUDE_CONFIG_DIR is unset, otherwise
// suffixed with the first 8 hex chars of SHA-256(config dir).
func KeychainService(configDir string) string {
	if configDir == "" || configDir == appdir.DefaultClaudeConfigDir() && os.Getenv("CLAUDE_CONFIG_DIR") == "" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(configDir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

func keychain(configDir string) ([]byte, error) {
	// A locked keychain can make `security` wait on a GUI prompt; don't let
	// that stall the monitor.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", KeychainService(configDir), "-w").Output()
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

// RateLimitError is returned for HTTP 429; RetryAfter may be zero.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string { return "usage API rate-limited; will retry later" }

// ErrUnauthorized means the API rejected the token.
var ErrUnauthorized = errors.New("login expired — open Claude Code in this profile to refresh it")

// parseRetryAfter reads a Retry-After header in seconds or HTTP-date form.
func parseRetryAfter(v string, now time.Time) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

type apiWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

type apiResponse struct {
	FiveHour *apiWindow `json:"five_hour"`
	SevenDay *apiWindow `json:"seven_day"`
}

var client = &http.Client{Timeout: 20 * time.Second}

// Fetch calls the usage endpoint with an OAuth access token.
func Fetch(ctx context.Context, token string) (Usage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint, nil)
	if err != nil {
		return Usage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-profile-manager")
	resp, err := client.Do(req)
	if err != nil {
		return Usage{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return Usage{}, &RateLimitError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Usage{}, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return Usage{}, fmt.Errorf("usage API returned %s", resp.Status)
	}
	return parse(body)
}

func parse(body []byte) (Usage, error) {
	var r apiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return Usage{}, fmt.Errorf("unexpected usage response: %w", err)
	}
	u := Usage{FetchedAt: time.Now(), Session: toWindow(r.FiveHour), Weekly: toWindow(r.SevenDay)}
	if !u.Has() {
		return u, errors.New("usage response had no limits (API-key or enterprise account?)")
	}
	return u, nil
}

func toWindow(a *apiWindow) *Window {
	if a == nil || a.Utilization == nil {
		return nil
	}
	w := &Window{Utilization: *a.Utilization}
	if a.ResetsAt != nil && *a.ResetsAt != "" {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999-07:00", "2006-01-02T15:04:05"} {
			if t, err := time.Parse(layout, *a.ResetsAt); err == nil {
				w.ResetsAt = t
				break
			}
		}
	}
	return w
}
