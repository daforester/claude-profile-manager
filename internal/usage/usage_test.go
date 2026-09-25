package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
