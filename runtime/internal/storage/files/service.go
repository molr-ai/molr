package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
)

type Options struct {
	Authorizer       policy.Authorizer
	AuthorizeChannel func(context.Context, core.ID, core.ID, core.ID) error
	Now              func() time.Time
	MaxUploadBytes   int64
	DiskQuota        func() int64
}
type fileService struct {
	store   persistence.Store
	objects Objects
	repo    Repository
	options Options
	mu      sync.Mutex
}
type uploadReceipt struct {
	Digest string
	FileID core.ID
}

func NewService(store persistence.Store, objects Objects, options Options) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxUploadBytes <= 0 {
		options.MaxUploadBytes = 20 << 20
	}
	if options.DiskQuota == nil {
		options.DiskQuota = func() int64 { return 10 << 30 }
	}
	return &fileService{store: store, objects: objects, repo: NewRepository(store), options: options}
}
func (s *fileService) require(ctx context.Context, p core.Principal, id core.ID, action policy.Action) error {
	if err := persistence.CheckContext(ctx); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if s.options.Authorizer == nil {
		return core.NewError(core.Forbidden, "file authorization unavailable")
	}
	return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "file", ID: id}, Action: action})
}
func (s *fileService) Upload(ctx context.Context, p core.Principal, request UploadRequest) (File, error) {
	return s.upload(ctx, p, request, "", func(ctx context.Context) error {
		if err := s.require(ctx, p, "", policy.Write); err != nil {
			return err
		}
		if request.TaskID != "" {
			return s.options.Authorizer.Require(ctx, p, policy.CheckRequest{Target: core.Object{Kind: "task", ID: request.TaskID}, Action: policy.Write})
		}
		return nil
	})
}
func (s *fileService) upload(ctx context.Context, p core.Principal, request UploadRequest, channelScope string, authorize func(context.Context) error) (File, error) {
	if err := authorize(ctx); err != nil {
		return File{}, err
	}
	if err := request.Mutation.Validate(false); err != nil {
		return File{}, err
	}
	if request.Size < 0 || request.Size > s.options.MaxUploadBytes || request.Body == nil {
		return File{}, core.Invalid("upload", "invalid upload size or stream")
	}
	name := filepath.Base(strings.ReplaceAll(request.Name, "\\", "/"))
	if name == "" || name == "." || name == "/" || strings.ContainsAny(name, "\x00\r\n") {
		return File{}, core.Invalid("name", "invalid filename")
	}
	if request.TaskID != "" && request.TaskID.Validate() != nil {
		return File{}, core.Invalid("task_id", "invalid task")
	}
	if request.MediaType == "" {
		request.MediaType = "application/octet-stream"
	}
	if _, _, err := mime.ParseMediaType(request.MediaType); err != nil || strings.ContainsAny(request.MediaType, "\r\n") {
		return File{}, core.Invalid("media_type", "invalid media type")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return File{}, err
	}
	id := core.ID(hex.EncodeToString(random[:]))
	staged, err := s.objects.Put(ctx, id, request.Body, request.Size)
	if err != nil {
		return File{}, err
	}
	published := false
	defer func() {
		if !published {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.objects.Remove(cleanup, id)
		}
	}()
	if staged.Size != request.Size {
		return File{}, core.Invalid("size", "declared and actual sizes differ")
	}
	// Serialize publication/quota checks; streaming above holds no SQL transaction.
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, _ := json.Marshal(struct {
		Name, Type, Hash string
		Size             int64
		TaskID           core.ID
	}{name, request.MediaType, staged.SHA256, request.Size, request.TaskID})
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	receiptScope := channelScope + request.Mutation.IdempotencyKey
	if p.Kind == core.ActorAssistant {
		receiptScope = "assistant/" + string(p.SubjectID) + "/" + receiptScope
	}
	key := persistence.Key(p.OwnerID, core.ID(receiptScope))
	var result File
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if err := authorize(ctx); err != nil {
			return err
		}
		var previous uploadReceipt
		err := tx.Get(ctx, "files.idempotency", key, &previous)
		if err == nil {
			if previous.Digest != digest {
				return persistence.Conflict("idempotency key payload mismatch")
			}
			return tx.Get(ctx, Bucket, persistence.Key(p.OwnerID, previous.FileID), &result)
		}
		if !persistence.Missing(err) {
			return err
		}
		used, err := tx.Sum(ctx, Bucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("State", Ready)}}, "Size")
		if err != nil {
			return err
		}
		quota := s.options.DiskQuota()
		if quota < 0 || staged.Size > quota || used > quota-staged.Size {
			return core.NewError(core.FailedPrecondition, "file storage quota exceeded")
		}
		result = File{ID: id, OwnerID: p.OwnerID, TaskID: request.TaskID, Name: name, MediaType: request.MediaType, Size: staged.Size, SHA256: staged.SHA256, State: Ready, CreatedAt: s.options.Now()}
		if p.Kind == core.ActorAssistant {
			result.AssistantID = p.SubjectID
		}
		if err := s.objects.Commit(ctx, staged); err != nil {
			return err
		}
		if err := tx.Put(ctx, Bucket, persistence.Key(p.OwnerID, id), result); err != nil {
			return err
		}
		if channelScope != "" {
			if err := tx.Put(ctx, "files.channel", persistence.Key(p.OwnerID, id), channelScope); err != nil {
				return err
			}
		}
		return tx.Put(ctx, "files.idempotency", key, uploadReceipt{Digest: digest, FileID: id})
	})
	if err != nil {
		return File{}, err
	}
	published = result.ID == id
	return result, nil
}
func (s *fileService) Get(ctx context.Context, p core.Principal, id core.ID) (File, error) {
	if err := s.require(ctx, p, id, policy.Read); err != nil {
		return File{}, err
	}
	file, err := s.repo.Get(ctx, p.OwnerID, id)
	if err != nil {
		return File{}, err
	}
	if file.State != Ready {
		return File{}, core.NewError(core.NotFound, "file not published")
	}
	return file, nil
}
func (s *fileService) Open(ctx context.Context, p core.Principal, id core.ID) (Download, error) {
	f, err := s.Get(ctx, p, id)
	if err != nil {
		return Download{}, err
	}
	body, err := s.objects.Open(ctx, id)
	if err != nil {
		return Download{}, err
	}
	return Download{File: f, Body: body}, nil
}
func (s *fileService) Delete(ctx context.Context, p core.Principal, id core.ID) error {
	if err := s.require(ctx, p, id, policy.Write); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var file File
		if err := tx.Get(ctx, Bucket, persistence.Key(p.OwnerID, id), &file); err != nil {
			return err
		}
		return tx.Delete(ctx, Bucket, persistence.Key(p.OwnerID, id))
	})
	if err != nil {
		return err
	}
	return s.objects.Remove(ctx, id)
}
