package shellwords

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := map[string][]string{
		"":                          nil,
		"--model opus":              {"--model", "opus"},
		`--append "hello world"`:    {"--append", "hello world"},
		`--x 'it''s'`:               {"--x", "its"},
		`C:\Users\me\proj --flag`:   {`C:\Users\me\proj`, "--flag"},
		`"C:\Program Files\x" y`:    {`C:\Program Files\x`, "y"},
		`a\ b`:                      {"a b"},
		`say \"hi\"`:                {"say", `"hi"`},
		"  spaced\targs \n":         {"spaced", "args"},
		`--settings '{"a": "b c"}'`: {"--settings", `{"a": "b c"}`},
		`--empty ""`:                {"--empty", ""},
	}
	for in, want := range cases {
		got, err := Split(in)
		if err != nil {
			t.Fatalf("Split(%q): %v", in, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Split(%q) = %#v, want %#v", in, got, want)
		}
	}
	if _, err := Split(`"unterminated`); err == nil {
		t.Error("expected error for unterminated quote")
	}
}

func TestJoinRoundTrip(t *testing.T) {
	in := []string{"--append", "hello world", `C:\x y\z`, `say "hi"`, ""}
	got, err := Split(Join(in))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Errorf("round trip = %#v, want %#v", got, in)
	}
}

func TestQuotePOSIX(t *testing.T) {
	for in, want := range map[string]string{
		"plain":      "plain",
		"/a/b-c.d":   "/a/b-c.d",
		"with space": "'with space'",
		"it's":       `'it'\''s'`,
		"":           "''",
	} {
		if got := QuotePOSIX(in); got != want {
			t.Errorf("QuotePOSIX(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestQuoteCmd(t *testing.T) {
	for in, want := range map[string]string{
		`C:\x\claude.exe`:        `C:\x\claude.exe`,
		`C:\Program Files\c.exe`: `"C:\Program Files\c.exe"`,
		"100%":                   "100%%",
		"a&b":                    `"a&b"`,
	} {
		if got := QuoteCmd(in); got != want {
			t.Errorf("QuoteCmd(%q) = %s, want %s", in, got, want)
		}
	}
}
