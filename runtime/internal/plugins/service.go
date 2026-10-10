package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/credential"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

type Binding struct {
	AssistantID core.ID
	ComputeID   core.ID
	Tools       []string
	Skills      []string
}
type Installation struct {
	CatalogID   core.ID
	ToolIDs     []core.ID
	UserEnabled bool
	UserVersion core.Version
	OwnerID     core.ID
	ID          string
	Manifest    Manifest
	Digest      string
	Version     core.Version
	Enabled     bool
	Grants      []string
	Bindings    []Binding
	Versions    []Release
	Deleted     bool
}
type Release struct {
	Version string
	Digest  string
}
type storedPackage struct {
	OwnerID core.ID
	Package Package
	Digest  string
}
type Configure struct {
	ID              string
	ExpectedVersion core.Version
	Enabled         bool
	Grants          []string
	Bindings        []Binding
	Release         string
}
type Options struct {
	CheckPlugin          func(context.Context, core.Principal, string) error
	CheckTool            func(context.Context, core.Principal, string) error
	AuthorizeSettings    func(context.Context, core.Principal) error
	CheckConnection      func(context.Context, core.Principal, core.ID) error
	CheckConnectionQuota func(context.Context, core.Principal, core.ID) error
	Credentials          credential.Manager
	Broker               credential.Broker
	HTTPClient           func(context.Context, core.Principal, core.Execution, core.ID, string) (*http.Client, error)

	UserBinding      func(context.Context, core.Principal, core.ID, Installation) (Binding, bool, error)
	IsAdministrator  func(context.Context, core.Principal) (bool, error)
	ComputeAvailable func(context.Context, core.Principal, core.ID) (bool, error)
	Authorize        func(context.Context, core.Principal, bool) error
	CheckAssistant   func(context.Context, core.Principal, core.ID) error
	CheckCompute     func(context.Context, core.Principal, core.ID) error
	Shell            func(context.Context, core.Execution, string, core.ID) (json.RawMessage, error)
	ReadPlans        func(context.Context, core.Principal) (any, error)
}
type Service struct {
	store           persistence.Store
	options         Options
	mu              sync.Mutex
	running         map[string]map[string]context.CancelFunc
	shellPrincipals map[string]core.Principal
	httpRunning     map[string]httpRun
}

func New(store persistence.Store, options Options) *Service {
	return &Service{store: store, options: options, running: map[string]map[string]context.CancelFunc{}, httpRunning: map[string]httpRun{}, shellPrincipals: map[string]core.Principal{}}
}
func (s *Service) authorize(ctx context.Context, p core.Principal, write bool) error {
	if err := p.RequireOwner(); err != nil {
		return err
	}
	if s.options.Authorize == nil {
		return core.NewError(core.Forbidden, "plugin authorization unavailable")
	}
	return s.options.Authorize(ctx, p, write)
}
func (s *Service) load(ctx context.Context, owner core.ID, id string) (Installation, error) {
	if !pluginID.MatchString(id) {
		return Installation{}, invalid("invalid plugin ID")
	}
	return persistence.LoadCatalog[Installation](ctx, s.store, "plugins.installations", owner, Key(id))
}
func (s *Service) list(ctx context.Context, owner core.ID) ([]Installation, error) {
	var e error
	owner, e = persistence.CatalogOwner(ctx, s.store, owner)
	if e != nil {
		return nil, e
	}
	all, err := persistence.Select[Installation](ctx, s.store, "plugins.installations", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("Deleted", false)}})
	out := []Installation{}
	for _, v := range all {
		if v.OwnerID == owner && !v.Deleted {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}
func (s *Service) List(ctx context.Context, p core.Principal) ([]Installation, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return nil, err
	}
	rows, err := s.list(ctx, p.OwnerID)
	if err != nil {
		return nil, err
	}
	admin := true
	if s.options.IsAdministrator != nil {
		admin, err = s.options.IsAdministrator(ctx, p)
		if err != nil {
			return nil, err
		}
	}
	visible := []Installation{}
	for _, v := range rows {
		if err := s.checkPlugin(ctx, p, v.ID); err != nil {
			if core.CodeOf(err) == core.Forbidden {
				continue
			}
			return nil, err
		}
		if !admin && !v.Enabled {
			continue
		}
		pref, err := s.preference(ctx, p.OwnerID, v.ID)
		if err != nil {
			return nil, err
		}
		v.CatalogID, v.UserEnabled, v.UserVersion = Key(v.ID), pref.Enabled, pref.Version
		for _, t := range v.Manifest.Contributes.Tools {
			v.ToolIDs = append(v.ToolIDs, core.ID(ToolName(v, t.ID)))
		}
		for _, sk := range v.Manifest.Contributes.Skills {
			v.ToolIDs = append(v.ToolIDs, core.ID(SkillName(v, sk.ID)))
		}
		if !admin {
			v.Bindings = []Binding{}
			v.Versions = nil
			v.Manifest.AdminSettings = nil
			v.Manifest.AdminSettingsPage = nil
		}
		visible = append(visible, v)
	}
	rows = visible
	return rows, nil
}
func randomID() core.ID {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return core.ID(hex.EncodeToString(b[:]))
}
func (s *Service) audit(ctx context.Context, p core.Principal, action, id string) error {
	return audit.NewRepository(s.store).Append(ctx, audit.Record{ID: randomID(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: "plugin." + action, Target: core.Object{Kind: "plugin", ID: Key(id)}, Outcome: audit.Succeeded, At: time.Now()})
}
func (s *Service) Install(ctx context.Context, p core.Principal, pkg Package) (Installation, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return Installation{}, err
	}
	if err := pkg.Validate(); err != nil {
		return Installation{}, err
	}
	var out Installation
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		catalogOwner, err := persistence.CatalogOwner(ctx, s.store, p.OwnerID)
		if err != nil {
			return err
		}
		old, err := s.load(ctx, p.OwnerID, pkg.Manifest.ID)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		digest := pkg.Digest()
		releaseKey := persistence.Key(catalogOwner, Key(pkg.Manifest.ID+"@"+pkg.Manifest.Version))
		previous, e := persistence.Load[storedPackage](ctx, s.store, "plugins.packages", releaseKey)
		if e == nil && previous.Digest != digest {
			return core.NewError(core.Conflict, "released versions are immutable")
		}
		if e != nil && !persistence.Missing(e) {
			return e
		}
		if old.ID != "" && !old.Deleted && slices.ContainsFunc(old.Versions, func(r Release) bool { return r.Version == pkg.Manifest.Version }) {
			out = old
			return nil
		}
		if old.ID == "" || old.Deleted {
			count, err := tx.Count(ctx, "plugins.installations", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", catalogOwner), persistence.Eq("Deleted", false)}, Limit: 32})
			if err != nil {
				return err
			}
			if count >= 32 {
				return invalid("installed plugin limit reached")
			}
		}
		if old.ID == "" {
			old = Installation{OwnerID: catalogOwner, ID: pkg.Manifest.ID, Manifest: pkg.Manifest, Digest: digest, Grants: []string{}, Bindings: []Binding{}, Versions: []Release{}}
		}
		if old.Deleted {
			old.Deleted = false
			old.Enabled = false
			old.Bindings = []Binding{}
			old.Grants = []string{}
			old.Manifest = pkg.Manifest
			old.Digest = digest
		}
		found := slices.ContainsFunc(old.Versions, func(r Release) bool { return r.Version == pkg.Manifest.Version })
		if !found {
			if len(old.Versions) >= 8 {
				return invalid("at most eight retained releases")
			}
			old.Versions = append(old.Versions, Release{pkg.Manifest.Version, digest})
		}
		if err := tx.Put(ctx, "plugins.packages", releaseKey, storedPackage{catalogOwner, pkg, digest}); err != nil {
			return err
		}
		old.Version++
		out = old
		if err := tx.Put(ctx, "plugins.installations", persistence.Key(catalogOwner, Key(old.ID)), old); err != nil {
			return err
		}
		return s.audit(ctx, p, "install", old.ID)
	})
	return out, err
}
func (s *Service) Configure(ctx context.Context, p core.Principal, q Configure) (Installation, error) {
	if err := s.authorize(ctx, p, true); err != nil {
		return Installation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Installation
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		catalogOwner, err := persistence.CatalogOwner(ctx, s.store, p.OwnerID)
		if err != nil {
			return err
		}
		v, err := s.load(ctx, p.OwnerID, q.ID)
		if err != nil {
			return err
		}
		if v.Deleted || q.ExpectedVersion == 0 || v.Version != q.ExpectedVersion {
			return core.NewError(core.Conflict, "plugin changed; reload before saving")
		}
		if q.Release != "" && q.Release != v.Manifest.Version {
			pkg, err := persistence.Load[storedPackage](ctx, s.store, "plugins.packages", persistence.Key(catalogOwner, Key(q.ID+"@"+q.Release)))
			if err != nil {
				return err
			}
			v.Manifest = pkg.Package.Manifest
			v.Digest = pkg.Digest
		}
		allowed := Requested(v.Manifest)
		if len(q.Grants) > len(capabilities) || len(q.Bindings) > 100 {
			return invalid("too many grants or bindings")
		}
		for _, g := range q.Grants {
			if !slices.Contains(allowed, g) {
				return invalid("undeclared capability")
			}
		}
		seen := map[core.ID]bool{}
		for _, b := range q.Bindings {
			if b.AssistantID.Validate() != nil || seen[b.AssistantID] {
				return invalid("invalid assistant binding")
			}
			seen[b.AssistantID] = true
			if s.options.CheckAssistant == nil {
				return core.NewError(core.Forbidden, "assistant validation unavailable")
			}
			if err := s.options.CheckAssistant(ctx, p, b.AssistantID); err != nil {
				return err
			}
			if len(b.Tools) > len(v.Manifest.Contributes.Tools) || len(b.Skills) > len(v.Manifest.Contributes.Skills) {
				return invalid("invalid binding contributions")
			}
			if needsCompute(v.Manifest, b.Tools) && b.ComputeID.Validate() != nil {
				return invalid("select a compute resource")
			}
			if b.ComputeID != "" {
				if s.options.CheckCompute == nil {
					return invalid("compute unavailable")
				}
				if err := s.options.CheckCompute(ctx, p, b.ComputeID); err != nil {
					return err
				}
			}
			for _, id := range b.Tools {
				if !slices.ContainsFunc(v.Manifest.Contributes.Tools, func(t Tool) bool { return t.ID == id }) {
					return invalid("unknown bound tool")
				}
			}
			for _, id := range b.Skills {
				idx := slices.IndexFunc(v.Manifest.Contributes.Skills, func(t Skill) bool { return t.ID == id })
				if idx < 0 {
					return invalid("unknown bound skill")
				}
				for _, dep := range v.Manifest.Contributes.Skills[idx].Tools {
					if !slices.Contains(b.Tools, dep) {
						return invalid("skill requires an enabled tool")
					}
				}
			}
		}
		v.Enabled = q.Enabled
		v.Grants = slices.Clone(q.Grants)
		v.Bindings = slices.Clone(q.Bindings)
		v.Version++
		out = v
		if err := tx.Put(ctx, "plugins.installations", persistence.Key(catalogOwner, Key(v.ID)), v); err != nil {
			return err
		}
		return s.audit(ctx, p, "configure", v.ID)
	})
	if err == nil {
		s.cancelPluginLocked(q.ID)
	}
	return out, err
}
func (s *Service) Remove(ctx context.Context, p core.Principal, id string, expected core.Version) error {
	if err := s.authorize(ctx, p, true); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		catalogOwner, err := persistence.CatalogOwner(ctx, s.store, p.OwnerID)
		if err != nil {
			return err
		}
		v, err := s.load(ctx, p.OwnerID, id)
		if err != nil {
			return err
		}
		if expected == 0 || v.Version != expected {
			return core.NewError(core.Conflict, "plugin changed")
		}
		v.Deleted = true
		v.Enabled = false
		v.Bindings = []Binding{}
		v.Grants = []string{}
		v.Version++
		if err := tx.Put(ctx, "plugins.installations", persistence.Key(catalogOwner, Key(id)), v); err != nil {
			return err
		}
		return s.audit(ctx, p, "uninstall", id)
	})
	if err == nil {
		s.cancelPluginLocked(id)
	}
	return err
}
func (s *Service) cancelPluginLocked(id string) {
	for _, r := range s.httpRunning {
		if r.PluginID == id {
			r.Cancel()
		}
	}
	for key, runs := range s.running {
		if strings.HasSuffix(key, "/"+id) {
			for _, cancel := range runs {
				cancel()
			}
			delete(s.running, key)
		}
	}
}
func (s *Service) cancelLocked(owner core.ID, id string) {
	for _, cancel := range s.running[string(owner)+"/"+id] {
		cancel()
	}
	delete(s.running, string(owner)+"/"+id)
}
func (s *Service) active(ctx context.Context, p core.Principal, id, digest string) (Installation, error) {
	v, err := s.load(ctx, p.OwnerID, id)
	if err != nil {
		return v, err
	}
	if v.Deleted || !v.Enabled || v.Digest != digest || v.Manifest.PluginAPI != 2 {
		return v, core.NewError(core.Forbidden, "plugin is disabled or its version changed")
	}
	if err := s.available(ctx, p, v); err != nil {
		return v, err
	}
	return v, nil
}
func (s *Service) pkg(ctx context.Context, v Installation) (Package, error) {
	p, err := persistence.Load[storedPackage](ctx, s.store, "plugins.packages", persistence.Key(v.OwnerID, Key(v.ID+"@"+v.Manifest.Version)))
	if err == nil && p.Digest != v.Digest {
		return Package{}, core.NewError(core.Conflict, "package digest mismatch")
	}
	return p.Package, err
}
func granted(v Installation, caps []string) bool {
	for _, c := range caps {
		if !slices.Contains(v.Grants, c) {
			return false
		}
	}
	return true
}
func entryFor(v Installation, kind, id, page string) (string, []string, error) {
	if kind == "admin-settings" && v.Manifest.AdminSettingsPage != nil && v.Manifest.AdminSettingsPage.ID == id {
		p := v.Manifest.AdminSettingsPage
		return p.Entry, p.Capabilities, nil
	}
	if kind == "settings" && v.Manifest.SettingsPage != nil && v.Manifest.SettingsPage.ID == id {
		p := v.Manifest.SettingsPage
		return p.Entry, p.Capabilities, nil
	}
	if kind == "page" {
		for _, p := range v.Manifest.Contributes.Pages {
			if p.ID == id {
				return p.Entry, p.Capabilities, nil
			}
		}
	}
	if kind == "theme" {
		for _, t := range v.Manifest.Contributes.Themes {
			if t.ID == id && t.Pages[page] != "" {
				return t.Pages[page], t.Capabilities, nil
			}
		}
	}
	return "", nil, core.NewError(core.NotFound, "extension not found")
}

type UIRequest struct {
	ID              string
	Digest          string
	Kind            string
	Contribution    string
	Page            string
	Method          string
	ExpectedVersion core.Version
	Values          map[string]any
	ConnectionID    core.ID
	Mutation        core.Mutation
}

func (s *Service) UI(ctx context.Context, p core.Principal, q UIRequest) (any, error) {
	if err := s.authorize(ctx, p, false); err != nil {
		return nil, err
	}
	scope := ""
	var v Installation
	var err error
	if q.Kind == "admin-settings" {
		scope = "admin"
		v, _, err = s.settingsInstallation(ctx, p, q.ID, q.Digest, scope)
	} else {
		v, err = s.active(ctx, p, q.ID, q.Digest)
	}
	if err != nil {
		return nil, err
	}
	entry, caps, err := entryFor(v, q.Kind, q.Contribution, q.Page)
	if err != nil {
		return nil, err
	}
	if !granted(v, caps) {
		return nil, core.NewError(core.Forbidden, "plugin permissions required")
	}
	switch q.Method {
	case "settings.get", "settings.update", "connections.list", "connections.test":
		required := map[string]string{"settings.get": "settings.read", "settings.update": "settings.write", "connections.list": "connections.read", "connections.test": "connections.test"}[q.Method]
		if !slices.Contains(caps, required) {
			return nil, core.NewError(core.Forbidden, "page capability required")
		}
		switch q.Method {
		case "settings.get":
			return s.Settings(ctx, p, v.ID, v.Digest, scope)
		case "settings.update":
			return s.SaveSettings(ctx, p, SettingsRequest{Scope: scope, ID: v.ID, Digest: v.Digest, ExpectedVersion: q.ExpectedVersion, Values: q.Values})
		case "connections.list":
			return s.Connections(ctx, p, v.ID, v.Digest)
		case "connections.test":
			return s.TestConnection(ctx, p, TestConnectionRequest{PluginID: v.ID, Digest: v.Digest, ID: q.ConnectionID, Mutation: q.Mutation})
		}

	case "document":
		pkg, err := s.pkg(ctx, v)
		if err != nil {
			return nil, err
		}
		return map[string]any{"HTML": pkg.Files[entry], "Digest": v.Digest, "Capabilities": caps}, nil
	case "plans.list":
		if !slices.Contains(caps, "plans.read") || s.options.ReadPlans == nil {
			return nil, core.NewError(core.Forbidden, "page does not have plans.read")
		}
		return s.options.ReadPlans(ctx, p)
	}
	return nil, core.NewError(core.Forbidden, "unsupported plugin method")
}
