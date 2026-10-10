package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"molr.ai/runtime/internal/agent/model"
	"molr.ai/runtime/internal/agent/operation"
	"molr.ai/runtime/internal/business/memory"
	"molr.ai/runtime/internal/business/settings"
	"molr.ai/runtime/internal/business/task"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/im/conversation"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
	"molr.ai/runtime/internal/storage/files"
)

func stableEventID(id core.ID, v core.Version) core.ID {
	sum := sha256.Sum256([]byte(string(id) + "/task/" + strconv.FormatUint(uint64(v), 10)))
	return core.ID(hex.EncodeToString(sum[:]))
}

type reservationRecord = quota.ModelRecord

func checkReservation(ctx context.Context, store persistence.Store, a access, q model.Request, provider model.Provider) error {
	if err := a.execution(ctx, q.Execution); err != nil {
		return err
	}
	r, err := persistence.Load[reservationRecord](ctx, store, "security.reservations", persistence.Key(q.Execution.Principal.OwnerID, q.ReservationID))
	if err != nil {
		return err
	}
	if r.OwnerID != q.Execution.Principal.OwnerID || r.AssistantID != q.Execution.AssistantID || r.ExecutionID != q.Execution.Principal.ExecutionID || r.Reservation.OperationID != q.Execution.OperationID || r.Reservation.TaskID != q.Execution.TaskID || r.Reservation.State != "reserved" {
		return denied()
	}
	estimate, err := estimateProvider(provider, q.MaxOutputTokens)
	if err != nil {
		return err
	}
	if r.Reservation.Reserved.Currency != estimate.Currency || r.Reservation.Reserved.Micros < estimate.Micros {
		return core.NewError(core.BudgetExceeded, "reservation no longer covers model pricing")
	}

	return nil
}
func settleReceipt(ctx context.Context, store persistence.Store, budget policy.Budget, e core.Execution, receipt model.Receipt) error {
	r, err := quota.ModelByOperation(ctx, store, e.Principal.OwnerID, e.OperationID)
	if err != nil {
		return err
	}
	if r == nil {
		return core.NewError(core.NotFound, "reservation unavailable")
	}
	_, err = budget.Settle(ctx, e, policy.SettleRequest{ReservationID: r.Reservation.ID, OperationID: e.OperationID, ReceiptID: receipt.ID, Actual: *receipt.Charged})
	return err
}
func reconcileOperation(ctx context.Context, store persistence.Store, op operation.Operation) (operation.Completion, error) {
	r, err := model.NewReceipts(store).ByOperation(ctx, op.OwnerID, op.ID)
	if err != nil {
		return operation.Completion{}, core.NewError(core.FailedPrecondition, "external effect requires independent evidence")
	}
	if !r.OutcomeKnown || r.Charged == nil {
		return operation.Completion{}, core.NewError(core.FailedPrecondition, "provider outcome remains unknown")
	}
	return operation.Completion{ID: op.ID, ExpectedVersion: op.Version, State: operation.Succeeded, ReceiptID: r.ID}, nil
}
func verifyDelivery(ctx context.Context, store persistence.Store, e core.Execution, d task.Delivery) error {
	for _, id := range d.ArtifactIDs {
		f, err := files.NewRepository(store).Get(ctx, e.Principal.OwnerID, id)
		if err != nil {
			return err
		}
		if f.TaskID != d.TaskID || f.State != files.Ready {
			return denied()
		}
	}
	for _, check := range d.Checks {
		if !check.Passed || len(check.EvidenceIDs) == 0 {
			return core.NewError(core.FailedPrecondition, "verification evidence required")
		}
		verified := false
		for _, id := range check.EvidenceIDs {
			op, err := operation.NewRepository(store).Get(ctx, e.Principal.OwnerID, id)
			if err != nil {
				return err
			}
			if op.TaskID != d.TaskID || op.AssistantID != e.AssistantID || op.State != operation.Succeeded || op.Action != "verify" {
				continue
			}
			var evidence struct {
				Criterion string
				Passed    bool
			}
			if json.Unmarshal(op.Result, &evidence) == nil && evidence.Passed && evidence.Criterion == check.Criterion {
				verified = true
			}
		}
		if !verified {
			return core.NewError(core.FailedPrecondition, "criterion has no trusted verification result")
		}
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, core.Invalid("export", "export size limit exceeded")
	}
	return b.Buffer.Write(p)
}
func saveExport(ctx context.Context, service files.Service, p core.Principal, bundle settings.ExportBundle, memories ...*memory.Files) (core.ID, error) {
	buf := &limitedBuffer{limit: 20 << 20}
	archive := zip.NewWriter(buf)
	manifest, err := archive.Create("records.json")
	if err != nil {
		return "", err
	}
	if err = json.NewEncoder(manifest).Encode(bundle.Records); err != nil {
		return "", err
	}
	if len(memories) > 0 {
		if err = memories[0].Export(ctx, p.OwnerID, func(name string, data []byte) error {
			entry, e := archive.Create("memory/" + name)
			if e != nil {
				return e
			}
			_, e = entry.Write(data)
			return e
		}); err != nil {
			return "", err
		}
	}
	if bundle.IncludeArtifacts {
		for _, raw := range bundle.Records[files.Bucket] {
			var f files.File
			if err = json.Unmarshal(raw, &f); err != nil {
				return "", err
			}
			if f.OwnerID != p.OwnerID || f.State != files.Ready {
				continue
			}
			if f.Size > 20<<20 {
				return "", core.Invalid("export", "artifact exceeds export limit")
			}
			download, err := service.Open(ctx, p, f.ID)
			if err != nil {
				return "", err
			}
			entry, err := archive.Create("files/" + string(f.ID))
			if err != nil {
				_ = download.Body.Close()
				return "", err
			}
			copied, copyErr := io.Copy(entry, io.LimitReader(download.Body, f.Size+1))
			err = copyErr
			if err == nil && copied != f.Size {
				err = core.NewError(core.FailedPrecondition, "artifact size changed")
			}
			_ = download.Body.Close()
			if err != nil {
				return "", err
			}
		}
	}
	if err = archive.Close(); err != nil {
		return "", err
	}
	file, err := service.Upload(ctx, p, files.UploadRequest{Mutation: core.Mutation{IdempotencyKey: "export-" + string(bundle.ID)}, Name: "molr-export.zip", MediaType: "application/zip", Size: int64(buf.Len()), Body: bytes.NewReader(buf.Bytes())})
	return file.ID, err
}
func cleanupObject(ctx context.Context, store persistence.Store, a access, p core.Principal, o core.Object, apply bool) error {
	if err := a.owns(ctx, p, o); err != nil {
		return err
	}
	if err := p.RequireOwner(); err != nil {
		return err
	}
	buckets := map[string]string{"file": files.Bucket, "message": "im.messages", "conversation": "im.conversations", "task": "business.tasks"}
	bucket, ok := buckets[o.Kind]
	if !ok {
		return core.Invalid("cleanup", "unsupported cleanup object")
	}
	if o.Kind == "task" {
		v, err := task.NewRepository(store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		if v.State != task.Completed && v.State != task.Canceled && v.State != task.Failed {
			return core.NewError(core.FailedPrecondition, "active task cannot be removed")
		}
	}
	if o.Kind == "conversation" {
		v, err := conversation.NewRepository(store).Get(ctx, p.OwnerID, o.ID)
		if err != nil {
			return err
		}
		if !v.Archived {
			return core.NewError(core.FailedPrecondition, "archive conversation before cleanup")
		}
	}
	if !apply {
		return nil
	}

	return store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		if o.Kind == "conversation" || o.Kind == "message" {
			return clearMessages(ctx, tx, p.OwnerID, o)
		}
		if o.Kind == "task" {
			rows, err := tx.Query(ctx, "business.revisions", persistence.Query{Prefix: persistence.Key(p.OwnerID, o.ID) + "/"})
			if err != nil {
				return err
			}
			prefix := persistence.Key(p.OwnerID, o.ID) + "/"
			for _, row := range rows {
				if strings.HasPrefix(row.Key, prefix) {
					if err = tx.Delete(ctx, "business.revisions", row.Key); err != nil {
						return err
					}
				}
			}
		}
		if o.Kind == "file" {
			if err := tx.Put(ctx, "files.gc", persistence.Key(p.OwnerID, o.ID), o.ID); err != nil {
				return err
			}
		}
		return tx.Delete(ctx, bucket, persistence.Key(p.OwnerID, o.ID))
	})
}
func clearMessages(ctx context.Context, tx persistence.Tx, owner core.ID, o core.Object) error {
	chatID := o.ID
	if o.Kind == "message" {
		var m conversation.Message
		if err := tx.Get(ctx, "im.messages", persistence.Key(owner, o.ID), &m); err != nil {
			return err
		}
		chatID = m.ConversationID
	}
	var c conversation.Conversation
	if err := tx.Get(ctx, "im.conversations", persistence.Key(owner, chatID), &c); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "im.messages", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", owner), persistence.Eq("ConversationID", chatID)}})
	if err != nil {
		return err
	}
	c.LastMessageID = ""
	c.UnreadCount = 0
	var newest uint64
	removed := map[core.ID]bool{}
	for _, row := range rows {
		if !strings.HasPrefix(row.Key, string(owner)+"/") {
			continue
		}
		m, err := persistence.Decode[conversation.Message](row)
		if err != nil {
			return err
		}
		if m.ConversationID != chatID {
			continue
		}
		if m.Sequence > c.LastSequence {
			c.LastSequence = m.Sequence
		}
		if o.Kind == "conversation" || m.ID == o.ID {
			if err = tx.Delete(ctx, "im.messages", row.Key); err != nil {
				return err
			}
			removed[m.ID] = true
			continue
		}
		if m.Sequence > newest {
			newest = m.Sequence
			c.LastMessageID = m.ID
		}
		if m.Sender == conversation.Assistant && m.Sequence > c.LastReadSequence {
			c.UnreadCount++
		}
	}
	// Remove still-pending notifications for deleted user messages atomically.
	pending, err := tx.Query(ctx, "im.outbox", persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("ConversationID", chatID)}})
	if err != nil {
		return err
	}
	for _, row := range pending {
		if !strings.HasPrefix(row.Key, string(owner)+"/") {
			continue
		}
		m, err := persistence.Decode[conversation.MessageCreated](row)
		if err != nil {
			return err
		}
		if removed[m.MessageID] {
			if err = tx.Delete(ctx, "im.outbox", row.Key); err != nil {
				return err
			}
		}
	}
	c.Version++
	return tx.Put(ctx, "im.conversations", persistence.Key(owner, chatID), c)
}
func collectFiles(ctx context.Context, store persistence.Store, objects files.Objects) error {
	var rows []persistence.Record
	err := store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		// Every pending deletion must be attempted, including failures from earlier passes.
		rows, err = tx.Scan(ctx, "files.gc")
		return err
	})
	if err != nil {
		return err
	}
	const batchSize = 128
	completed := make([]string, 0, batchSize)
	flush := func() error {
		if err := persistence.CheckContext(ctx); err != nil {
			return err
		}
		if len(completed) == 0 {
			return nil
		}
		if err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
			for _, key := range completed {
				if err := tx.Delete(ctx, "files.gc", key); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		completed = completed[:0]
		return nil
	}
	for _, row := range rows {
		if err := persistence.CheckContext(ctx); err != nil {
			return err
		}
		id, err := persistence.Decode[core.ID](row)
		if err != nil {
			return err
		}
		if id.Validate() != nil {
			return core.NewError(core.Internal, "invalid garbage collection record")
		}
		if err := objects.Remove(ctx, id); err != nil {
			if canceled := persistence.CheckContext(ctx); canceled != nil {
				return canceled
			}
			continue
		}
		// Only confirmed removals are acknowledged. An interrupted/failed batch
		// stays queued; Remove is idempotent when its object is already absent.
		completed = append(completed, row.Key)
		if len(completed) == batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}
