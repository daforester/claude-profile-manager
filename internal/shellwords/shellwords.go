// Package shellwords splits and quotes command-line arguments.
package shellwords

import (
	"fmt"
	"strings"
	"unicode"
)

// Split breaks a command line into arguments. It honours single and double
// quotes, and a backslash escapes the following character only when that
// character is a quote, a backslash or whitespace — so Windows paths such as
// C:\Users\me survive untouched.
func Split(s string) ([]string, error) {
	var (
		args  []string
		cur   strings.Builder
		inArg bool
		quote rune
	)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r == '\\' && quote != '\'' && i+1 < len(rs) {
			if n := rs[i+1]; n == '"' || n == '\'' || n == '\\' || unicode.IsSpace(n) {
				cur.WriteRune(n)
				inArg = true
				i++
				continue
			}
		}
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inArg = true
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// Join renders args back into a single line that Split parses identically.
func Join(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\n\"'") {
			parts[i] = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a) + `"`
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// QuotePOSIX quotes s for sh/bash/zsh.
func QuotePOSIX(s string) string {
	if s == "" {
		return "''"
	}
	safe := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("-_./:=@%+,", r))
	}) < 0
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// QuoteCmd quotes s for use as an argument inside a Windows .cmd batch file.
func QuoteCmd(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if s == "" || strings.ContainsAny(s, " \t&|<>^(),;=\"") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// QuotePowerShell quotes s as a PowerShell single-quoted literal.
func QuotePowerShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
