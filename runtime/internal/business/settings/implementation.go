package settings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"molr.ai/runtime/internal/audit"
	"molr.ai/runtime/internal/auth"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

var uiThemePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+@[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$`)

func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func invalid(field string) error { return core.Invalid(field, "invalid value") }
func conflict() error            { return core.NewError(core.Conflict, "version or idempotency conflict") }
func missing() error             { return core.NewError(core.NotFound, "object not found") }
func forbidden() error           { return core.NewError(core.Forbidden, "access denied") }
func (s *service) now() time.Time {
	if s.options.Now != nil {
		return s.options.Now()
	}
	return time.Now().UTC()
}
func (s *service) id() core.ID {
	if s.options.NewID != nil {
		return s.options.NewID()
	}
	return newID()
}
func (s *service) authorize(ctx context.Context, p core.Principal, target core.Object, action policy.Action, ownerOnly bool) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if ownerOnly {
		if err := p.RequireOwner(); err != nil {
			return err
		}
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Unavailable, "authorization not configured")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: target, Action: action})
}
func (s *service) read(ctx context.Context, p core.Principal, id core.ID, action policy.Action, ownerOnly bool, fn func(context.Context) error) error {
	return s.store.Read(ctx, func(ctx context.Context, _ persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: id}, action, ownerOnly); err != nil {
			return err
		}
		return fn(ctx)
	})
}

func mutate[T any](ctx context.Context, s *service, p core.Principal, action string, id core.ID, m core.Mutation, payload any, update, ownerOnly bool, fn func(context.Context) (T, error)) (T, error) {
	var result T
	if err := m.Validate(update); err != nil {
		return result, err
	}
	digest, err := persistence.PayloadDigest(payload)
	if err != nil {
		return result, err
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: id}, policy.Write, ownerOnly); err != nil {
			return err
		}
		key := persistence.Key(p.OwnerID, core.ID(action+"/"+string(id)+"/"+m.IdempotencyKey))
		replayed, err := persistence.ReplayReceipt(ctx, tx, receiptBucket, key, digest, &result)
		if replayed || err != nil {
			return err
		}
		result, err = fn(ctx)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if action != "cleanup" {
			target := core.Object{Kind: objectKind, ID: id}
			auditAction := "settings." + action
			if value, ok := any(result).(Export); ok {
				target.ID = value.ID
				auditAction = "settings.export.request"
			}
			if err := audit.NewRepository(tx.Store()).Append(ctx, audit.Record{ID: s.id(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: auditAction, Target: target, Outcome: audit.Succeeded, At: s.now()}); err != nil {
				return err
			}
		}
		return tx.Put(ctx, receiptBucket, key, persistence.Receipt{Digest: digest, Result: encoded})
	})
	return result, err
}
func validText(text string, limit int) bool {
	return strings.TrimSpace(text) != "" && len(text) <= limit
}

const objectKind = "settings"
const bucket = "business.settings"
const exportBucket = "business.exports"
const receiptBucket = "business.settings.receipts"

// ExportBundle contains only authorized, allowlisted product data. The file
// adapter packages referenced artifacts when IncludeArtifacts is true.
type ExportBundle struct {
	ID               core.ID
	Records          map[string][]json.RawMessage
	IncludeArtifacts bool
}
type Options struct {
	PrepareMemoryCleanup func(context.Context, core.Principal, memory.Ref) (string, error)
	CleanupMemory        func(context.Context, core.Principal, memory.Ref, string, string) error
	WorkbenchAvailable   bool
	Authorizer           policy.Authorizer
	Now                  func() time.Time
	NewID                func() core.ID
	CheckVerification    func(context.Context, core.Principal, auth.Verification, string) error
	SaveExport           func(context.Context, core.Principal, ExportBundle) (core.ID, error)
	// CleanupObject validates when apply=false; when true it performs only
	// transactional database changes. External file GC follows after commit.
	CleanupObject func(context.Context, core.Principal, core.Object, bool) error
}
type repository struct{ store persistence.Store }
type service struct {
	store   persistence.Store
	repo    Repository
	options Options
}

func NewRepository(store persistence.Store) Repository { return &repository{store: store} }
func NewService(store persistence.Store, options Options) Service {
	return &service{store: store, repo: NewRepository(store), options: options}
}
func (r *repository) Get(ctx context.Context, owner core.ID) (Settings, error) {
	return persistence.Load[Settings](ctx, r.store, bucket, string(owner))
}
func (r *repository) Save(ctx context.Context, v Settings, expected core.Version) error {
	if expected == 0 && v.Version == 1 {
		return persistence.Insert(ctx, r.store, bucket, string(v.OwnerID), v)
	}
	return persistence.SaveVersion(ctx, r.store, bucket, string(v.OwnerID), v, expected, func(v Settings) core.Version { return v.Version })
}
func (r *repository) PutExport(ctx context.Context, owner core.ID, v Export) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Put(ctx, exportBucket, persistence.Key(owner, v.ID), v)
	})
}
func (r *repository) Export(ctx context.Context, owner, id core.ID) (Export, error) {
	return persistence.Load[Export](ctx, r.store, exportBucket, persistence.Key(owner, id))
}
func (s *service) Get(ctx context.Context, p core.Principal) (Settings, error) {
	var result Settings
	err := s.read(ctx, p, p.OwnerID, policy.Read, true, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Get(ctx, p.OwnerID)
		if persistence.Missing(err) {
			result = Settings{OwnerID: p.OwnerID, Language: "en", Timezone: "UTC", Currency: "USD", Theme: System, Palette: Forest, UITheme: DefaultUITheme}
			return nil
		}
		if !s.options.WorkbenchAvailable {
			result.WorkbenchVisible = false
		}
		if result.Palette == "" {
			result.Palette = Forest
		}
		if result.UITheme == "" {
			result.UITheme = DefaultUITheme
		}
		if result.Currency == "" {
			result.Currency = "USD"
		}
		return err
	})
	return result, err
}
func (s *service) Update(ctx context.Context, p core.Principal, req UpdateRequest) (Settings, error) {
	return mutate(ctx, s, p, "update", p.OwnerID, req.Mutation, req, req.Mutation.ExpectedVersion != 0, true, func(ctx context.Context) (Settings, error) {
		v := req.Settings
		if v.WorkbenchVisible && !s.options.WorkbenchAvailable {
			return v, core.NewError(core.FailedPrecondition, "runtime workbench is disabled")
		}
		if v.Currency != "USD" && v.Currency != "CNY" {
			return v, invalid("currency")
		}
		if v.OwnerID != p.OwnerID {
			return v, forbidden()
		}
		// Omitted fields from older clients must not erase saved appearance choices.
		if v.Palette == "" || v.UITheme == "" {
			previous, err := s.repo.Get(ctx, p.OwnerID)
			if err != nil && !persistence.Missing(err) {
				return v, err
			}
			if v.Palette == "" {
				v.Palette = previous.Palette
				if v.Palette == "" {
					v.Palette = Forest
				}
			}
			if v.UITheme == "" {
				v.UITheme = previous.UITheme
				if v.UITheme == "" {
					v.UITheme = DefaultUITheme
				}
			}
		}
		if len(v.UITheme) > 162 || !uiThemePattern.MatchString(v.UITheme) {
			return v, invalid("ui_theme")
		}
		switch v.Palette {
		case Forest, Ocean, Iris, Sand, Rose, Monochrome:
		default:
			return v, invalid("palette")
		}
		if v.Theme != System && v.Theme != Light && v.Theme != Dark {
			return v, invalid("theme")
		}
		if _, err := time.LoadLocation(v.Timezone); err != nil {
			return v, invalid("timezone")
		}
		if !validText(v.Language, 64) {
			return v, invalid("language")
		}

		v.Version = req.Mutation.ExpectedVersion + 1
		return v, s.repo.Save(ctx, v, req.Mutation.ExpectedVersion)
	})
}
func (s *service) verify(ctx context.Context, p core.Principal, v auth.Verification, purpose string) error {
	if s.options.CheckVerification == nil {
		return core.NewError(core.Unavailable, "verification not configured")
	}
	return s.options.CheckVerification(ctx, p, v, purpose)
}
func (s *service) Export(ctx context.Context, p core.Principal, req ExportRequest) (Export, error) {
	if s.options.SaveExport == nil {
		return Export{}, core.NewError(core.Unavailable, "export storage not configured")
	}
	result, err := mutate(ctx, s, p, "export", "", req.Mutation, struct {
		Request ExportRequest
		Proof   string
	}{req, verificationDigest(req.Verification)}, false, true, func(ctx context.Context) (Export, error) {
		if err := s.verify(ctx, p, req.Verification, "export"); err != nil {
			return Export{}, err
		}
		v := Export{ID: s.id(), State: "pending", CreatedAt: s.now()}
		bundle := ExportBundle{ID: v.ID, Records: map[string][]json.RawMessage{}, IncludeArtifacts: req.IncludeArtifacts}
		buckets := []string{"business.assistants", "business.groups", "business.tasks", "business.revisions", "business.todos", "business.schedules", "business.plan-links", "business.resources", "business.dream_configs", "business.dream_timers", "business.dream_sessions", "business.dream_backlogs", "business.dream_runs", "business.settings"}
		if req.IncludeMessages {
			buckets = append(buckets, "im.conversations", "im.messages")
		}
		if req.IncludeArtifacts {
			buckets = append(buckets, "storage.files")
		}
		err := s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
			for _, bucket := range buckets {
				records, err := tx.Query(ctx, bucket, persistence.Query{Any: [][]persistence.Filter{{persistence.Eq("key", string(p.OwnerID))}, {{Field: "key", Op: ">=", Value: string(p.OwnerID) + "/"}, {Field: "key", Op: "<", Value: string(p.OwnerID) + "/\U0010ffff"}}}})
				if err != nil {
					return err
				}
				for _, record := range records {
					if record.Key == string(p.OwnerID) || strings.HasPrefix(record.Key, string(p.OwnerID)+"/") {
						bundle.Records[bucket] = append(bundle.Records[bucket], record.Data)
					}
				}
			}
			return nil
		})
		if err != nil {
			return v, err
		}
		if err = s.repo.PutExport(ctx, p.OwnerID, v); err != nil {
			return v, err
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			return tx.Put(ctx, "business.export-bundles", persistence.Key(p.OwnerID, v.ID), bundle)
		})
		return v, err
	})
	if err != nil {
		return result, err
	}
	current, err := s.repo.Export(ctx, p.OwnerID, result.ID)
	if err != nil {
		return Export{}, err
	}
	if current.State == "completed" {
		return current, nil
	}
	bundle, err := persistence.Load[ExportBundle](ctx, s.store, "business.export-bundles", persistence.Key(p.OwnerID, result.ID))
	if err != nil {
		return current, err
	}
	fileID, err := s.options.SaveExport(ctx, p, bundle)
	if err != nil {
		return current, err
	}
	if fileID.Validate() != nil {
		return current, core.NewError(core.Internal, "export adapter returned invalid file")
	}
	current.State = "completed"
	current.FileID = fileID
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind, ID: p.OwnerID}, policy.Manage, true); err != nil {
			return err
		}
		if err := s.repo.PutExport(ctx, p.OwnerID, current); err != nil {
			return err
		}
		if err := audit.NewRepository(tx.Store()).Append(ctx, audit.Record{ID: s.id(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: "settings.export.completed", Target: core.Object{Kind: "settings", ID: current.ID}, Outcome: audit.Succeeded, At: s.now()}); err != nil {
			return err
		}
		return tx.Delete(ctx, "business.export-bundles", persistence.Key(p.OwnerID, current.ID))
	})
	return current, err
}
func (s *service) ExportStatus(ctx context.Context, p core.Principal, id core.ID) (Export, error) {
	var result Export
	err := s.read(ctx, p, id, policy.Read, true, func(ctx context.Context) error {
		var err error
		result, err = s.repo.Export(ctx, p.OwnerID, id)
		return err
	})
	return result, err
}

type memoryCleanup struct {
	Ref     memory.Ref
	Version string
}

func (s *service) Cleanup(ctx context.Context, p core.Principal, req CleanupRequest) error {
	plans, err := mutate(ctx, s, p, "cleanup", "", req.Mutation, struct {
		Request CleanupRequest
		Proof   string
	}{req, verificationDigest(req.Verification)}, false, true, func(ctx context.Context) ([]memoryCleanup, error) {
		if len(req.ObjectIDs)+len(req.MemoryFiles) == 0 || len(req.ObjectIDs)+len(req.MemoryFiles) > 200 {
			return nil, invalid("objects")
		}
		plans := []memoryCleanup{}
		for _, object := range req.ObjectIDs {
			if err := object.ID.Validate(); err != nil {
				return nil, err
			}
			if object.Kind == "memory" {
				return nil, core.Invalid("memory_files", "use scope and path references")
			}
			if s.options.CleanupObject == nil {
				return nil, core.NewError(core.Unavailable, "cleanup unavailable")
			}
			if err := s.options.CleanupObject(ctx, p, object, false); err != nil {
				return nil, err
			}
		}
		for _, ref := range req.MemoryFiles {
			if err := ref.Validate(false); err != nil {
				return nil, err
			}
			if s.options.PrepareMemoryCleanup == nil || s.options.CleanupMemory == nil {
				return nil, core.NewError(core.Unavailable, "memory cleanup unavailable")
			}
			version, err := s.options.PrepareMemoryCleanup(ctx, p, ref)
			if err != nil {
				return nil, err
			}
			plans = append(plans, memoryCleanup{ref, version})
		}
		if err := s.verify(ctx, p, req.Verification, "cleanup"); err != nil {
			return nil, err
		}
		for _, object := range req.ObjectIDs {
			if object.Kind == "memory" {
				continue
			}
			if err := s.options.CleanupObject(ctx, p, object, true); err != nil {
				return nil, err
			}
			if err := audit.NewRepository(s.store).Append(ctx, audit.Record{ID: s.id(), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: "settings.cleanup", Target: object, Outcome: audit.Succeeded, At: s.now()}); err != nil {
				return nil, err
			}
		}
		return plans, nil
	})
	if err != nil {
		return err
	}
	// Files have independent commits. Retry the same authorized plan and hashes;
	// never expand a retry to newly-created or subsequently-edited content.
	for _, plan := range plans {
		if s.options.CleanupMemory == nil {
			return core.NewError(core.Unavailable, "memory cleanup unavailable")
		}
		refBytes, _ := json.Marshal(plan.Ref)
		sum := sha256.Sum256([]byte(req.Mutation.IdempotencyKey + "\x00" + string(refBytes)))
		fileTarget := audit.FileTarget{Scope: string(plan.Ref.Scope), AssistantID: plan.Ref.AssistantID, Path: plan.Ref.Path}
		key := hex.EncodeToString(sum[:])
		if err = s.options.CleanupMemory(ctx, p, plan.Ref, plan.Version, key); err != nil {
			return err
		}
		err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			var old audit.Record
			err := tx.Get(ctx, "security.audit", persistence.Key(p.OwnerID, core.ID(key)), &old)
			if err == nil {
				if old.Action == "settings.cleanup" && old.File != nil && *old.File == fileTarget {
					return nil
				}
				return conflict()
			}
			if !persistence.Missing(err) {
				return err
			}
			return audit.NewRepository(s.store).Append(ctx, audit.Record{ID: core.ID(key), OwnerID: p.OwnerID, SubjectID: p.SubjectID, ActorKind: p.Kind, Action: "settings.cleanup", Target: core.Object{Kind: "memory"}, File: &fileTarget, Outcome: audit.Succeeded, At: s.now()})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func verificationDigest(v auth.Verification) string {
	digest := sha256.Sum256([]byte(v.Token.Reveal()))
	return hex.EncodeToString(digest[:])
}
