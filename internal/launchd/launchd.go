package launchd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	Label = "dev.brew-updater"
)

func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func LogsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "brew-updater.log"), nil
}

func Install(binaryPath, configPath string, startNow bool) (string, error) {
	plistPath, err := PlistPath()
	if err != nil {
		return "", err
	}
	logPath, err := LogsPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return "", err
	}

	plist := renderPlist(binaryPath, configPath, logPath, startNow)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return "", err
	}

	if err := bootstrap(plistPath); err != nil {
		return "", err
	}
	return plistPath, nil
}

func Uninstall() error {
	plistPath, err := PlistPath()
	if err != nil {
		return err
	}
	_ = bootout(plistPath)
	return os.Remove(plistPath)
}

func Status() (bool, error) {
	cmd := exec.Command("/bin/launchctl", "list")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), Label), nil
}

func renderPlist(binaryPath, configPath, logPath string, startNow bool) string {
	runAtLoad := ""
	if startNow {
		runAtLoad = "<key>RunAtLoad</key>\n  <true/>"
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>check</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  %s
  <key>StartInterval</key>
  <integer>60</integer>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
  <key>LowPriorityBackgroundIO</key>
  <true/>
  <key>LowPriorityIO</key>
  <true/>
  <key>ProcessType</key>
  <string>Background</string>
</dict>
</plist>
`, Label, binaryPath, configPath, runAtLoad, logPath, logPath)
}

func bootstrap(plistPath string) error {
	uid := strconv.Itoa(os.Getuid())
	cmd := exec.Command("/bin/launchctl", "bootstrap", "gui/"+uid, plistPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// fallback to load
		if err := exec.Command("/bin/launchctl", "load", plistPath).Run(); err != nil {
			return fmt.Errorf("launchctl bootstrap failed: %v: %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	// give launchd a moment
	time.Sleep(200 * time.Millisecond)
	return nil
}

func bootout(plistPath string) error {
	uid := strconv.Itoa(os.Getuid())
	cmd := exec.Command("/bin/launchctl", "bootout", "gui/"+uid, plistPath)
	_ = cmd.Run()
	return nil
}
