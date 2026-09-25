package profile

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Account is what we can learn about a profile's Claude Code login without
// touching its credentials.
type Account struct {
	Email        string
	DisplayName  string
	Organization string
	// HasCredentials reports whether a credential file exists. On macOS
	// Claude Code keeps the token in the Keychain instead, so this is only
	// meaningful on Windows and Linux.
	HasCredentials bool
	// DesktopInitialised reports whether Claude Desktop has created data
	// in this profile's user-data directory (i.e. it has been opened).
	DesktopInitialised bool
}

// SignedIn reports whether the profile appears to be logged in to Claude Code.
func (a Account) SignedIn() bool { return a.Email != "" || a.HasCredentials }

// Summary is a short human description for list rows.
func (a Account) Summary() string {
	switch {
	case a.Email != "" && a.Organization != "":
		return a.Email + " · " + a.Organization
	case a.Email != "":
		return a.Email
	case a.HasCredentials:
		return "Signed in"
	default:
		return "Not signed in yet"
	}
}

// ReadAccount inspects the profile's Claude Code state. Claude Code writes
// .claude.json inside CLAUDE_CONFIG_DIR when that variable is set; the
// oauthAccount block there names the logged-in account.
func (p *Profile) ReadAccount() Account {
	var a Account
	dir := p.ConfigDir()
	if data, err := os.ReadFile(filepath.Join(dir, ".claude.json")); err == nil {
		var cfg struct {
			OAuthAccount struct {
				EmailAddress     string `json:"emailAddress"`
				DisplayName      string `json:"displayName"`
				OrganizationName string `json:"organizationName"`
			} `json:"oauthAccount"`
		}
		if json.Unmarshal(data, &cfg) == nil {
			a.Email = cfg.OAuthAccount.EmailAddress
			a.DisplayName = cfg.OAuthAccount.DisplayName
			a.Organization = cfg.OAuthAccount.OrganizationName
		}
	}
	if runtime.GOOS != "darwin" {
		if _, err := os.Stat(filepath.Join(dir, ".credentials.json")); err == nil {
			a.HasCredentials = true
		}
	}
	if entries, err := os.ReadDir(p.DesktopDir()); err == nil && len(entries) > 0 {
		a.DesktopInitialised = true
	}
	return a
}

// SharedConfigItems are the parts of a Claude Code config directory that
// carry customisation rather than identity, and are safe to copy between
// profiles. Credentials, history, projects and .claude.json are excluded.
var SharedConfigItems = []string{
	"settings.json", "CLAUDE.md", "agents", "commands", "skills", "output-styles", "hooks", "plugins",
}

// CopyConfigFrom copies SharedConfigItems from src (e.g. ~/.claude) into the
// profile's config directory, skipping anything that already exists there.
// It returns the names that were copied.
func (p *Profile) CopyConfigFrom(src string) ([]string, error) {
	dst := p.ConfigDir()
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return nil, err
	}
	var copied []string
	for _, name := range SharedConfigItems {
		from := filepath.Join(src, name)
		to := filepath.Join(dst, name)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		if _, err := os.Stat(to); err == nil {
			continue
		}
		if err := copyTree(from, to); err != nil {
			return copied, err
		}
		copied = append(copied, name)
	}
	return copied, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type()&fs.ModeSymlink != 0:
			return nil // skip links; they usually point at machine-specific paths
		default:
			return copyFile(path, target)
		}
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
