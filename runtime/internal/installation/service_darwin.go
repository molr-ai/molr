package installation

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func launchLabel(r *installrecord.Record) string { return "ai.molr." + r.Name }
func (e *Engine) launchDomain(r *installrecord.Record) string {
	gui := fmt.Sprintf("gui/%d", r.UID)
	if _, err := e.run([]string{"/bin/launchctl", "print", gui}, true); err == nil {
		return gui
	}
	return fmt.Sprintf("user/%d", r.UID)
}
func (e *Engine) unitState(r *installrecord.Record) (map[string]string, error) {
	state := map[string]string{"LoadState": "not-found", "ActiveState": "inactive", "MainPID": "0"}
	for _, kind := range []string{"gui", "user"} {
		domain := fmt.Sprintf("%s/%d", kind, r.UID)
		data, err := e.run([]string{"/bin/launchctl", "print", domain + "/" + launchLabel(r)}, true)
		if err != nil {
			detail := err.Error()
			if strings.Contains(detail, "Could not find service") || strings.Contains(detail, "Could not find specified service") || strings.Contains(detail, "Could not find domain") || strings.Contains(detail, "Domain does not support specified action") {
				continue
			}
			return nil, err
		}
		if state["LoadState"] != "not-found" {
			return nil, fmt.Errorf("duplicate launchd registrations; stop the duplicate instance before maintenance")
		}
		state["LoadState"] = "loaded"
		state["Domain"] = domain
		for _, line := range strings.Split(string(data), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), " = ")
			if !ok {
				continue
			}
			switch k {
			case "path":
				state["FragmentPath"] = v
			case "state":
				state["ActiveState"] = v
			case "pid":
				state["MainPID"] = v
			}
		}
		if state["FragmentPath"] != r.Paths.Unit {
			return nil, fmt.Errorf("launchd service belongs to another installation")
		}
	}
	return state, nil
}
func (e *Engine) stopService(r *installrecord.Record, state map[string]string, unregister bool) error {
	if state["LoadState"] != "not-found" {
		if _, err := e.run([]string{"/bin/launchctl", "bootout", state["Domain"] + "/" + launchLabel(r)}, true); err != nil {
			return err
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			s, err := e.unitState(r)
			if err != nil {
				return err
			}
			if s["LoadState"] == "not-found" {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("launchd service has not stopped")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if unregister {
		return removeFile(r.Paths.Unit)
	}
	return nil
}
func plistString(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return "<string>" + b.String() + "</string>"
}
func renderLaunchAgent(r *installrecord.Record) string {
	args := ""
	for _, s := range r.ServiceArgs() {
		args += plistString(s)
	}
	// Both session types support a desktop login or an SSH-only user domain.
	return `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict>` +
		"<key>Label</key>" + plistString(launchLabel(r)) + "<key>ProgramArguments</key><array>" + args + "</array>" +
		"<key>LimitLoadToSessionType</key><array><string>Aqua</string><string>Background</string></array>" +
		"<key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict><key>ThrottleInterval</key><integer>10</integer><key>ExitTimeOut</key><integer>30</integer><key>Umask</key><integer>63</integer>" +
		"<key>WorkingDirectory</key>" + plistString(r.Paths.Program) + "<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string><key>TMPDIR</key>" + plistString(filepath.Join(r.Paths.Cache, "tmp")) + "<key>XDG_CACHE_HOME</key>" + plistString(r.Paths.Cache) + "</dict>" +
		"<key>StandardOutPath</key>" + plistString(filepath.Join(r.Paths.Cache, "stdout.log")) + "<key>StandardErrorPath</key>" + plistString(filepath.Join(r.Paths.Cache, "stderr.log")) + "</dict></plist>"
}
func (e *Engine) startService(r *installrecord.Record, _ bool, start bool) error {
	if err := atomicWrite(r.Paths.Unit, strings.NewReader(renderLaunchAgent(r)), 0644, r.OwnerUID, r.GID); err != nil {
		return err
	}
	if !start {
		return nil
	}
	domain := e.launchDomain(r)
	if _, err := e.run([]string{"/bin/launchctl", "enable", domain + "/" + launchLabel(r)}, true); err != nil {
		return err
	}
	_, err := e.run([]string{"/bin/launchctl", "bootstrap", domain, r.Paths.Unit}, true)
	return err
}
func (e *Engine) serviceInstructions(r *installrecord.Record) {
	e.UI.print("Service: launchctl print %s/%s\nLogs: %s\nDesktop login startup: %s", e.launchDomain(r), launchLabel(r), filepath.Join(r.Paths.Cache, "stderr.log"), r.Paths.Unit)
	if e.Options.NoStart {
		e.UI.print("Start: launchctl bootstrap %s %s", e.launchDomain(r), strconv.Quote(r.Paths.Unit))
	}
}
