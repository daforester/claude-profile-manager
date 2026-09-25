package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStoreLifecycle(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	work := s.New("Work Account")
	if err := s.Save(work); err != nil {
		t.Fatal(err)
	}
	personal := s.New("personal")
	if err := s.Save(personal); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(s.New("WORK ACCOUNT")); err == nil {
		t.Error("duplicate names (case-insensitive) must be rejected")
	}
	if err := s.Save(s.New("  ")); err == nil {
		t.Error("blank names must be rejected")
	}

	// Reload from disk.
	s2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	list := s2.List()
	if len(list) != 2 || list[0].Name != "personal" || list[1].Name != "Work Account" {
		t.Fatalf("unexpected list after reload: %+v", list)
	}
	for _, key := range []string{work.ID, "work account", "work-account"} {
		got, err := s2.Get(key)
		if err != nil || got.ID != work.ID {
			t.Errorf("Get(%q) = %v, %v", key, got, err)
		}
	}

	// Default directories live under the root and are distinct per profile.
	w, _ := s2.Get(work.ID)
	if filepath.Dir(filepath.Dir(w.ConfigDir())) != filepath.Join(root, "profiles") {
		t.Errorf("unexpected config dir %s", w.ConfigDir())
	}
	p, _ := s2.Get(personal.ID)
	if w.ConfigDir() == p.ConfigDir() || w.DesktopDir() == p.DesktopDir() {
		t.Error("profiles must not share directories")
	}

	// Purging deletes only the managed directory.
	if err := w.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := s2.Delete(w.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir()); !os.IsNotExist(err) {
		t.Errorf("profile dir should be gone, stat err = %v", err)
	}
	if _, err := s2.Get(w.ID); err == nil {
		t.Error("deleted profile still found")
	}
}

func TestDeleteNeverTouchesCustomDirs(t *testing.T) {
	root := t.TempDir()
	custom := t.TempDir()
	s, _ := Open(root)
	p := s.New("custom")
	p.ClaudeConfigDir = custom
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(p.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(custom); err != nil {
		t.Errorf("custom config dir was removed: %v", err)
	}
}

func TestEnvParsing(t *testing.T) {
	env, err := ParseEnv("# comment\nFOO=bar\n\n BAZ = a=b \n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"FOO": "bar", "BAZ": " a=b"}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("ParseEnv = %#v, want %#v", env, want)
	}
	if _, err := ParseEnv("NOEQUALS"); err == nil {
		t.Error("expected error")
	}
	if FormatEnv(map[string]string{"B": "2", "A": "1"}) != "A=1\nB=2" {
		t.Error("FormatEnv should sort keys")
	}
}

func TestReadAccountAndCopy(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root)
	p := s.New("acct")
	_ = s.Save(p)
	if p.ReadAccount().SignedIn() {
		t.Error("fresh profile should not be signed in")
	}
	_ = p.EnsureDirs()
	cfg := `{"oauthAccount":{"emailAddress":"me@example.com","organizationName":"Acme"}}`
	if err := os.WriteFile(filepath.Join(p.ConfigDir(), ".claude.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	a := p.ReadAccount()
	if a.Email != "me@example.com" || a.Summary() != "me@example.com · Acme" {
		t.Errorf("unexpected account %+v", a)
	}

	// CopyConfigFrom copies customisations but never credentials.
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{}`), 0o600)
	_ = os.WriteFile(filepath.Join(src, ".credentials.json"), []byte(`secret`), 0o600)
	_ = os.MkdirAll(filepath.Join(src, "commands", "sub"), 0o700)
	_ = os.WriteFile(filepath.Join(src, "commands", "sub", "x.md"), []byte(`x`), 0o600)
	q := s.New("copy")
	_ = s.Save(q)
	copied, err := q.CopyConfigFrom(src)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(copied, []string{"settings.json", "commands"}) {
		t.Errorf("copied = %v", copied)
	}
	if _, err := os.Stat(filepath.Join(q.ConfigDir(), ".credentials.json")); !os.IsNotExist(err) {
		t.Error("credentials must never be copied")
	}
	if _, err := os.Stat(filepath.Join(q.ConfigDir(), "commands", "sub", "x.md")); err != nil {
		t.Error("nested command not copied")
	}
}

func TestValidate(t *testing.T) {
	p := &Profile{Name: "x", Color: "red"}
	if p.Validate() == nil {
		t.Error("bad colour accepted")
	}
	p = &Profile{Name: "x", ExtraArgs: `"oops`}
	if p.Validate() == nil {
		t.Error("bad args accepted")
	}
	p = &Profile{Name: "x", WorkingDir: "/definitely/not/here"}
	if p.Validate() == nil {
		t.Error("missing working dir accepted")
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{"Work Account": "work-account", "  Acme / Client #2 ": "acme-client-2", "ÄÖ": ""} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
