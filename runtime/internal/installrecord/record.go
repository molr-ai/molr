// Package installrecord defines the one authoritative installation receipt.
package installrecord

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Build struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}
type Paths struct {
	Program string `json:"program"`
	Config  string `json:"config"`
	Data    string `json:"data"`
	Cache   string `json:"cache"`
	Unit    string `json:"unit"`
}
type Updates struct {
	Source  string `json:"source"`
	Channel string `json:"channel"`
}
type Record struct {
	Target      string  `json:"target,omitempty"`
	Manager     string  `json:"manager,omitempty"`
	Home        string  `json:"home,omitempty"`
	Schema      int     `json:"schema"`
	State       string  `json:"state"`
	Health      string  `json:"health,omitempty"`
	Name        string  `json:"name"`
	User        string  `json:"user"`
	UID         int     `json:"uid"`
	GID         int     `json:"gid"`
	OwnerUID    int     `json:"owner_uid"`
	System      bool    `json:"system"`
	ProgramRoot string  `json:"program_root"`
	Paths       Paths   `json:"paths"`
	DataDir     string  `json:"data_dir"`
	ManagedData bool    `json:"managed_data"`
	Build       Build   `json:"build"`
	Updates     Updates `json:"updates"`
}

var namePattern = regexp.MustCompile(`^molr(?:-[a-z0-9][a-z0-9-]{0,47})?$`)

func Instance(name string) (string, error) {
	if !namePattern.MatchString(name) || name == "molr-default" {
		return "", fmt.Errorf("service name must be molr or molr-<name>; molr-default is reserved")
	}
	if name == "molr" {
		return "default", nil
	}
	return strings.TrimPrefix(name, "molr-"), nil
}
func (r Record) Binary() string {
	if r.Target == "sandbox" {
		return "molr-sandbox"
	}
	return "molr"
}
func (r Record) ServiceArgs() []string {
	args := []string{filepath.Join(r.Paths.Program, "bin", r.Binary())}
	if r.Target != "sandbox" {
		args = append(args, "serve")
	}
	args = append(args, "-config", filepath.Join(r.Paths.Config, "server.json"))
	if r.Target == "sandbox" {
		args = append(args, "-token-file", filepath.Join(r.DataDir, "sandbox-token"))
	}
	return args
}
func (r Record) File() string { return filepath.Join(r.Paths.Program, "installation.json") }
func (r Record) UpdateSource() string {
	if r.Updates.Source == "" {
		return ""
	}
	return strings.TrimRight(r.Updates.Source, "/") + "/" + r.Updates.Channel
}
func (r Record) Validate() error {
	if r.Target != "" && r.Target != "runtime" && r.Target != "sandbox" {
		return fmt.Errorf("invalid installation target")
	}
	if r.Target == "sandbox" && r.Manager == "desktop" {
		return fmt.Errorf("desktop manages Runtime instances")
	}
	if r.Manager != "" && r.Manager != "desktop" {
		return fmt.Errorf("unknown installation manager")
	}
	instance, err := Instance(r.Name)
	if err != nil {
		return err
	}
	if r.Schema != 1 || r.UID <= 0 || r.GID < 0 || r.OwnerUID < 0 || r.User == "" {
		return fmt.Errorf("invalid installation identity")
	}
	if r.State != "installing" && r.State != "installed" && r.State != "uninstalled" {
		return fmt.Errorf("invalid installation state")
	}
	if (r.System && r.OwnerUID != 0) || (!r.System && r.OwnerUID != r.UID) {
		return fmt.Errorf("inconsistent installation owner")
	}
	for _, p := range []string{r.ProgramRoot, r.Paths.Program, r.Paths.Config, r.Paths.Data, r.Paths.Cache, r.Paths.Unit, r.DataDir} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || p == "/" || strings.ContainsAny(p, "\x00\r\n") {
			return fmt.Errorf("invalid installation path")
		}
	}
	if r.Paths.Program != filepath.Join(r.ProgramRoot, instance) || filepath.Base(r.Paths.Config) != instance || filepath.Base(r.Paths.Data) != instance || filepath.Base(r.Paths.Cache) != instance {
		return fmt.Errorf("installation paths do not match instance")
	}
	if r.ManagedData && r.DataDir != r.Paths.Data {
		return fmt.Errorf("custom data cannot be marked managed")
	}
	if r.Updates.Channel != "stable" && r.Updates.Channel != "preview" && r.Updates.Channel != "nightly" {
		return fmt.Errorf("invalid update channel")
	}
	if r.Build.OS == "darwin" {
		if r.System || !filepath.IsAbs(r.Home) || filepath.Clean(r.Home) != r.Home || r.Home == "/" || r.Paths.Unit != filepath.Join(r.Home, "Library", "LaunchAgents", r.Name+".plist") {
			return fmt.Errorf("invalid macOS user installation paths")
		}
	} else {
		if filepath.Base(r.Paths.Unit) != r.Name+".service" {
			return fmt.Errorf("invalid service filename")
		}
		if r.System && (r.Paths.Config != filepath.Join("/etc/molr", instance) || r.Paths.Data != filepath.Join("/var/lib/molr", instance) || r.Paths.Cache != filepath.Join("/var/cache/molr", instance) || r.Paths.Unit != filepath.Join("/etc/systemd/system", r.Name+".service")) {
			return fmt.Errorf("invalid system installation paths")
		}
		if !r.System && r.Paths.Unit != filepath.Join(filepath.Dir(filepath.Dir(r.Paths.Config)), "systemd/user", r.Name+".service") {
			return fmt.Errorf("invalid user unit path")
		}
	}
	return nil
}
func Read(path string) (*Record, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 || info.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("installation record must be a protected regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Record
	if err = json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if err = r.Validate(); err != nil {
		return nil, err
	}
	if filepath.Clean(path) != r.File() {
		return nil, fmt.Errorf("installation record location mismatch")
	}
	return &r, nil
}
