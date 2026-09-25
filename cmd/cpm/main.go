// Command cpm is the console-only companion of Claude Profile Manager. It has
// no GUI dependencies, so it builds without cgo and can run Claude Code
// inside the current terminal (`cpm run work`).
package main

import (
	"os"

	"claude-profile-manager/internal/cli"
)

func main() {
	os.Exit(cli.Main(cli.Env{Prog: "cpm", Stdout: os.Stdout, Stderr: os.Stderr}, os.Args[1:]))
}
