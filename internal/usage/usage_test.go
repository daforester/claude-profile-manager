package usage

import (
	"context"
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
}

func newMonitorFixture(t *testing.T) *monitorFixture {
	f := &monitorFixture{status: http.StatusOK}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
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
