package daemon

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const label = "com.github.jabiop.clindar"

// Unit is a service definition for the current OS.
type Unit struct {
	Path    string // where the file goes
	Content string
	Enable  string // command the user runs to start it
	Disable string // command to stop it before removing the file
	Restart string // command that restarts it on the current file (and so the current binary)
}

// ForThisOS returns the systemd user unit (Linux) or launchd agent (macOS)
// that runs `exe daemon`.
// Dirs are the config and data folders the daemon must use: the ones of the
// shell that installed it. A service manager starts it without that shell's
// environment, where it could otherwise pick other folders (an empty database).
type Dirs struct{ Config, Data string }

func ForThisOS(exe, home string, dirs Dirs) (Unit, error) {
	switch runtime.GOOS {
	case "linux":
		return SystemdUnit(exe, home, dirs), nil
	case "darwin":
		return LaunchdAgent(exe, home, dirs), nil
	}
	return Unit{}, fmt.Errorf("daemon install is not supported on %s; run `clindar daemon` yourself", runtime.GOOS)
}

func SystemdUnit(exe, home string, dirs Dirs) Unit {
	return Unit{
		Path: filepath.Join(home, ".config", "systemd", "user", "clindar.service"),
		Content: fmt.Sprintf(`[Unit]
Description=clindar Google Calendar sync
After=network-online.target

[Service]
ExecStart=%s daemon
Environment="CLINDAR_CONFIG_DIR=%s" "CLINDAR_DATA_DIR=%s"
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
`, exe, dirs.Config, dirs.Data),
		Enable:  "systemctl --user daemon-reload && systemctl --user enable --now clindar.service",
		Disable: "systemctl --user disable --now clindar.service",
		Restart: "systemctl --user daemon-reload && systemctl --user restart clindar.service",
	}
}

func LaunchdAgent(exe, home string, dirs Dirs) Unit {
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
	<key>EnvironmentVariables</key><dict>
		<key>CLINDAR_CONFIG_DIR</key><string>%s</string>
		<key>CLINDAR_DATA_DIR</key><string>%s</string>
	</dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, label, exe, xmlText(dirs.Config), xmlText(dirs.Data), logFile),
		Enable:  "launchctl load -w " + path,
		Disable: "launchctl unload -w " + path,
		Restart: "launchctl unload " + path + " 2>/dev/null; launchctl load -w " + path,
	}
}

func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Write creates the unit file (and its directory).
func (u Unit) Write() error {
	if err := os.MkdirAll(filepath.Dir(u.Path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(u.Path, []byte(u.Content), 0o644)
}
