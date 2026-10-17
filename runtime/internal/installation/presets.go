//go:build linux || darwin

package installation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"molr.ai/runtime/internal/config"
	"molr.ai/runtime/internal/installrecord"
)

// Reapplying a preset changes resource policy, never service paths, identities,
// credentials or existing assistant grants. It is an explicit maintenance action.
func (e *Engine) configureResources(r *installrecord.Record) (resultErr error) {
	if r == nil || r.State != "installed" {
		return fmt.Errorf("installed instance required")
	}
	name := e.Options.Preset
	if name == "" {
		answer, err := e.UI.answer("Apply [p] permissive or [l] least-privilege resource preset? ")
		if err != nil {
			return err
		}
		switch answer {
		case "p":
			name = "permissive"
		case "l":
			name = "least-privilege"
		default:
			return fmt.Errorf("choose a resource preset")
		}
	}
	preset, err := config.SandboxPreset(name)
	if err != nil {
		return err
	}
	cfg, err := desktopConfig(r)
	if err != nil {
		return err
	}
	before := jsonBytes(cfg)
	settings, err := object(cfg, "sandbox")
	if err != nil {
		return err
	}
	values := map[string]any{}
	if err = json.Unmarshal(jsonBytes(preset), &values); err != nil {
		return err
	}
	e.UI.print("Resource preset: %s\nExisting assistant grants are retained. Resource availability changes after restart.", name)
	for _, key := range []string{"enabled", "host_tools", "allow_full_access", "restrict_directories", "directories", "network_presets", "networks", "gpu", "authorization_ceilings"} {
		e.UI.print("sandbox.%s: %s -> %s", key, bytes.TrimSpace(jsonBytes(settings[key])), bytes.TrimSpace(jsonBytes(values[key])))
		settings[key] = values[key]
	}
	if _, err = config.Load(bytes.NewReader(jsonBytes(cfg))); err != nil {
		return err
	}
	if e.Options.DryRun {
		return nil
	}
	ok, err := e.UI.confirm("Apply these resource settings and restart this instance?")
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("configuration unchanged")
	}
	if err = e.acquire(r); err != nil {
		return err
	}
	current, err := desktopConfig(r)
	if err != nil {
		return err
	}
	if !bytes.Equal(before, jsonBytes(current)) {
		return fmt.Errorf("configuration changed since preview; retry")
	}
	actual, err := readRecord(r.File(), true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, r) {
		return fmt.Errorf("installation changed since preview; retry")
	}
	if err = e.prepareDependencies(preset.Enabled); err != nil {
		return err
	}
	delegate, err := e.checkServiceHost(r, preset.Enabled)
	if err != nil {
		return err
	}
	if preset.Enabled {
		if err = e.preparePolicy(r); err != nil {
			return err
		}
	}
	state, err := e.unitState(r)
	if err != nil {
		return err
	}
	running := state["MainPID"] != "0" && state["MainPID"] != ""
	previous := *r
	backup, files, err := snapshotInstallation(r)
	if err != nil {
		return err
	}
	defer func() {
		var pending *VerificationPending
		if resultErr != nil && !errors.As(resultErr, &pending) {
			if recoveryErr := e.rollback(&previous, files, delegate, running); recoveryErr != nil {
				resultErr = installationFailed(resultErr, recoveryErr)
				return
			}
		}
		_ = os.RemoveAll(backup)
	}()
	configPath := filepath.Join(r.Paths.Config, "server.json")
	mode := os.FileMode(0600)
	if r.System {
		mode = 0640
	}
	if err = copyFile(filepath.Join(r.Paths.Config, "server.previous.json"), configPath, mode, r.OwnerUID, r.GID); err != nil {
		return err
	}
	if running {
		if err = e.stopService(r, state, false); err != nil {
			return err
		}
	}
	if err = atomicWrite(configPath, bytes.NewReader(jsonBytes(cfg)), mode, r.OwnerUID, r.GID); err != nil {
		return err
	}
	r.Health = "not_started"
	if running {
		if err = e.startService(r, delegate, true); err != nil {
			return err
		}
		r.Health = "verification_pending"
		if _, err = e.verify(r, cfg); err != nil {
			_ = writeRecord(r)
			return err
		}
	}
	if err = writeRecord(r); err != nil {
		return err
	}
	e.UI.print("Resource configuration updated: %s\nHealth: %s", configPath, r.Health)
	if r.Health == "verification_pending" {
		return &VerificationPending{}
	}
	return nil
}
