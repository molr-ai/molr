//go:build linux

package installation

import (
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
	"strings"
)

func (e *Engine) unitState(r *installrecord.Record) (map[string]string, error) {
	data, err := e.run(append(control(r), "show", r.Name+".service", "--property=LoadState,ActiveState,MainPID,ControlGroup,FragmentPath"), true)
	state := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			state[k] = v
		}
	}
	if err != nil && state["LoadState"] != "not-found" {
		return nil, err
	}
	if state["LoadState"] == "" {
		return nil, fmt.Errorf("cannot inspect service")
	}
	if p := state["FragmentPath"]; p != "" {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || real != r.Paths.Unit {
			return nil, fmt.Errorf("service belongs to another installation: %s", p)
		}
	}
	return state, nil
}

func (e *Engine) stopService(r *installrecord.Record, state map[string]string, unregister bool) error {
	var err error
	if state["LoadState"] != "not-found" {
		if err = e.service(r, "stop", r.Name+".service"); err != nil {
			return err
		}
		state, err = e.unitState(r)
		if err != nil {
			return err
		}
		if state["MainPID"] != "0" || (state["ActiveState"] != "inactive" && state["ActiveState"] != "failed") {
			return fmt.Errorf("service has not stopped")
		}
		if group := state["ControlGroup"]; group != "" {
			events, err := os.ReadFile(filepath.Join("/sys/fs/cgroup", group, "cgroup.events"))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if strings.Contains(string(events), "populated 1") {
				return fmt.Errorf("service cgroup still contains processes")
			}
		}
		if unregister {
			if err = e.service(r, "disable", r.Name+".service"); err != nil {
				return err
			}
		}
	}
	if !unregister {
		return nil
	}
	if err = removeFile(r.Paths.Unit); err != nil {
		return err
	}
	if err = e.service(r, "daemon-reload"); err != nil {
		return err
	}
	return nil
}
func (e *Engine) startService(r *installrecord.Record, delegate bool, start bool) error {
	if err := atomicWrite(r.Paths.Unit, strings.NewReader(renderUnit(r, delegate)), 0644, r.OwnerUID, r.GID); err != nil {
		return err
	}
	if err := e.service(r, "daemon-reload"); err != nil {
		return err
	}
	if !start {
		return nil
	}
	if err := e.service(r, "enable", r.Paths.Unit); err != nil {
		return err
	}
	return e.service(r, "restart", r.Name+".service")
}
func (e *Engine) serviceInstructions(r *installrecord.Record) {
	e.UI.print("Service status: %s status %s.service\nService logs: journalctl %s-u %s.service -n 50 --no-pager", shellJoin(control(r)), r.Name, map[bool]string{false: "--user "}[r.System], r.Name)
	if !r.System {
		e.UI.print("For operation after logout: sudo loginctl enable-linger %s", r.User)
	}
	if e.Options.NoStart {
		e.UI.print("Start after initialization: %s enable --now %s", shellJoin(control(r)), shellJoin([]string{r.Paths.Unit}))
	}
}
