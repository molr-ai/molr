package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"molr.ai/runtime/internal/textfiles"
	"net/http"
	"net/netip"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/agent/tools"
	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/policy"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

type sandboxView struct {
	ConnectionReason                   string `json:",omitempty"`
	CheckedAt                          time.Time
	ID                                 core.ID
	Name, Endpoint, Fingerprint, State string
	Local                              bool
	Version                            core.Version
	Info                               sm.Info
}
type sandboxConnection struct {
	OwnerID core.ID
	View    sandboxView
	Secret  []byte
	Digest  string
}
type sandboxRouter struct {
	waitMu                  sync.Mutex
	waitWG                  sync.WaitGroup
	waitClosed, waitPolling bool
	nextWaitPoll            time.Time
	adapters                adapters
	shell                   *shellService
	files                   *resourceFileTools
	catalog                 tools.Catalog
	cipher                  cipher.AEAD
	verify                  func(context.Context, core.Principal, auth.Verification, string) error
}

func newSandboxRouter(a adapters, shell *shellService, key []byte) (*sandboxRouter, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	box, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sandboxRouter{adapters: a, shell: shell, cipher: box}, nil
}
func (s *sandboxRouter) connection(ctx context.Context, p core.Principal, id core.ID) (sandboxConnection, error) {
	c, err := persistence.LoadCatalog[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", p.OwnerID, id)
	if err != nil {
		return c, err
	}
	if !catalogOwned(ctx, s.adapters.store, c.OwnerID, p.OwnerID) || c.View.ID != id || c.View.State == "disconnected" {
		return c, denied()
	}
	return c, nil
}
func (s *sandboxRouter) client(ctx context.Context, p core.Principal, c sandboxConnection) (*sm.Client, error) {
	if len(c.Secret) < s.cipher.NonceSize() {
		return nil, denied()
	}
	nonce := c.Secret[:s.cipher.NonceSize()]
	data, err := s.cipher.Open(nil, nonce, c.Secret[s.cipher.NonceSize():], []byte(persistence.Key(c.OwnerID, c.View.ID)))
	if err != nil {
		return nil, denied()
	}
	defer clear(data)
	return s.newClient(p, c.View.Endpoint, c.View.Fingerprint, core.NewSecret(string(data)))
}
func (s *sandboxRouter) newClient(p core.Principal, endpoint, fingerprint string, token core.Secret) (*sm.Client, error) {
	addresses := []netip.Addr{}
	for _, raw := range s.adapters.networkConfig.AllowedPrivateIPs {
		a, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, err
		}
		addresses = append(addresses, a)
	}
	guard := platform.NewNetworkGuard(platform.NetworkOptions{AllowedPrivateAddresses: addresses, OwnerBinding: func(context.Context, core.Principal, core.ID) (platform.NetworkBinding, error) {
		return platform.NetworkBinding{Origin: endpoint}, nil
	}, Binding: func(context.Context, core.Execution, core.ID) (platform.NetworkBinding, error) {
		return platform.NetworkBinding{Origin: endpoint}, nil
	}})
	var client *http.Client
	if p.Kind == core.ActorOwner {
		client = guard.OwnerHTTPClient(p, "sandbox", endpoint)
	} else {
		client = guard.HTTPClient(core.Execution{Principal: p, AssistantID: p.SubjectID, OperationID: "sandbox-transport"}, "sandbox", endpoint)
	}
	return sm.NewClient(endpoint, fingerprint, token, client)
}
func (s *sandboxRouter) localInfo(ctx context.Context) sm.Info {
	cfg := s.adapters.storageConfig
	info := sm.Info{WorkspaceProjectQuotas: cfg.WorkspaceProjectQuotas, ResourceQuotas: true, UserGroupQuotas: true, AuthorizationCeilings: cfg.ComputeCeilings(), ShellSessions: true, Protocol: sm.ProtocolVersion, Enabled: true, Storage: textfiles.Supported(), Capabilities: platform.Capabilities{OS: runtime.GOOS, Reason: "sandbox_disabled"}, Directories: []sm.Directory{}, Networks: []sm.Network{}, Limits: platform.Limits{MemoryBytes: cfg.MemoryBytes, Processes: cfg.Processes, CPUPercent: cfg.CPUPercent, TempBytes: cfg.TempBytes, MaxFileBytes: cfg.MaxFileBytes, NetworkBytes: cfg.NetworkBytes, NetworkConnections: cfg.NetworkConnections}, TimeoutSeconds: 0}
	if platform.NativeSandbox() {
		info.Capabilities.NetworkBackend = "seatbelt-toggle"
	}
	if s.shell != nil {
		info.Capabilities, _ = s.shell.runner.Capabilities(ctx)
		info.GPUs = s.shell.runner.GPUs()
	}
	for _, d := range cfg.Directories {
		info.Directories = append(info.Directories, sm.Directory{ID: directoryID(d.Path), Name: filepath.Base(d.Path), ReadOnly: d.ReadOnly})
	}
	for _, n := range cfg.NetworkTargets(s.adapters.ownPort) {
		info.Networks = append(info.Networks, sm.Network{ID: n.Name, Name: n.Name, Target: n.Target()})
	}
	return info
}
func (s *sandboxRouter) admin(ctx context.Context, p core.Principal, action string, raw json.RawMessage) (any, error) {
	catalogOwner, err := persistence.CatalogOwner(ctx, s.adapters.store, p.OwnerID)
	if err != nil {
		return nil, err
	}
	if err := p.RequireOwner(); err != nil {
		return nil, err
	}
	accessAction := policy.Manage
	if action == "list" || action == "tools" || action == "quota_usage" {
		accessAction = policy.Read
	}
	if err := s.adapters.policy.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "resource"}, Action: accessAction}); err != nil {
		return nil, err
	}
	switch action {
	case "quota_usage":
		var q struct{ AssistantID, ResourceID core.ID }
		if err := decodeSandboxArgs(raw, &q); err != nil {
			return nil, err
		}
		return s.quotaUsage(ctx, p, q.AssistantID, q.ResourceID)
	case "test":
		return s.testConnection(ctx, p, raw)
	case "rename":
		return s.renameSandbox(ctx, p, raw)
	case "details":
		return s.sandboxResourceDetails(ctx, p, raw)
	case "tools":
		return s.catalog.Catalog(ctx, p)
	case "list":
		out := []sandboxView{{ID: "local", Name: "Local sandbox", Local: true, State: "available", Version: 1, Info: s.localInfo(ctx), CheckedAt: time.Now()}}
		if !out[0].Info.Enabled {
			out[0].State = "disabled"
		}
		rows, err := persistence.SelectCatalog[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", p.OwnerID, persistence.Query{})
		if err != nil {
			return nil, err
		}
		for _, c := range rows {
			if !catalogOwned(ctx, s.adapters.store, c.OwnerID, p.OwnerID) {
				continue
			}
			v := c.View
			v.CheckedAt = time.Now()
			if v.State != "disconnected" {
				client, err := s.client(ctx, p, c)
				if err == nil {
					check, cancel := context.WithTimeout(ctx, 3*time.Second)
					v.Info, err = client.Info(check)
					cancel()
				}
				if err != nil {
					v.State = "offline"
					v.ConnectionReason = string(sandboxDiagnosis(err).Reason)
					v.Info = c.View.Info
					v.Info.Enabled = false
					v.Info.Storage = false
					v.Info.Capabilities = platform.Capabilities{OS: v.Info.Capabilities.OS, Reason: "sandbox_offline"}
				}
			}
			out = append(out, v)
		}
		visible := out[:0]
		for _, v := range out {
			ref, e := groupReference(ctx, s.adapters.store, p.OwnerID, "sandbox", v.ID)
			if e != nil {
				if core.CodeOf(e) == core.Forbidden {
					continue
				}
				return nil, e
			}
			if ref != nil {
				v.Info = groupGPUInfo(v.Info, ref)
				v.Info.Capabilities.FullAccess = v.Info.Capabilities.FullAccess && ref.AllowFullAccess
				v.Endpoint = ""
				v.Fingerprint = ""
				if c := ref.Quota.Compute; c != nil {
					v.Info.AuthorizationCeilings.CPUPercent = min(v.Info.AuthorizationCeilings.CPUPercent, c.CPUPercent)
					v.Info.AuthorizationCeilings.MemoryBytes = min(v.Info.AuthorizationCeilings.MemoryBytes, c.MemoryBytes)
					v.Info.AuthorizationCeilings.Processes = min(v.Info.AuthorizationCeilings.Processes, c.Processes)
					v.Info.AuthorizationCeilings.TempBytes = min(v.Info.AuthorizationCeilings.TempBytes, c.TempBytes)
				}
				if n := ref.Quota.Network; n != nil {
					v.Info.AuthorizationCeilings.NetworkBytes = min(v.Info.AuthorizationCeilings.NetworkBytes, n.Bytes)
					v.Info.AuthorizationCeilings.NetworkConnections = min(v.Info.AuthorizationCeilings.NetworkConnections, n.Connections)
				}
			}
			visible = append(visible, v)
		}
		return visible, nil
	case "connect":
		var q struct {
			ID                                 core.ID
			Name, Endpoint, Fingerprint, Token string
			Mutation                           core.Mutation
			Verification                       string
		}
		if err := decodeSandboxArgs(raw, &q); err != nil {
			return nil, err
		}
		if err := q.Mutation.Validate(q.ID != ""); err != nil {
			return nil, err
		}
		if strings.TrimSpace(q.Name) == "" || len(q.Name) > 256 {
			return nil, core.Invalid("name", "sandbox name required")
		}
		if p.SessionID == "" {
			return nil, denied()
		}
		if s.verify != nil {
			if err := s.verify(ctx, p, auth.Verification{Token: core.NewSecret(q.Verification)}, "credential.put"); err != nil {
				return nil, err
			}
		}
		q.Endpoint = strings.TrimSuffix(q.Endpoint, "/")
		q.Fingerprint = strings.ToLower(q.Fingerprint)
		client, err := s.newClient(p, q.Endpoint, q.Fingerprint, core.NewSecret(q.Token))
		if err != nil {
			return nil, err
		}
		info, err := client.Info(ctx)
		if err != nil {
			return nil, err
		}
		if info.Capabilities.OS != "linux" {
			return nil, core.Invalid("platform", "only Linux sandboxes are supported")
		}
		if q.ID != "" {
			if err = s.invalidate(ctx, p); err != nil {
				return nil, err
			}
		}
		id := q.ID
		if id == "" {
			id = core.ID("sb_" + digestBytes([]byte(string(p.OwnerID) + "/" + q.Mutation.IdempotencyKey))[:24])
		}
		if id == "local" || id.Validate() != nil {
			return nil, core.Invalid("id", "invalid sandbox ID")
		}
		digest := digestBytes([]byte(q.Name + "\x00" + q.Endpoint + "\x00" + q.Fingerprint + "\x00" + q.Token))
		nonce := make([]byte, s.cipher.NonceSize())
		if _, err = rand.Read(nonce); err != nil {
			return nil, err
		}
		secret := s.cipher.Seal(nonce, nonce, []byte(q.Token), []byte(persistence.Key(catalogOwner, id)))
		v := sandboxView{ID: id, Name: q.Name, Endpoint: q.Endpoint, Fingerprint: q.Fingerprint, State: "available", Version: 1, Info: info}
		err = s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			rows, err := persistence.Select[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", p.OwnerID), persistence.Eq("View.Endpoint", q.Endpoint)}})
			if err != nil {
				return err
			}
			for _, c := range rows {
				if c.OwnerID == p.OwnerID && c.View.ID != id && c.View.Endpoint == q.Endpoint {
					return core.NewError(core.Conflict, "sandbox endpoint already connected; update its existing connection")
				}
			}
			var old sandboxConnection
			err = tx.Get(ctx, "sandbox.connections", persistence.Key(catalogOwner, id), &old)
			if err == nil {
				if q.ID == "" {
					if old.Digest != digest {
						return core.NewError(core.Conflict, "mutation reused")
					}
					v = old.View
					return nil
				}
				if old.View.Version != q.Mutation.ExpectedVersion || old.View.Endpoint != q.Endpoint {
					return core.NewError(core.Conflict, "sandbox changed; endpoint cannot be replaced")
				}
				v.Version = old.View.Version + 1
			} else if !persistence.Missing(err) {
				return err
			} else if q.ID != "" {
				return err
			}
			if err := s.audit(ctx, p, "sandbox.connect", id); err != nil {
				return err
			}
			return tx.Put(ctx, "sandbox.connections", persistence.Key(catalogOwner, id), sandboxConnection{OwnerID: catalogOwner, View: v, Secret: secret, Digest: digest})
		})
		return v, err
	case "disconnect":
		var q struct {
			ID       core.ID
			Mutation core.Mutation
		}
		if err := decodeSandboxArgs(raw, &q); err != nil {
			return nil, err
		}
		if err := q.Mutation.Validate(true); err != nil {
			return nil, err
		}
		c, err := s.connection(ctx, p, q.ID)
		if err != nil {
			return nil, err
		}
		if c.View.Version != q.Mutation.ExpectedVersion {
			return nil, core.NewError(core.Conflict, "sandbox changed")
		}
		if err = s.invalidate(ctx, p); err != nil {
			return nil, err
		}
		err = s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			var current sandboxConnection
			if err := tx.Get(ctx, "sandbox.connections", persistence.Key(catalogOwner, q.ID), &current); err != nil {
				return err
			}
			if current.View.Version != q.Mutation.ExpectedVersion {
				return core.NewError(core.Conflict, "sandbox changed")
			}
			current.View.State = "disconnected"
			current.View.Version++
			if err := s.audit(ctx, p, "sandbox.disconnect", q.ID); err != nil {
				return err
			}
			return tx.Put(ctx, "sandbox.connections", persistence.Key(catalogOwner, q.ID), current)
		})
		return nil, err
	case "resource":
		return s.connectResource(ctx, p, raw)
	}
	return nil, core.NewError(core.NotFound, "sandbox action unavailable")
}
func (s *sandboxRouter) connectResource(ctx context.Context, p core.Principal, raw json.RawMessage) (any, error) {
	var q struct {
		SandboxID      core.ID
		Kind           resource.Kind
		SourceID, Name string
		Mutation       core.Mutation
	}
	if err := decodeSandboxArgs(raw, &q); err != nil {
		return nil, err
	}
	if err := q.Mutation.Validate(false); err != nil {
		return nil, err
	}
	if q.Name == "" || len(q.Name) > 256 {
		return nil, core.Invalid("name", "required")
	}
	var info sm.Info
	if q.SandboxID == "local" {
		info = s.localInfo(ctx)
	} else {
		c, err := s.connection(ctx, p, q.SandboxID)
		if err != nil {
			return nil, err
		}
		client, err := s.client(ctx, p, c)
		if err != nil {
			return nil, err
		}
		info, err = client.Info(ctx)
		if err != nil {
			return nil, err
		}
	}
	if !info.Enabled {
		return nil, core.NewError(core.Unavailable, "sandbox disabled")
	}
	binding := resource.Binding{SandboxID: q.SandboxID, SandboxResource: q.SourceID}
	actions := []string{}
	provider := "remote-" + string(q.Kind)
	switch q.Kind {
	case resource.Storage:
		for _, d := range info.Directories {
			if d.ID == q.SourceID {
				actions = []string{"read"}
				if !d.ReadOnly {
					actions = append(actions, "write")
				}
			}
		}
		if q.SandboxID == "local" {
			provider = "local-directory"
			for _, d := range s.adapters.storageConfig.Directories {
				if directoryID(d.Path) == q.SourceID {
					binding.RootPath = d.Path
				}
			}
		}
	case resource.Compute:
		if q.SourceID != "compute" {
			return nil, core.Invalid("source", "compute required")
		}
		actions = []string{"execute"}
		if q.SandboxID == "local" {
			provider = "local-sandbox"
		}
	case resource.Network:
		for _, n := range info.Networks {
			if n.ID == q.SourceID {
				actions = []string{"execute"}
			}
		}
		if q.SandboxID == "local" {
			provider = "sandbox-network"
			binding.SandboxTarget = q.SourceID
		}
	default:
		return nil, core.Invalid("kind", "sandbox resources are storage, compute or network")
	}
	if len(actions) == 0 {
		return nil, denied()
	}
	id := core.ID("sr_" + digestBytes([]byte(string(p.OwnerID) + "/" + string(q.SandboxID) + "/" + string(q.Kind) + "/" + q.Mutation.IdempotencyKey))[:24])
	if q.SandboxID == "local" {
		binding.SandboxResource = ""
	}
	binding.ResourceID = id
	v := resource.Resource{ID: id, OwnerID: p.OwnerID, SandboxID: q.SandboxID, Kind: q.Kind, Name: q.Name, Provider: provider, Actions: actions, State: resource.Available, Version: 1}
	err := s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := resource.NewRepository(s.adapters.store).Get(ctx, p.OwnerID, id)
		if err == nil {
			saved, err := resource.NewRepository(s.adapters.store).Binding(ctx, p.OwnerID, id)
			if err != nil {
				return err
			}
			if old.Name != q.Name || old.Kind != q.Kind || saved.SandboxResource != binding.SandboxResource || saved.RootPath != binding.RootPath || saved.SandboxTarget != binding.SandboxTarget {
				return core.NewError(core.Conflict, "resource mutation reused")
			}
			if old.State == resource.Disconnected {
				return core.NewError(core.Conflict, "resource disconnected")
			}
			v = old
			return nil
		}
		if !persistence.Missing(err) {
			return err
		}
		if err := s.audit(ctx, p, "sandbox.resource.connect", id); err != nil {
			return err
		}
		return resource.NewRepository(s.adapters.store).Insert(ctx, v, binding)
	})
	return v, err
}
func (s *sandboxRouter) epoch(ctx context.Context, owner core.ID) (uint64, error) {
	v, err := persistence.Load[uint64](ctx, s.adapters.store, "sandbox.authorization", string(owner))
	if persistence.Missing(err) {
		return 0, nil
	}
	return v, err
}
func (s *sandboxRouter) invalidate(ctx context.Context, p core.Principal) error {
	var epoch uint64
	err := s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		old, err := s.epoch(ctx, p.OwnerID)
		if err != nil {
			return err
		}
		epoch = old + 1
		return tx.Put(ctx, "sandbox.authorization", string(p.OwnerID), epoch)
	})
	if err != nil {
		return err
	}
	rows, err := persistence.SelectCatalog[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", p.OwnerID, persistence.Query{})
	if err != nil {
		return err
	}
	for _, c := range rows {
		if !catalogOwned(ctx, s.adapters.store, c.OwnerID, p.OwnerID) || c.View.State == "disconnected" {
			continue
		}
		client, err := s.client(ctx, p, c)
		if err == nil {
			revoke, cancel := context.WithTimeout(ctx, 3*time.Second)
			_ = client.Revoke(revoke, p.OwnerID, epoch)
			cancel()
		}
	}
	return nil
}

// Legacy resources remain addressable by their original IDs and grants.
func (s *sandboxRouter) migrate(ctx context.Context) error {
	return s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		rows, err := tx.Query(ctx, "business.resources", persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("SandboxID", "")}, {persistence.Eq("SandboxID", nil)}}})
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := persistence.Decode[resource.Resource](row)
			if err != nil {
				return err
			}
			if r.SandboxID == "" && (r.Kind == resource.Storage || r.Kind == resource.Compute || r.Kind == resource.Network) {
				r.SandboxID = "local"
				if err = tx.Put(ctx, "business.resources", row.Key, r); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *sandboxRouter) audit(ctx context.Context, p core.Principal, action string, id core.ID) error {
	return audit.NewRepository(s.adapters.store).Append(ctx, audit.Record{ID: randomOperation(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: action, Target: core.Object{Kind: "sandbox", ID: id}, Outcome: audit.Succeeded, At: time.Now()})
}
