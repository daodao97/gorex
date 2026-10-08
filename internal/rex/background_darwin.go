package rex

import (
	"bytes"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// StartBackground gives macOS services their own login environment. launchd
// creates the process, so an automation tool's command environment never becomes
// the environment of the long-lived service or the terminals it later creates.
func StartBackground(args []string, dir, logName string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	shell := defaultShell
	if u, err := user.Current(); err == nil {
		out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+u.Username, "UserShell").Output()
		if value, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "UserShell: "); err == nil && ok && filepath.IsAbs(value) {
			shell = value
		}
	}
	return startLaunchdBackground(exe, args, dir, logName, shell)
}

func startLaunchdBackground(exe string, args []string, dir, logName, shell string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(dir, logName)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	logf.Close()
	identity := strings.Join(append([]string{dir, exe, shell}, args...), "\x00")
	sum := sha256.Sum256([]byte(identity))
	label := fmt.Sprintf("dev.gorex.background.%x", sum[:12])
	target := fmt.Sprintf("gui/%d", os.Getuid())
	if exec.Command("/bin/launchctl", "print", target).Run() != nil {
		target = fmt.Sprintf("user/%d", os.Getuid())
	}
	service := target + "/" + label
	// An idle job can be started again without replacing or killing a running
	// instance. launchd reads the executable at the same path on each start.
	if exec.Command("/bin/launchctl", "print", service).Run() == nil {
		return exec.Command("/bin/launchctl", "kickstart", service).Run()
	}
	var plist bytes.Buffer
	str := func(s string) {
		plist.WriteString("<string>")
		xml.EscapeText(&plist, []byte(s))
		plist.WriteString("</string>")
	}
	plist.WriteString(`<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Label</key>`)
	str(label)
	plist.WriteString(`<key>ProgramArguments</key><array>`)
	argv := append([]string{shell, "-l", "-c", `exec "$@"`, "gorex-service", exe}, args...)
	if filepath.Base(shell) == "fish" {
		argv = append([]string{shell, "--login", "-c", `exec $argv`, exe}, args...)
	}
	for _, arg := range argv {
		str(arg)
	}
	plist.WriteString(`</array><key>EnvironmentVariables</key><dict><key>GOREX_DIR</key>`)
	str(dir)
	plist.WriteString(`<key>SHELL</key>`)
	str(shell)
	plist.WriteString(`</dict><key>StandardOutPath</key>`)
	str(logPath)
	plist.WriteString(`<key>StandardErrorPath</key>`)
	str(logPath)
	plist.WriteString(`<key>RunAtLoad</key><true/><key>KeepAlive</key><false/><key>AbandonProcessGroup</key><true/></dict></plist>`)
	// This file is loaded on demand, not installed as a login/startup item.
	f, err := os.CreateTemp(dir, ".background-*.plist")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(plist.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	out, err := exec.Command("/bin/launchctl", "bootstrap", target, f.Name()).CombinedOutput()
	if err != nil {
		// Concurrent clients may have registered this same service first.
		if exec.Command("/bin/launchctl", "print", service).Run() == nil {
			return exec.Command("/bin/launchctl", "kickstart", service).Run()
		}
		return fmt.Errorf("starting background service: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
