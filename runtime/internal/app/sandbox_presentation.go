package app

import (
	"context"
	"encoding/json"
	"errors"
	"molr.ai/runtime/internal/catalog"
	"strings"
	"time"

	"molr.ai/runtime/internal/business/resource"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/platform"
	"molr.ai/runtime/internal/sandboxidentity"
	sm "molr.ai/runtime/internal/sandboxmanager"
)

// Only fixed reason/field values are exposed. Transport errors may contain
// endpoints or credentials and must never be copied into owner responses.
type sandboxDiagnostic struct {
	Connected bool
	Reason    core.Reason
	Field     string
	Info      *sm.Info `json:",omitempty"`
	CheckedAt time.Time
}

func sandboxDiagnosis(err error) sandboxDiagnostic {
	out := sandboxDiagnostic{Reason: core.ReasonOf(err), CheckedAt: time.Now()}
	switch out.Reason {
	case "sandbox_certificate_mismatch", "sandbox_auth_failed", "sandbox_connection_code_invalid", "sandbox_connection_code_unsupported":
		out.Field = "ConnectionCode"
	case "sandbox_destination_denied", "sandbox_unreachable", "sandbox_connection_timeout", "sandbox_protocol_unsupported":
		out.Field = "Endpoint"
	default:
		var detail *core.Error
		if errors.As(err, &detail) && detail.Code == core.InvalidArgument {
			switch detail.Field {
			case "fingerprint":
				out.Reason = "sandbox_connection_code_invalid"
				out.Field = "ConnectionCode"
			case "token":
				out.Reason = "sandbox_connection_code_invalid"
				out.Field = "ConnectionCode"
			default:
				out.Reason = "sandbox_endpoint_invalid"
				out.Field = "Endpoint"
			}
		} else {
			out.Reason = "sandbox_unreachable"
			out.Field = "Endpoint"
		}
	}
	return out
}
func (s *sandboxRouter) testConnection(ctx context.Context, p core.Principal, raw json.RawMessage) (any, error) {
	if p.SessionID == "" {
		return nil, denied()
	}
	var q struct {
		ID                       core.ID
		Endpoint, ConnectionCode string
	}
	if err := decodeSandboxArgs(raw, &q); err != nil {
		return nil, err
	}
	var client *sm.Client
	var err error
	if q.ID != "" && q.ConnectionCode == "" {
		c, e := s.connection(ctx, p, q.ID)
		if e != nil {
			return nil, e
		}
		if q.Endpoint != "" && strings.TrimSuffix(q.Endpoint, "/") != c.View.Endpoint {
			return nil, core.Invalid("connection", "saved credential cannot be used with another endpoint or certificate")
		}
		client, err = s.client(ctx, p, c)
	} else {
		var publicKey string
		var token core.Secret
		publicKey, token, err = sandboxidentity.Decode(q.ConnectionCode)
		if err == nil {
			client, err = s.newClient(p, strings.TrimSuffix(q.Endpoint, "/"), publicKey, token)
		}
	}
	if err != nil {
		return sandboxDiagnosis(err), nil
	}
	probe, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	info, err := client.Info(probe)
	if err != nil {
		return sandboxDiagnosis(err), nil
	}
	if info.Capabilities.OS != "linux" && info.Capabilities.OS != "darwin" {
		return sandboxDiagnostic{Reason: "sandbox_platform_unsupported", Field: "Endpoint", CheckedAt: time.Now()}, nil
	}
	return sandboxDiagnostic{Connected: true, Info: &info, CheckedAt: time.Now()}, nil
}
func (s *sandboxRouter) renameSandbox(ctx context.Context, p core.Principal, raw json.RawMessage) (any, error) {
	catalogOwner := catalog.Scope
	var q struct {
		ID       core.ID
		Name     string
		Mutation core.Mutation
	}
	if err := decodeSandboxArgs(raw, &q); err != nil {
		return nil, err
	}
	if err := q.Mutation.Validate(true); err != nil {
		return nil, err
	}
	q.Name = strings.TrimSpace(q.Name)
	if q.ID.Validate() != nil || q.ID == "local" || q.Name == "" || len(q.Name) > 256 {
		return nil, core.Invalid("sandbox", "valid remote sandbox and name required")
	}
	var result sandboxView
	err := s.adapters.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(catalogOwner, q.ID)
		var current sandboxConnection
		if err := tx.Get(ctx, "sandbox.connections", key, &current); err != nil {
			return err
		}
		if current.OwnerID != catalogOwner {
			return denied()
		}
		// A retried rename is harmless only when both the desired name and version match.
		if current.View.Version == q.Mutation.ExpectedVersion+1 && current.View.Name == q.Name {
			result = current.View
			return nil
		}
		if current.View.Version != q.Mutation.ExpectedVersion {
			return core.NewError(core.Conflict, "sandbox changed")
		}
		current.View.Name = q.Name
		current.View.Version++
		result = current.View
		if err := s.audit(ctx, p, "sandbox.rename", q.ID); err != nil {
			return err
		}
		return tx.Put(ctx, "sandbox.connections", key, current)
	})
	return result, err
}

// Metadata is available only through the owner API, never permission_list or tool
// results. Remote host paths and all connection secrets remain inaccessible.
type sandboxResourceDetail struct {
	Capacity       *platform.StorageCapacity `json:",omitempty"`
	ID             core.ID
	SourceID, Path string
}

func (s *sandboxRouter) sandboxResourceDetails(ctx context.Context, p core.Principal, raw json.RawMessage) (any, error) {
	var q struct{ ID core.ID }
	if err := decodeSandboxArgs(raw, &q); err != nil {
		return nil, err
	}
	if q.ID.Validate() != nil {
		return nil, core.Invalid("sandbox_id", "required")
	}
	out := []sandboxResourceDetail{}
	remoteDirectories := map[int]sm.DirectoryRequest{}
	err := s.adapters.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if q.ID != "local" {
			c, err := catalog.Load[sandboxConnection](ctx, s.adapters.store, "sandbox.connections", q.ID)
			if err != nil {
				return err
			}
			if c.OwnerID != catalog.Scope {
				return denied()
			}
		}
		rows, err := catalog.Select[resource.Resource](ctx, s.adapters.store, "business.resources", persistence.Query{})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.OwnerID != catalog.Scope || sandboxOf(r) != q.ID || (r.Kind != resource.Storage && r.Kind != resource.Compute && r.Kind != resource.Network) {
				continue
			}
			b, err := resource.NewRepository(s.adapters.store).Binding(ctx, p.OwnerID, r.ID)
			if err != nil {
				return err
			}
			v := sandboxResourceDetail{ID: r.ID, SourceID: b.SandboxResource}
			if q.ID != "local" && r.Kind == resource.Storage {
				if ref, err := directoryReference(b.SandboxResource); err == nil {
					v.SourceID = ref.SourceID
					v.Path = ref.Path
					if r.State == resource.Available {
						remoteDirectories[len(out)] = ref
					}
				}
			}
			if q.ID == "local" {
				switch r.Kind {
				case resource.Storage:
					v.SourceID = directoryID(b.RootPath)
					v.Path = b.RootPath
					if r.State == resource.Available {
						root, reserve := b.RootPath, int64(0)
						if r.Provider == "managed-workspace" {
							root = platform.AssistantWorkspace(s.adapters.dataDir, s.adapters.storageConfig.WorkRoot, "", "")
							reserve = s.adapters.storageConfig.MinFreeDiskBytes
						}
						capacity := platform.FilesystemCapacity(root, reserve)
						v.Capacity = &capacity
					}
				case resource.Compute:
					v.SourceID = "compute"
				case resource.Network:
					v.SourceID = b.SandboxTarget
				}
			}
			out = append(out, v)
		}
		return nil
	})
	if err == nil && len(remoteDirectories) > 0 {
		connection, e := s.connection(ctx, p, q.ID)
		if e == nil {
			client, e := s.client(ctx, p, connection)
			if e == nil {
				probe, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				for index, request := range remoteDirectories {
					if directory, e := client.Directory(probe, request); e == nil {
						out[index].Capacity = &directory.Capacity
					}
				}
			}
		}
	}
	return out, err
}
