package search

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

func newID() core.ID {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return core.ID(hex.EncodeToString(b[:]))
}
func invalid(field string) error { return core.Invalid(field, "invalid value") }
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

func paginate[T any](items []T, owner core.ID, query any, request core.PageRequest) (core.Page[T], error) {
	var result core.Page[T]
	p, err := request.Normalize()
	if err != nil {
		return result, err
	}
	encoded, _ := json.Marshal(query)
	digest := sha256.Sum256(encoded)
	scope := string(owner) + ":" + hex.EncodeToString(digest[:]) + ":"
	offset := 0
	if p.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
		if err != nil || !strings.HasPrefix(string(raw), scope) {
			return result, invalid("cursor")
		}
		offset, err = strconv.Atoi(strings.TrimPrefix(string(raw), scope))
		if err != nil || offset < 0 || offset > len(items) {
			return result, invalid("cursor")
		}
	}
	end := offset + p.Limit
	if end > len(items) {
		end = len(items)
	}
	// Detach the page so callers do not retain every matched document.
	result.Items = make([]T, end-offset)
	copy(result.Items, items[offset:end])
	if end < len(items) {
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + strconv.Itoa(end)))
	}
	return result, nil
}

const objectKind = "search"
const indexBucket = "search.documents"
const statusBucket = "search.jobs"

// Document is an authorized source projection; credentials/bindings are never
// accepted as searchable content. Documents returns authoritative current data.
type Document struct {
	Memory            *memory.Ref `json:",omitempty"`
	OwnerID           core.ID
	Object            core.Object
	Source            Source
	AssistantID       core.ID
	Title, Text       string
	FileID            core.ID
	Version           core.Version
	Archived, Deleted bool
}
type Options struct {
	Memories   memory.Service
	Authorizer policy.Authorizer
	Now        func() time.Time
	NewID      func() core.ID
	Documents  func(context.Context, core.ID) ([]Document, error)
}
type service struct {
	store   persistence.Store
	options Options
}

func NewService(store persistence.Store, options Options) Service {
	return &service{store: store, options: options}
}
func NewIndexer(store persistence.Store, options Options) Indexer {
	return &service{store: store, options: options}
}
func validSource(source Source) bool {
	return source == Messages || source == Assistants || source == Resources || source == Tasks || source == Memories
}
func (s *service) documents(ctx context.Context, owner core.ID) ([]Document, error) {
	if s.options.Documents != nil {
		return s.options.Documents(ctx, owner)
	}
	return s.defaultDocuments(ctx, owner)
}
func (s *service) defaultDocuments(ctx context.Context, owner core.ID) ([]Document, error) {
	result := []Document{}
	err := s.walkDocuments(ctx, owner, Query{IncludeArchived: true}, func(doc Document) error {
		result = append(result, doc)
		return nil
	})
	return result, err
}
func (s *service) Search(ctx context.Context, p core.Principal, q Query) (Results, error) {
	result := Results{Counts: map[Source]int{}}
	for _, source := range q.Sources {
		if !validSource(source) {
			return result, invalid("source")
		}
	}
	if len(q.Text) > 4096 {
		return result, invalid("text")
	}
	if _, err := q.Page.Normalize(); err != nil {
		return result, err
	}
	if err := s.authorize(ctx, p, core.Object{Kind: objectKind}, policy.Read, false); err != nil {
		return result, err
	}
	hits := []Hit{}
	needle := strings.ToLower(strings.TrimSpace(q.Text))
	accept := func(doc Document) error {
		if err := persistence.CheckContext(ctx); err != nil {
			return err
		}
		if doc.OwnerID != p.OwnerID || doc.Deleted || doc.Archived && !q.IncludeArchived || q.AssistantID != "" && q.AssistantID != doc.AssistantID || !selectedSource(q, doc.Source) {
			return nil
		}
		if doc.Source == Memories {
			if p.Kind == core.ActorAssistant && doc.AssistantID != "" && doc.AssistantID != p.SubjectID {
				return nil
			}
		}
		// Collect matching candidates without holding a database transaction.
		// Authorize them after all potentially slow source reads finish.
		text := strings.ToLower(doc.Title + " " + doc.Text)
		if needle != "" && !strings.Contains(text, needle) {
			return nil
		}
		snippet := make([]rune, 0, 240)
		for _, char := range doc.Text {
			if len(snippet) == 240 {
				break
			}
			snippet = append(snippet, char)
		}
		score := float64(strings.Count(text, needle))
		if needle == "" {
			score = 1
		}
		hits = append(hits, Hit{Memory: doc.Memory, Source: doc.Source, Object: doc.Object, AssistantID: doc.AssistantID, Title: doc.Title, Snippet: string(snippet), FileID: doc.FileID, Score: score})
		return nil
	}
	if s.options.Documents != nil {
		documents, err := s.options.Documents(ctx, p.OwnerID)
		if err != nil {
			return result, err
		}
		for _, doc := range documents {
			if err := accept(doc); err != nil {
				return result, err
			}
		}
	} else if err := s.walkDocuments(ctx, p.OwnerID, q, accept); err != nil {
		return result, err
	}
	if s.options.Memories != nil && selectedSource(q, Memories) {
		scopes := []memory.Scope{memory.Assistant}
		if q.AssistantID == "" {
			scopes = append(scopes, memory.Global)
		}
		for _, scope := range scopes {
			query := memory.Query{Scope: scope, AssistantID: q.AssistantID, Page: core.PageRequest{Limit: 200}}
			total := 0
			for {
				page, err := s.options.Memories.List(ctx, p, query)
				if err != nil {
					return result, err
				}
				for _, m := range page.Items {
					total += len(m.Text)
					if total > 8<<20 {
						return result, core.NewError(core.FailedPrecondition, "memory content scan limit exceeded")
					}
					if err := accept(Document{OwnerID: p.OwnerID, Object: core.Object{Kind: "memory"}, Memory: &m.Ref, Source: Memories, AssistantID: m.AssistantID, Title: m.Path, Text: m.Text}); err != nil {
						return result, err
					}
				}
				if page.NextCursor == "" {
					break
				}
				query.Page.Cursor = page.NextCursor
			}
		}
	}
	// A grant can change while files or later batches are being read. Check
	// every matching object now, before publishing counts or ranked results.
	allowed := hits[:0]
	for _, hit := range hits {
		if err := persistence.CheckContext(ctx); err != nil {
			return result, err
		}
		target := hit.Object
		if hit.Source == Memories {
			target = core.Object{Kind: "memory"}
		}
		if err := s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: target, Action: policy.Read}); err != nil {
			if core.CodeOf(err) == core.Forbidden || core.CodeOf(err) == core.NotFound {
				continue
			}
			return result, err
		}
		allowed = append(allowed, hit)
		result.Counts[hit.Source]++
	}
	hits = allowed

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		a, b := hits[i], hits[j]
		key := func(h Hit) string {
			if h.Memory != nil {
				return "memory/" + string(h.Memory.Scope) + "/" + string(h.Memory.AssistantID) + "/" + h.Memory.Path
			}
			return h.Object.Kind + string(h.Object.ID)
		}
		return key(a) < key(b)
	})
	page := q.Page
	q.Page = core.PageRequest{}
	var err error
	result.Hits, err = paginate(hits, p.OwnerID, q, page)
	if err != nil {
		return result, err
	}
	return result, s.authorize(ctx, p, core.Object{Kind: objectKind}, policy.Read, false)
}

func indexKey(owner core.ID, object core.Object) string {
	return persistence.Key(owner, core.ID(object.Kind+"/"+string(object.ID)))
}
func (s *service) Apply(ctx context.Context, change Change) error {
	if change.Object.Kind == "memory" {
		return nil
	}
	if change.OwnerID.Validate() != nil || change.Object.ID.Validate() != nil || change.Version == 0 {
		return invalid("change")
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := indexKey(change.OwnerID, change.Object)
		var previous Document
		err := tx.Get(ctx, indexBucket, key, &previous)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if err == nil && previous.Version >= change.Version {
			return nil
		}
		if change.Deleted {
			return tx.Put(ctx, indexBucket, key, Document{OwnerID: change.OwnerID, Object: change.Object, Version: change.Version, Deleted: true})
		}
		docs, err := s.documents(ctx, change.OwnerID)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			if doc.Object == change.Object {
				if doc.OwnerID != change.OwnerID {
					return forbidden()
				}
				if doc.Version != change.Version {
					return nil
				}
				return tx.Put(ctx, indexBucket, key, doc)
			}
		}
		return nil
	})
}
func (s *service) Rebuild(ctx context.Context, p core.Principal) (core.ID, error) {
	var id core.ID
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := s.authorize(ctx, p, core.Object{Kind: objectKind}, policy.Manage, true); err != nil {
			return err
		}
		documents, err := s.documents(ctx, p.OwnerID)
		if err != nil {
			return err
		}
		old, err := tx.Query(ctx, indexBucket, persistence.Query{Prefix: string(p.OwnerID) + "/"})
		if err != nil {
			return err
		}
		for _, record := range old {
			if strings.HasPrefix(record.Key, string(p.OwnerID)+"/") {
				if err = tx.Delete(ctx, indexBucket, record.Key); err != nil {
					return err
				}
			}
		}
		var processed int64
		for _, doc := range documents {
			if doc.Source == Memories {
				continue
			}
			if doc.OwnerID != p.OwnerID {
				return forbidden()
			}
			if err = tx.Put(ctx, indexBucket, indexKey(p.OwnerID, doc.Object), doc); err != nil {
				return err
			}
			processed++
		}
		id = s.id()
		return tx.Put(ctx, statusBucket, persistence.Key(p.OwnerID, id), IndexStatus{ID: id, State: "completed", Processed: processed})
	})
	return id, err
}
func (s *service) Status(ctx context.Context, p core.Principal, id core.ID) (IndexStatus, error) {
	var result IndexStatus
	err := s.read(ctx, p, id, policy.Read, true, func(ctx context.Context) error {
		var err error
		result, err = persistence.Load[IndexStatus](ctx, s.store, statusBucket, persistence.Key(p.OwnerID, id))
		return err
	})
	return result, err
}
