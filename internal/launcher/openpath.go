package launcher

import (
	"os/exec"
	"runtime"
)

// OpenPath reveals a folder or file in the OS file manager.
func OpenPath(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	// explorer.exe returns exit code 1 even on success, so only report
	// failures to start.
	return startDetached(cmd)
}
