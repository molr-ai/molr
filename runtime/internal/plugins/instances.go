package plugins

import (
	"context"
	"encoding/json"
	"io"
	"path"
	"slices"
	"strings"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/textfiles"
)

// Instances belong to the real account and assistant, independently of the
// administrator's shared package catalog and currently selected UI release.
type Instance struct {
	Credentials  []SecretField `json:",omitempty"`
	Requirements []Requirement `json:",omitempty"`
	OwnerID      core.ID
	PluginID     string
	AssistantID  core.ID
	Release      string
	Digest       string
	Enabled      bool
	Version      core.Version
	Workspace    string
	Environment  string
	Account      string
	Detail       string
}
type InstanceRequest struct {
	ID              string
	AssistantID     core.ID
	Release         string
	Enabled         bool
	ExpectedVersion core.Version
}

func instanceKey(owner, assistant core.ID, id string) string {
	return persistence.Key(owner, Key(string(assistant)+"/"+id))
}
func WorkspacePath(id string) string { return "/work/.local/plugins/" + id }
func (s *Service) instance(ctx context.Context, owner, assistant core.ID, id string) (Instance, error) {
	v, err := persistence.Load[Instance](ctx, s.store, "plugins.instances", instanceKey(owner, assistant, id))
	if persistence.Missing(err) {
		return Instance{OwnerID: owner, PluginID: id, AssistantID: assistant, Workspace: WorkspacePath(id), Environment: "pending", Account: "unknown"}, nil
	}
	v.OwnerID = owner
	return v, err
}
func (s *Service) release(ctx context.Context, v Installation, release string) (Installation, Package, error) {
	if !slices.ContainsFunc(v.Versions, func(r Release) bool { return r.Version == release }) {
		return v, Package{}, core.NewError(core.NotFound, "plugin release not found")
	}
	pkg, err := persistence.Load[storedPackage](ctx, s.store, "plugins.packages", persistence.Key(v.OwnerID, Key(v.ID+"@"+release)))
	if err != nil {
		return v, Package{}, err
	}
	v.Manifest, v.Digest = pkg.Package.Manifest, pkg.Digest
	return v, pkg.Package, nil
}
func (s *Service) checkInstance(ctx context.Context, p core.Principal, id string, assistant core.ID, active bool) (Installation, error) {
	if s.options.CheckAssistant == nil {
		return Installation{}, core.NewError(core.Forbidden, "assistant validation unavailable")
	}
	if err := s.options.CheckAssistant(ctx, p, assistant); err != nil {
		return Installation{}, err
	}
	v, err := s.load(ctx, p.OwnerID, id)
	if err != nil {
		return v, err
	}
	if v.Deleted || (active && !v.Enabled) {
		return v, core.NewError(core.Forbidden, "plugin unavailable")
	}
	if err := s.checkPlugin(ctx, p, id); err != nil {
		return v, err
	}
	if active {
		err = s.available(ctx, p, v)
	}
	return v, err
}
func (s *Service) Instance(ctx context.Context, p core.Principal, id string, assistant core.ID) (Instance, error) {
	if !pluginID.MatchString(id) {
		return Instance{}, invalid("invalid plugin ID")
	}
	if err := s.authorize(ctx, p, false); err != nil {
		return Instance{}, err
	}
	if s.options.CheckAssistant == nil {
		return Instance{}, core.NewError(core.Forbidden, "assistant validation unavailable")
	}
	if err := s.options.CheckAssistant(ctx, p, assistant); err != nil {
		return Instance{}, err
	}
	v, err := s.instance(ctx, p.OwnerID, assistant, id)
	if err != nil {
		return v, err
	}
	if v.Release != "" {
		installed, e := s.checkInstance(ctx, p, id, assistant, false)
		if e == nil {
			selected, _, e := s.release(ctx, installed, v.Release)
			if e != nil {
				return v, e
			}
			v.Credentials = selected.Manifest.Credentials
			v.Requirements = selected.Manifest.Requirements
		}
	}
	if v.Release != "" && s.options.WorkspaceRoot != nil {
		root, e := s.options.WorkspaceRoot(ctx, p, assistant, false)
		if e != nil {
			return v, e
		}
		v.Environment, v.Account, v.Detail = readInstanceStatus(root, v)
	}
	return v, nil
}

func readInstanceStatus(root string, v Instance) (string, string, string) {
	environment, account, detail := "pending", "unknown", ""
	read := func(name string, target any) bool {
		f, err := textfiles.Open(root, name)
		if err != nil {
			return false
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 8193))
		if err != nil || len(b) > 8192 {
			return false
		}
		return json.Unmarshal(b, target) == nil
	}
	var status struct{ Digest, Environment, Detail string }
	if read(".local/plugins/"+v.PluginID+"/versions/"+v.Release+"/status.json", &status) && status.Digest == v.Digest && slices.Contains([]string{"pending", "installing", "ready", "failed"}, status.Environment) {
		environment = status.Environment
		// Free-form process output may contain credentials. Keep it out of the UI.
		if environment == "failed" {
			detail = "Check the assistant's installation task for details."
		}
	}
	var login struct{ Account string }
	if read(".local/plugins/"+v.PluginID+"/config/account.json", &login) && slices.Contains([]string{"unknown", "pending", "ready", "not_required"}, login.Account) {
		account = login.Account
	}
	return environment, account, detail
}

func (s *Service) SaveInstance(ctx context.Context, p core.Principal, q InstanceRequest) (Instance, error) {
	if !pluginID.MatchString(q.ID) {
		return Instance{}, invalid("invalid plugin ID")
	}
	if err := s.authorize(ctx, p, false); err != nil {
		return Instance{}, err
	}
	if s.options.AuthorizeSettings == nil {
		return Instance{}, core.NewError(core.Forbidden, "settings write unavailable")
	}
	if err := s.options.AuthorizeSettings(ctx, p); err != nil {
		return Instance{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !q.Enabled {
		if s.options.CheckAssistant == nil {
			return Instance{}, core.NewError(core.Forbidden, "assistant validation unavailable")
		}
		if err := s.options.CheckAssistant(ctx, p, q.AssistantID); err != nil {
			return Instance{}, err
		}
		old, err := s.instance(ctx, p.OwnerID, q.AssistantID, q.ID)
		if err != nil {
			return old, err
		}
		if old.Version == 0 || old.Version != q.ExpectedVersion {
			return old, core.NewError(core.Conflict, "plugin instance changed")
		}
		old.Enabled = false
		old.Version++
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			if err := tx.Put(ctx, "plugins.instances", instanceKey(p.OwnerID, q.AssistantID, q.ID), old); err != nil {
				return err
			}
			return s.audit(ctx, p, "instance.disable", q.ID)
		})
		return old, err
	}
	v, err := s.checkInstance(ctx, p, q.ID, q.AssistantID, q.Enabled)
	if err != nil {
		return Instance{}, err
	}
	old, err := s.instance(ctx, p.OwnerID, q.AssistantID, q.ID)
	if err != nil {
		return old, err
	}
	if old.Version != q.ExpectedVersion {
		return old, core.NewError(core.Conflict, "plugin instance changed")
	}
	release := q.Release
	if release == "" {
		release = old.Release
	}
	if release == "" {
		release = v.Manifest.Version
	}
	selected, pkg, err := s.release(ctx, v, release)
	if err != nil {
		return old, err
	}
	if len(pkg.Manifest.Contributes.Skills) == 0 {
		return old, invalid("plugin has no assistant skills")
	}
	if q.Enabled {
		if s.options.WorkspaceRoot == nil {
			return old, core.NewError(core.Unavailable, "workspace unavailable")
		}
		root, err := s.options.WorkspaceRoot(ctx, p, q.AssistantID, true)
		if err != nil {
			return old, err
		}
		if err = prepareWorkspace(ctx, root, pkg, selected.Digest, func(size int64) error {
			if s.options.CheckWorkspaceFile != nil {
				if err := s.options.CheckWorkspaceFile(ctx, p, q.AssistantID, size); err != nil {
					return err
				}
			}
			_, err := s.checkInstance(ctx, p, q.ID, q.AssistantID, true)
			return err
		}); err != nil {
			return old, err
		}
	}
	old.Enabled, old.Release, old.Digest = q.Enabled, release, selected.Digest
	old.Version++
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		current, err := s.instance(ctx, p.OwnerID, q.AssistantID, q.ID)
		if err != nil {
			return err
		}
		if current.Version != q.ExpectedVersion {
			return core.NewError(core.Conflict, "plugin instance changed")
		}
		if err = tx.Put(ctx, "plugins.instances", instanceKey(p.OwnerID, q.AssistantID, q.ID), old); err != nil {
			return err
		}
		return s.audit(ctx, p, "instance.update", q.ID)
	})
	return old, err
}

func prepareWorkspace(ctx context.Context, root string, pkg Package, digest string, authorize func(int64) error) error {
	base := ".local/plugins/" + pkg.Manifest.ID
	version := base + "/versions/" + pkg.Manifest.Version
	for _, dir := range []string{base + "/config", base + "/data", base + "/cache", version + "/package"} {
		if err := textfiles.MkdirAll(root, dir); err != nil {
			return err
		}
	}
	files := map[string]string{}
	for name, body := range pkg.Files {
		files[version+"/package/"+name] = body
	}
	files[version+"/PACKAGE.md"] = "# Plugin workspace\n\nPackage: " + pkg.Manifest.ID + "@" + pkg.Manifest.Version + "\nDigest: " + digest + "\n\nUse ordinary file and shell tools. Install the CLI and dependencies in this version directory. Use the plugin root config, data and cache directories via absolute workspace paths. Package files are working copies; original files are readable at /plugins/" + pkg.Manifest.ID + "/" + pkg.Manifest.Version + "/.\n\nIf prepare.sh is present, invoke it directly through ordinary shell_execute; it manages the installation lock and check sequence. Otherwise obtain a directory lock with mkdir ../../install.lock before installing and arrange cleanup with a shell trap. If a previous interrupted task left a lock, inspect shell_task before removing it. Only one installation per assistant/plugin may run at once. Run the package's check instructions before writing status.json.\n\nWrite status.json next to this file with {\"Digest\":\"" + digest + "\",\"Environment\":\"ready\"} only after verification succeeds. Use pending, installing or failed otherwise. Write config/account.json with {\"Account\":\"pending\"}, ready or not_required as appropriate. These files report the assistant's last check, not a live health guarantee. Never put tokens or command output in status files.\n"
	if _, ok := pkg.Files["scripts/install.sh"]; ok {
		if _, ok := pkg.Files["scripts/check.sh"]; ok {
			files[version+"/prepare.sh"] = prepareScript(pkg.Manifest.ID, pkg.Manifest.Version, digest)
		}
	}
	for name, body := range files {
		if f, err := textfiles.Open(root, name); err == nil {
			f.Close()
			continue
		} else if core.CodeOf(err) != core.NotFound {
			return err
		}
		if err := textfiles.MkdirAll(root, path.Dir(name)); err != nil {
			return err
		}
		if _, err := textfiles.Replace(ctx, root, name, body, false, "", func() error { return authorize(int64(len(body))) }); err != nil {
			return err
		}
	}
	return nil
}

// ResolvePackageFile provides an immutable virtual file to file_read. It never
// selects another owner's workspace, and checks the current instance on every read.
func (s *Service) ResolvePackageFile(ctx context.Context, e core.Execution, name string) (string, string, error) {
	if path.Clean(name) != name || strings.ContainsAny(name, "\\\x00") {
		return "", "", invalid("canonical package path required")
	}
	parts := strings.SplitN(strings.TrimPrefix(name, "/plugins/"), "/", 3)
	if len(parts) != 3 || !strings.HasPrefix(name, "/plugins/") {
		return "", "", invalid("plugin file path required")
	}
	v, err := s.checkInstance(ctx, e.Principal, parts[0], e.AssistantID, true)
	if err != nil {
		return "", "", err
	}
	instance, err := s.instance(ctx, e.Principal.OwnerID, e.AssistantID, v.ID)
	if err != nil {
		return "", "", err
	}
	if !instance.Enabled || instance.Release != parts[1] {
		return "", "", core.NewError(core.Forbidden, "plugin instance disabled or changed")
	}
	v, pkg, err := s.release(ctx, v, parts[1])
	if err != nil {
		return "", "", err
	}
	if instance.Digest != v.Digest {
		return "", "", core.NewError(core.Conflict, "plugin digest changed")
	}
	body, ok := pkg.Files[parts[2]]
	if !ok {
		return "", "", core.NewError(core.NotFound, "package file not found")
	}
	return body, v.Digest, nil
}

// This wrapper runs only when the assistant explicitly invokes it through the
// ordinary Shell. It shares the assistant's grants, task receipts and quotas.
func prepareScript(id, version, digest string) string {
	return "#!/bin/sh\nset -eu\ncd '/work/.local/plugins/" + id + "/versions/" + version + "'\n" + `if ! mkdir ../../install.lock 2>/dev/null; then
 echo 'Another installation holds the lock. Inspect existing shell tasks before recovering an interrupted installation.' >&2
 exit 1
fi
cleanup() { rmdir ../../install.lock; }
trap cleanup EXIT
trap 'exit 1' INT TERM HUP
` + "printf '%s\n' '{\"Digest\":\"" + digest + "\",\"Environment\":\"installing\"}' > status.json\n" + `if /bin/sh package/scripts/install.sh && /bin/sh package/scripts/check.sh; then
` + " printf '%s\n' '{\"Digest\":\"" + digest + "\",\"Environment\":\"ready\"}' > status.json\n" + " printf '%s\n' '{\"Release\":\"" + version + "\",\"Digest\":\"" + digest + "\"}' > ../../active.json.tmp\n mv ../../active.json.tmp ../../active.json\n" + `else
` + " printf '%s\n' '{\"Digest\":\"" + digest + "\",\"Environment\":\"failed\"}' > status.json\n exit 1\nfi\n"
}

func (s *Service) ListInstances(ctx context.Context, p core.Principal) ([]Instance, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return nil, err
	}
	rows, err := persistence.Select[Instance](ctx, s.store, "plugins.instances", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID)}})
	if err != nil {
		return nil, err
	}
	out := []Instance{}
	for _, row := range rows {
		if row.OwnerID == p.OwnerID && row.Release != "" {
			out = append(out, row)
		}
	}
	return out, nil
}
