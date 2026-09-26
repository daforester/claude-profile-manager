package usage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"claude-profile-manager/internal/profile"
)

func TestParse(t *testing.T) {
	u, err := parse([]byte(`{"five_hour":{"utilization":42.5,"resets_at":"2026-09-25T15:00:00.123456+00:00"},"seven_day":{"utilization":12,"resets_at":null},"seven_day_opus":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if u.Session.Utilization != 42.5 || u.Session.ResetsAt.IsZero() || u.Weekly.Utilization != 12 || !u.Weekly.ResetsAt.IsZero() {
		t.Errorf("unexpected %+v %+v", u.Session, u.Weekly)
	}
	if v, _ := u.Metric("max"); v != 42.5 {
		t.Errorf("max = %v", v)
	}
	if v, _ := u.Metric("weekly"); v != 12 {
		t.Errorf("weekly = %v", v)
	}
	if _, err := parse([]byte(`{}`)); err == nil {
		t.Error("empty response should be an error")
	}
}

func TestFetchHeadersAndRateLimit(t *testing.T) {
	var gotAuth, gotBeta string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotBeta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		if status != http.StatusOK {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":5}}`))
	}))
	defer srv.Close()
	old := client
	client = srv.Client()
	defer func() { client = old }()
	endpointOverride(t, srv.URL)

	if _, err := Fetch(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" || gotBeta != "oauth-2025-04-20" {
		t.Errorf("headers: %q %q", gotAuth, gotBeta)
	}
	status = http.StatusTooManyRequests
	_, err := Fetch(context.Background(), "tok")
	rl, ok := err.(*RateLimitError)
	if !ok || rl.RetryAfter != 120*time.Second {
		t.Errorf("want rate limit with 120s, got %v", err)
	}
}

func TestReadCredentials(t *testing.T) {
	s, _ := profile.Open(t.TempDir())
	p := s.New("x")
	_ = s.Save(p)
	if _, err := ReadCredentials(p); err != ErrNoLogin {
		t.Errorf("want ErrNoLogin, got %v", err)
	}
	_ = p.EnsureDirs()
	_ = os.WriteFile(filepath.Join(p.ConfigDir(), ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"accessToken":"abc","expiresAt":1893456000000,"subscriptionType":"max"}}`), 0o600)
	c, err := ReadCredentials(p)
	if err != nil || c.AccessToken != "abc" || c.SubscriptionType != "max" || c.ExpiresAt.Year() != 2030 {
		t.Errorf("got %+v %v", c, err)
	}
}

func TestKeychainService(t *testing.T) {
	if got := KeychainService("/Users/me/profiles/a/claude-code"); len(got) != len("Claude Code-credentials-")+8 {
		t.Errorf("unexpected service %q", got)
	}
}

func endpointOverride(t *testing.T, url string) {
	old := Endpoint
	Endpoint = url
	t.Cleanup(func() { Endpoint = old })
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 25, 23, 0, 0, 0, time.UTC)
	if d := parseRetryAfter("2576", now); d != 2576*time.Second {
		t.Errorf("seconds: %v", d)
	}
	if d := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); d != 90*time.Second {
		t.Errorf("date: %v", d)
	}
	if d := parseRetryAfter("", now); d != 0 {
		t.Errorf("empty: %v", d)
	}
}

// monitorFixture is a monitor over one profile whose token and API
// responses the test controls.
type monitorFixture struct {
	m      *Monitor
	p      *profile.Profile
	calls  int
	status int
	// Token endpoint: renewals counts requests, gotAuth the usage call's
	// bearer token, and tokenReply/tokenStatus what renewal answers.
	renewals    int
	gotAuth     string
	tokenStatus int
	tokenReply  string
	// accept, if set, is the only token the usage API takes.
	accept string
}

func newMonitorFixture(t *testing.T) *monitorFixture {
	f := &monitorFixture{status: http.StatusOK, tokenStatus: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			f.renewals++
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["grant_type"] != "refresh_token" || body["client_id"] == "" {
				t.Errorf("renewal body %v", body)
			}
			w.WriteHeader(f.tokenStatus)
			_, _ = w.Write([]byte(f.tokenReply))
			return
		}
		f.calls++
		f.gotAuth = r.Header.Get("Authorization")
		if f.accept != "" && f.gotAuth != "Bearer "+f.accept {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if f.status != http.StatusOK {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(f.status)
			return
		}
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":5}}`))
	}))
	t.Cleanup(srv.Close)
	old := client
	client = srv.Client()
	t.Cleanup(func() { client = old })
	endpointOverride(t, srv.URL)
	oldTok := TokenEndpoint
	TokenEndpoint = srv.URL + "/token"
	t.Cleanup(func() { TokenEndpoint = oldTok })

	root := t.TempDir()
	s, _ := profile.Open(root)
	f.p = s.New("x")
	_ = s.Save(f.p)
	_ = f.p.EnsureDirs()
	f.m = NewMonitor(root, s, func() time.Duration { return 5 * time.Minute }, nil)
	return f
}

func (f *monitorFixture) login(t *testing.T, token string, expires time.Time) {
	b := []byte(`{"claudeAiOauth":{"accessToken":"` + token + `","expiresAt":` + strconv.FormatInt(expires.UnixMilli(), 10) + `}}`)
	if err := os.WriteFile(filepath.Join(f.p.ConfigDir(), ".credentials.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorSkipsExpiredToken(t *testing.T) {
	f := newMonitorFixture(t)
	ctx := context.Background()
	f.login(t, "old", time.Now().Add(-time.Minute))
	f.m.pollDue(ctx, "")
	if u := f.m.Get(f.p.ID); f.calls != 0 || !u.LoginExpired {
		t.Fatalf("expired token: calls=%d usage=%+v", f.calls, u)
	}
	// Still expired: neither the tick nor a manual refresh calls the API.
	f.m.pollDue(ctx, "")
	f.m.nextDue[f.p.ID] = time.Time{}
	f.m.pollDue(ctx, "")
	if f.calls != 0 {
		t.Fatalf("called API %d times with an expired token", f.calls)
	}
	// Claude Code signs in again: the next tick polls without waiting.
	f.login(t, "new", time.Now().Add(time.Hour))
	f.m.pollDue(ctx, "")
	if u := f.m.Get(f.p.ID); f.calls != 1 || u.Err != "" || u.LoginExpired || !u.Has() {
		t.Fatalf("after new login: calls=%d usage=%+v", f.calls, u)
	}
}

func TestMonitorRejectedTokenWaitsForNewLogin(t *testing.T) {
	f := newMonitorFixture(t)
	ctx := context.Background()
	f.login(t, "revoked", time.Now().Add(time.Hour))
	f.status = http.StatusUnauthorized
	f.m.pollDue(ctx, "")
	if u := f.m.Get(f.p.ID); !u.LoginExpired {
		t.Fatalf("usage=%+v", u)
	}
	f.status = http.StatusOK
	f.m.pollDue(ctx, "") // same token, not due: no call
	if f.calls != 1 {
		t.Fatalf("calls=%d", f.calls)
	}
	f.login(t, "fresh", time.Now().Add(time.Hour))
	f.m.pollDue(ctx, "")
	if u := f.m.Get(f.p.ID); f.calls != 2 || u.LoginExpired {
		t.Fatalf("calls=%d usage=%+v", f.calls, u)
	}
}

func TestMonitorHonoursRetryAfter(t *testing.T) {
	f := newMonitorFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.login(t, "tok", time.Now().Add(2*time.Hour))
	f.status = http.StatusTooManyRequests
	f.m.pollDue(ctx, "")
	u := f.m.Get(f.p.ID)
	if u.Err == "" || u.LoginExpired {
		t.Fatalf("usage=%+v", u)
	}
	if d := time.Until(f.m.nextDue[f.p.ID]); d < 59*time.Minute {
		t.Errorf("next poll in %v, want Retry-After (1h)", d)
	}
	// A manual refresh must not bypass the server's Retry-After.
	go f.m.Run(ctx)
	f.m.Refresh("")
	time.Sleep(200 * time.Millisecond)
	cancel()
	if f.calls != 1 {
		t.Errorf("manual refresh called the API during Retry-After (calls=%d)", f.calls)
	}
}

func (f *monitorFixture) credentials(t *testing.T) map[string]any {
	b, err := os.ReadFile(filepath.Join(f.p.ConfigDir(), ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *monitorFixture) loginWithRefresh(t *testing.T) {
	b := []byte(`{"claudeAiOauth":{"accessToken":"old","refreshToken":"r1","expiresAt":` +
		strconv.FormatInt(time.Now().Add(-time.Minute).UnixMilli(), 10) +
		`,"subscriptionType":"max","rateLimitTier":"tier"},"other":{"keep":true}}`)
	if err := os.WriteFile(filepath.Join(f.p.ConfigDir(), ".credentials.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorRenewsExpiredToken(t *testing.T) {
	f := newMonitorFixture(t)
	f.loginWithRefresh(t)
	f.tokenReply = `{"access_token":"new","refresh_token":"r2","expires_in":28800,"scope":"user:inference user:profile"}`
	f.m.pollDue(context.Background(), "")

	if u := f.m.Get(f.p.ID); f.renewals != 1 || f.calls != 1 || u.LoginExpired || u.Err != "" || u.Plan != "max" {
		t.Fatalf("renewals=%d calls=%d usage=%+v", f.renewals, f.calls, u)
	}
	if f.gotAuth != "Bearer new" {
		t.Errorf("usage called with %q", f.gotAuth)
	}
	d := f.credentials(t)
	o := d["claudeAiOauth"].(map[string]any)
	if o["accessToken"] != "new" || o["refreshToken"] != "r2" || o["rateLimitTier"] != "tier" || o["subscriptionType"] != "max" {
		t.Errorf("stored login %v", o)
	}
	if exp := time.UnixMilli(int64(o["expiresAt"].(float64))); time.Until(exp) < 7*time.Hour {
		t.Errorf("expiresAt %v", exp)
	}
	if d["other"] == nil {
		t.Error("unrelated fields were dropped")
	}
}

func TestMonitorRenewalRejected(t *testing.T) {
	f := newMonitorFixture(t)
	f.loginWithRefresh(t)
	before := f.credentials(t)
	f.tokenStatus = http.StatusBadRequest
	f.tokenReply = `{"error":"invalid_grant"}`
	ctx := context.Background()
	f.m.pollDue(ctx, "")
	if u := f.m.Get(f.p.ID); !u.LoginExpired || f.calls != 0 {
		t.Fatalf("calls=%d usage=%+v", f.calls, u)
	}
	if after := f.credentials(t); after["claudeAiOauth"].(map[string]any)["refreshToken"] != before["claudeAiOauth"].(map[string]any)["refreshToken"] {
		t.Error("credentials changed after a rejected renewal")
	}
	// Not retried until a new login appears.
	f.m.nextDue[f.p.ID] = time.Time{}
	f.m.pollDue(ctx, "")
	if f.renewals != 1 {
		t.Errorf("renewals=%d", f.renewals)
	}
}

func TestMonitorRenewsRejectedToken(t *testing.T) {
	f := newMonitorFixture(t)
	// A token that hasn't reached its expiry time but the API rejects.
	b := []byte(`{"claudeAiOauth":{"accessToken":"old","refreshToken":"r1","expiresAt":` +
		strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10) + `}}`)
	_ = os.WriteFile(filepath.Join(f.p.ConfigDir(), ".credentials.json"), b, 0o600)
	f.accept = "new"
	f.tokenReply = `{"access_token":"new","refresh_token":"r2","expires_in":3600}`
	f.m.pollDue(context.Background(), "")
	if u := f.m.Get(f.p.ID); f.renewals != 1 || f.calls != 2 || u.LoginExpired || !u.Has() {
		t.Fatalf("renewals=%d calls=%d usage=%+v", f.renewals, f.calls, u)
	}
}

func TestRenewedLoginKeptWhenSaveFails(t *testing.T) {
	f := newMonitorFixture(t)
	f.loginWithRefresh(t)
	f.tokenReply = `{"access_token":"new","refresh_token":"r2","expires_in":3600}`
	failing := true
	saveLogin = func(s *storedLogin, p *profile.Profile, raw []byte) error {
		if failing {
			return errors.New("access is denied")
		}
		return s.write(p, raw)
	}
	t.Cleanup(func() { saveLogin = (*storedLogin).write; unsaved.m = map[string]pendingLogin{} })

	f.m.pollDue(context.Background(), "")
	if u := f.m.Get(f.p.ID); u.Err != "" || f.gotAuth != "Bearer new" {
		t.Fatalf("usage=%+v auth=%q", u, f.gotAuth)
	}
	if o := f.credentials(t)["claudeAiOauth"].(map[string]any); o["refreshToken"] != "r1" {
		t.Fatalf("file changed although saving failed: %v", o)
	}
	// Still unsaved: reads use the renewed login rather than renewing again.
	if c, err := ReadCredentials(f.p); err != nil || c.AccessToken != "new" || f.renewals != 1 {
		t.Fatalf("creds=%+v err=%v renewals=%d", c, err, f.renewals)
	}
	// Once the file can be written, the next read saves it.
	failing = false
	_, _ = ReadCredentials(f.p)
	if o := f.credentials(t)["claudeAiOauth"].(map[string]any); o["refreshToken"] != "r2" || o["accessToken"] != "new" {
		t.Fatalf("renewed login not saved: %v", o)
	}
	if len(unsaved.m) != 0 {
		t.Error("unsaved login kept after saving")
	}
}

func TestUnsavedLoginDroppedForNewerOne(t *testing.T) {
	f := newMonitorFixture(t)
	f.loginWithRefresh(t)
	f.tokenReply = `{"access_token":"new","refresh_token":"r2","expires_in":3600}`
	saveLogin = func(*storedLogin, *profile.Profile, []byte) error { return errors.New("access is denied") }
	t.Cleanup(func() { saveLogin = (*storedLogin).write; unsaved.m = map[string]pendingLogin{} })
	f.m.pollDue(context.Background(), "")

	// Claude Code signs in again and writes its own login.
	f.login(t, "fromclaude", time.Now().Add(time.Hour))
	if c, err := ReadCredentials(f.p); err != nil || c.AccessToken != "fromclaude" {
		t.Fatalf("creds=%+v err=%v", c, err)
	}
	if len(unsaved.m) != 0 {
		t.Error("stale unsaved login kept")
	}
}
