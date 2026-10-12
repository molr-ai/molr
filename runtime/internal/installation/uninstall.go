package installation

import (
	"encoding/json"
	"fmt"
	"molr.ai/runtime/internal/installrecord"
	"os"
	"path/filepath"
	"reflect"
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
func (e *Engine) uninstall(r *installrecord.Record) error {
	if r == nil {
		return fmt.Errorf("installation record required")
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if r.OwnerUID != os.Geteuid() {
		return fmt.Errorf("installation owner required")
	}
	e.UI.step(1, "Inspect uninstall plan")
	choices := map[string]*permissionChoice{}
	files := []string{r.Paths.Unit}
	for _, name := range programFiles {
		files = append(files, filepath.Join(r.Paths.Program, name))
	}
	if e.Options.Purge {
		files = append(files, filepath.Join(r.Paths.Config, "server.json"), filepath.Join(r.Paths.Config, "server.previous.json"))
	}
	for _, p := range append(files, r.File()) {
		if err := checkManaged(p, r.OwnerUID, choices); err != nil {
			return err
		}
	}
	trees := []string{r.Paths.Cache}
	if e.Options.Purge && r.ManagedData {
		trees = append(trees, r.Paths.Data)
	}
	protected := []string{r.DataDir}
	data, err := os.ReadFile(filepath.Join(r.Paths.Config, "server.json"))
	if err == nil {
		var cfg map[string]any
		if err = json.Unmarshal(data, &cfg); err != nil {
			return err
		}
		if p, ok := cfg["data_dir"].(string); ok {
			protected = append(protected, p)
		}
		if s, ok := cfg["sandbox"].(map[string]any); ok {
			if p, ok := s["work_root"].(string); ok && p != "" {
				protected = append(protected, p)
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = separate(r.Paths.Program, r.Paths.Config, r.Paths.Data, r.Paths.Cache, r.Paths.Unit); err != nil {
		return err
	}
	for _, p := range protected {
		if err = checkPath(p); err != nil {
			return err
		}
		for _, tree := range append([]string{r.Paths.Program, r.Paths.Config, r.Paths.Cache}, trees...) {
			if e.Options.Purge && r.ManagedData && p == r.Paths.Data && tree == p {
				continue
			}
			if overlaps(p, tree) {
				return fmt.Errorf("uninstall would affect data/workspace path: %s", p)
			}
		}
	}
	for _, p := range trees {
		if _, err = os.Lstat(p); os.IsNotExist(err) {
			continue
		}
		if err = checkTree(p, r.UID); err != nil {
			return err
		}
	}
	e.UI.print("Instance: %s\nProgram: %s\nConfiguration: %s\nData: %s\nPurge managed configuration/data: %t", r.Name, r.Paths.Program, r.Paths.Config, r.DataDir, e.Options.Purge)
	if !r.ManagedData {
		e.UI.print("Custom data directory will be preserved: %s", r.DataDir)
	}
	if e.Options.DryRun {
		e.UI.print("Dry run complete: no changes.")
		return nil
	}
	if err = e.resolvePermissions(choices); err != nil {
		return err
	}
	if e.Options.Purge && !e.Options.Yes {
		if !e.UI.interactive() {
			return fmt.Errorf("non-interactive purge requires --yes")
		}
		answer, err := e.UI.answer("Type instance name to confirm purge: ")
		if err != nil {
			return err
		}
		if answer != r.Name {
			return fmt.Errorf("uninstall cancelled")
		}
	} else {
		ok, err := e.UI.confirm("Uninstall this instance?")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("uninstall cancelled")
		}
	}
	if err = e.acquire(r); err != nil {
		return err
	}
	current, err := readRecord(r.File(), true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(r, current) {
		return fmt.Errorf("installation changed since planning")
	}
	state, err := e.unitState(r)
	if err != nil {
		return err
	}
	for _, c := range sortedChoices(choices) {
		if err = applyPermission(c); err != nil {
			return err
		}
	}
	e.UI.step(2, "Stop and unregister service")
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
		if err = e.service(r, "disable", r.Name+".service"); err != nil {
			return err
		}
	}
	if err = removeFile(r.Paths.Unit); err != nil {
		return err
	}
	if err = e.service(r, "daemon-reload"); err != nil {
		return err
	}
	e.UI.step(3, "Remove managed files")
	for _, p := range files {
		if err = removeFile(p); err != nil {
			return err
		}
	}
	for _, p := range trees {
		if _, err = os.Lstat(p); os.IsNotExist(err) {
			continue
		}
		if err = removeTree(p); err != nil {
			return err
		}
	}
	if e.Options.Purge {
		if err = removeFile(r.File()); err != nil {
			return err
		}
	} else {
		r.State = "uninstalled"
		r.Health = ""
		if err = writeRecord(r); err != nil {
			return err
		}
	}
	e.UI.step(5, "Uninstall complete")
	if !e.Options.Purge {
		e.UI.print("Configuration, data and installation record retained. Reinstall from a package to reuse them.")
	}
	e.UI.print("Unknown files and the stable .setup.lock file are preserved.")
	return nil
}
