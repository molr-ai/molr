package files

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const Bucket = "storage.files"

type repository struct{ store persistence.Store }

func NewRepository(store persistence.Store) Repository { return &repository{store: store} }
func (r *repository) Get(ctx context.Context, owner, id core.ID) (File, error) {
	return persistence.Load[File](ctx, r.store, Bucket, persistence.Key(owner, id))
}
func (r *repository) Insert(ctx context.Context, file File) error {
	if file.ID.Validate() != nil || file.OwnerID.Validate() != nil {
		return core.Invalid("file", "invalid identity")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var previous File
		err := tx.Get(ctx, Bucket, persistence.Key(file.OwnerID, file.ID), &previous)
		if err == nil {
			return persistence.Conflict("file already exists")
		}
		if !persistence.Missing(err) {
			return err
		}
		return tx.Put(ctx, Bucket, persistence.Key(file.OwnerID, file.ID), file)
	})
}
func (r *repository) SetState(ctx context.Context, owner, id core.ID, state State) error {
	if state != Staged && state != Ready && state != Failed {
		return core.Invalid("state", "invalid file state")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var f File
		if err := tx.Get(ctx, Bucket, persistence.Key(owner, id), &f); err != nil {
			return err
		}
		f.State = state
		return tx.Put(ctx, Bucket, persistence.Key(owner, id), f)
	})
}
func (r *repository) Delete(ctx context.Context, owner, id core.ID) error {
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Delete(ctx, Bucket, persistence.Key(owner, id))
	})
}
