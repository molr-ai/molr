//go:build linux || darwin

package installation

import (
	"encoding/json"
	"errors"
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
	"reflect"
)

// DesktopResult is emitted only to the trusted local parent process. LoginURL is
// ephemeral and must never be persisted in receipts or diagnostic logs.
type DesktopResult struct {
	Type         string   `json:"type"`
	Installed    bool     `json:"installed"`
	State        string   `json:"state"`
	Health       string   `json:"health,omitempty"`
	Version      string   `json:"version,omitempty"`
	URL          string   `json:"url,omitempty"`
	Record       string   `json:"record,omitempty"`
	LogDirectory string   `json:"logDirectory,omitempty"`
	LoginURL     string   `json:"loginURL,omitempty"`
	Missing      []string `json:"missing,omitempty"`
}

func (e *Engine) desktopRecord() (*installrecord.Record, error) {
	r, err := e.locate()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.Manager != "desktop" || r.System || r.Name != e.Options.Name {
		return nil, fmt.Errorf("instance is not owned by Molr Desktop; connect to it as an external Runtime")
	}
	return r, nil
}

func (e *Engine) desktopSnapshot(r *installrecord.Record) (DesktopResult, error) {
	out := DesktopResult{Type: "desktop", State: "missing"}
	if r == nil {
		return out, nil
	}
	out.Installed = r.State != "uninstalled"
	out.State, out.Health, out.Version = r.State, r.Health, r.Build.Version
	out.Record, out.LogDirectory = r.File(), r.Paths.Cache
	if !out.Installed {
		return out, nil
	}
	state, err := e.unitState(r)
	if err != nil {
		return out, err
	}
	out.State = state["ActiveState"]
	if out.State == "" {
		out.State = "inactive"
	}
	cfg, err := desktopConfig(r)
	if err != nil {
		return out, err
	}
	h, _ := cfg["http"].(map[string]any)
	out.URL, _ = h["public_url"].(string)
	return out, nil
}

func desktopConfig(r *installrecord.Record) (map[string]any, error) {
	p := filepath.Join(r.Paths.Config, "server.json")
	if err := checkPath(p); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	err = json.Unmarshal(b, &cfg)
	if err == nil && cfg == nil {
		err = fmt.Errorf("invalid Runtime configuration")
	}
	return cfg, err
}

func (e *Engine) desktopCommand() error {
	action := e.Options.TaskAction
	switch action {
	case "status", "plan", "install", "upgrade", "start", "stop", "restart", "uninstall":
	default:
		return fmt.Errorf("unknown desktop action")
	}
	r, err := e.desktopRecord()
	if err != nil {
		return err
	}
	if action == "plan" || action == "install" || action == "upgrade" {
		pkg, err := e.packageAt(e.Root)
		if err != nil {
			return err
		}
		if action == "plan" {
			p, err := e.plan(pkg, r)
			if err != nil {
				return err
			}
			missing := []string{}
			if p.Sandbox {
				missing = missingDependencies()
			}
			_, err = e.checkServiceHost(p.Record, p.Sandbox)
			if err != nil {
				missing = append(missing, err.Error())
			}
			return json.NewEncoder(e.machineOut).Encode(DesktopResult{Type: "desktop", State: "planned", Missing: missing})
		}
		if action == "install" && r != nil && r.State == "installed" {
			return fmt.Errorf("Runtime is already installed")
		}
		if action == "upgrade" && (r == nil || r.State != "installed") {
			return fmt.Errorf("installed Runtime required")
		}
		if err = e.install(pkg, r); err != nil {
			var pending *VerificationPending
			if !errors.As(err, &pending) {
				return err
			}
		}
		r, err = e.desktopRecord()
		if err != nil {
			return err
		}
	} else if action != "status" {
		if r == nil || r.State != "installed" {
			return fmt.Errorf("installed Runtime required")
		}
		if action == "uninstall" {
			if err = e.uninstall(r); err != nil {
				return err
			}
		} else {
			if err = e.acquire(r); err != nil {
				return err
			}
			current, err := e.desktopRecord()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(current, r) {
				return fmt.Errorf("installation changed since inspection; retry")
			}
			state, err := e.unitState(r)
			if err != nil {
				return err
			}
			if action == "stop" || action == "restart" {
				if err = e.stopService(r, state, false); err != nil {
					return err
				}
				r.Health = "not_started"
			}
			if action == "start" || action == "restart" {
				cfg, err := desktopConfig(r)
				if err != nil {
					return err
				}
				sandbox := true
				if s, ok := cfg["sandbox"].(map[string]any); ok {
					if b, ok := s["enabled"].(bool); ok {
						sandbox = b
					}
				}
				delegate, err := e.checkServiceHost(r, sandbox)
				if err != nil {
					return err
				}
				// Starting an already-running instance must not restart its jobs.
				if action == "restart" || state["MainPID"] == "0" || state["MainPID"] == "" {
					if err = e.startService(r, delegate, true); err != nil {
						return err
					}
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
		}
	}
	out, err := e.desktopSnapshot(r)
	if err != nil {
		return err
	}
	out.LoginURL = e.desktopLogin
	return json.NewEncoder(e.machineOut).Encode(out)
}
