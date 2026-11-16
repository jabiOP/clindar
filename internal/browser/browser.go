// Package browser opens URLs in the user's web browser.
package browser

import (
	"os/exec"
	"runtime"
)

// Open tries the platform's opener (xdg-open on Linux, open on macOS).
// Callers should also show the URL, in case it fails.
func Open(url string) error {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "open"
	}
	return exec.Command(cmd, url).Start()
}
