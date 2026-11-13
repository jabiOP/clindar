package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const label = "com.github.jabiop.clindar"

// Unit is a service definition for the current OS.
type Unit struct {
	Path    string // where the file goes
	Content string
	Enable  string // command the user runs to start it
	Disable string // command to stop it before removing the file
}

// ForThisOS returns the systemd user unit (Linux) or launchd agent (macOS)
// that runs `exe daemon`.
func ForThisOS(exe, home string) (Unit, error) {
	switch runtime.GOOS {
	case "linux":
		return SystemdUnit(exe, home), nil
	case "darwin":
		return LaunchdAgent(exe, home), nil
	}
	return Unit{}, fmt.Errorf("daemon install is not supported on %s; run `clindar daemon` yourself", runtime.GOOS)
}

func SystemdUnit(exe, home string) Unit {
	return Unit{
		Path: filepath.Join(home, ".config", "systemd", "user", "clindar.service"),
		Content: fmt.Sprintf(`[Unit]
Description=clindar Google Calendar sync
After=network-online.target

[Service]
ExecStart=%s daemon
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
`, exe),
		Enable:  "systemctl --user daemon-reload && systemctl --user enable --now clindar.service",
		Disable: "systemctl --user disable --now clindar.service",
	}
}

func LaunchdAgent(exe, home string) Unit {
	path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	logFile := filepath.Join(home, "Library", "Logs", "clindar.log")
	return Unit{
		Path: path,
		Content: fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key><array><string>%s</string><string>daemon</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, label, exe, logFile),
		Enable:  "launchctl load -w " + path,
		Disable: "launchctl unload -w " + path,
	}
}

// Write creates the unit file (and its directory).
func (u Unit) Write() error {
	if err := os.MkdirAll(filepath.Dir(u.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(u.Path, []byte(u.Content), 0o644)
}
