package usage

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"claude-profile-manager/internal/profile"
)

// TokenEndpoint and ClientID are what Claude Code uses to renew its OAuth
// login. CPM_TOKEN_ENDPOINT and CPM_OAUTH_CLIENT_ID override them.
var (
	TokenEndpoint = envOr("CPM_TOKEN_ENDPOINT", "https://console.anthropic.com/v1/oauth/token")
	ClientID      = envOr("CPM_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e")
)

// storedLogin is a profile's raw credentials plus where they came from, so
// a renewed login can be written back in place with every other field kept.
type storedLogin struct {
	raw      []byte
	keychain bool
}

// unsaved holds renewed logins that couldn't be written back yet (Windows
// won't replace a file another process has open). Renewal replaces the
// refresh token, so losing one would sign the profile out; they are used
// in memory and saving is retried on every read.
var unsaved = struct {
	sync.Mutex
	m map[string]pendingLogin
}{m: map[string]pendingLogin{}}

type pendingLogin struct {
	p     *profile.Profile
	base  []byte // what was stored when the login was renewed
	login storedLogin
}

// SaveUnsaved makes a last attempt to write back renewed logins that are
// still only held in memory; call it before exiting.
func SaveUnsaved() {
	unsaved.Lock()
	var ps []*profile.Profile
	for _, pl := range unsaved.m {
		ps = append(ps, pl.p)
	}
	unsaved.Unlock()
	for _, p := range ps {
		_, _ = readStored(p)
	}
}

func readStored(p *profile.Profile) (*storedLogin, error) {
	disk, err := readDisk(p)
	unsaved.Lock()
	defer unsaved.Unlock()
	pl, ok := unsaved.m[p.ID]
	if !ok {
		return disk, err
	}
	if err == nil && !bytes.Equal(disk.raw, pl.base) {
		// Claude Code has stored a newer login since.
		delete(unsaved.m, p.ID)
		return disk, nil
	}
	if saveLogin(&pl.login, p, pl.login.raw) == nil {
		delete(unsaved.m, p.ID)
	}
	l := pl.login
	return &l, nil
}

func readDisk(p *profile.Profile) (*storedLogin, error) {
	if runtime.GOOS == "darwin" {
		if raw, err := keychain(p.ConfigDir()); err == nil {
			return &storedLogin{raw: raw, keychain: true}, nil
		}
	}
	raw, err := os.ReadFile(credentialsPath(p))
	if err != nil {
		return nil, ErrNoLogin
	}
	return &storedLogin{raw: raw}, nil
}

func credentialsPath(p *profile.Profile) string {
	return filepath.Join(p.ConfigDir(), ".credentials.json")
}

// saveLogin writes a login back; tests replace it.
var saveLogin = (*storedLogin).write

func (s *storedLogin) write(p *profile.Profile, raw []byte) error {
	if s.keychain {
		return writeKeychain(p.ConfigDir(), raw)
	}
	return writeFileAtomic(credentialsPath(p), raw)
}

// writeFileAtomic replaces path in one step so Claude Code never reads a
// half-written file. Windows refuses the replace while another process has
// the file open, so it retries briefly and then overwrites in place.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	for i := 0; ; i++ {
		err = os.Rename(tmp, path)
		if err == nil || i == 4 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err == nil {
		return nil
	}
	if werr := writeInPlace(path, data); werr != nil {
		return err
	}
	return nil
}

func writeInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeKeychain updates Claude Code's Keychain item. The secret goes over
// stdin (hex-encoded) so it never appears in a process listing.
func writeKeychain(configDir string, raw []byte) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "security", "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -a %q -s %q -X %s\n",
		u.Username, KeychainService(configDir), hex.EncodeToString(raw)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("updating Keychain: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

// Renew exchanges the profile's refresh token for a new access token and
// stores the result where Claude Code keeps it. Each renewal replaces the
// refresh token too, so writing both back keeps Claude Code signed in.
// It returns ErrUnauthorized when the login can't be renewed and needs a
// fresh sign-in.
func Renew(ctx context.Context, p *profile.Profile) (*Credentials, error) {
	stored, err := readStored(p)
	if err != nil {
		return nil, err
	}
	var doc map[string]json.RawMessage
	var oauth map[string]any
	if err := json.Unmarshal(stored.raw, &doc); err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}
	if err := json.Unmarshal(doc["claudeAiOauth"], &oauth); err != nil || oauth == nil {
		return nil, ErrNoLogin
	}
	refresh, _ := oauth["refreshToken"].(string)
	if refresh == "" {
		return nil, ErrUnauthorized
	}
	if ms, ok := oauth["refreshTokenExpiresAt"].(float64); ok && ms > 0 && !time.Now().Before(time.UnixMilli(int64(ms))) {
		return nil, ErrUnauthorized
	}

	tok, err := requestToken(ctx, refresh)
	if err != nil {
		return nil, err
	}

	// Claude Code may have renewed the login while we were waiting; its copy
	// is then the one to keep.
	if cur, err := readStored(p); err == nil && !bytes.Equal(cur.raw, stored.raw) {
		if c, err := parseCredentials(cur.raw); err == nil && !c.Expired(time.Now()) {
			return c, nil
		}
	}

	oauth["accessToken"] = tok.AccessToken
	if tok.RefreshToken != "" {
		oauth["refreshToken"] = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		oauth["expiresAt"] = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UnixMilli()
	}
	if tok.Scope != "" {
		oauth["scopes"] = strings.Fields(tok.Scope)
	}
	b, err := json.Marshal(oauth)
	if err != nil {
		return nil, err
	}
	doc["claudeAiOauth"] = b
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	werr := saveLogin(stored, p, out)
	unsaved.Lock()
	if werr == nil {
		delete(unsaved.m, p.ID)
	} else {
		base := stored.raw
		if pl, ok := unsaved.m[p.ID]; ok {
			base = pl.base // what's on disk is still the older login
		}
		unsaved.m[p.ID] = pendingLogin{p: p, base: base, login: storedLogin{raw: out, keychain: stored.keychain}}
	}
	unsaved.Unlock()
	return parseCredentials(out)
}

func requestToken(ctx context.Context, refresh string) (*tokenResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refresh,
		"client_id":     ClientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-profile-manager")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tok tokenResponse
	_ = json.Unmarshal(raw, &tok)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &RateLimitError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		tok.Error == "invalid_grant":
		return nil, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("renewing login: server returned %s", resp.Status)
	case tok.AccessToken == "":
		return nil, errors.New("renewing login: response had no access token")
	}
	return &tok, nil
}
