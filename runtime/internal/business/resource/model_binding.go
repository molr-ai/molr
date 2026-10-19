package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/catalog"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// ModelID is stable for one provider resource and model name.
func ModelID(resourceID core.ID, name string) core.ID {
	sum := sha256.Sum256([]byte(string(resourceID) + "\x00" + name))
	return core.ID("model_" + hex.EncodeToString(sum[:16]))
}

// Derived memberships stay private to storage. Binding's public/internal DTO
// does not accept them, and every binding write derives them from current models.
type storedBinding struct {
	Binding
	ModelIDs []core.ID
}

func putBinding(ctx context.Context, tx persistence.Tx, owner core.ID, binding Binding) error {
	row := storedBinding{Binding: binding, ModelIDs: make([]core.ID, 0, len(binding.Models))}
	for _, model := range binding.Models {
		row.ModelIDs = append(row.ModelIDs, ModelID(binding.ResourceID, model.Name))
	}
	return tx.Put(ctx, bindingBucket, persistence.Key(owner, binding.ResourceID), row)
}

func (r *repository) BindingForModel(ctx context.Context, owner, id core.ID) (Binding, error) {
	owner = catalog.Scope
	rows, err := persistence.Select[Binding](ctx, r.store, bindingBucket, persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerKey", owner), {Field: "ModelIDs", Op: "has", Value: string(id)}}, Limit: 1, Unordered: true})
	if err != nil {
		return Binding{}, err
	}
	if len(rows) == 0 {
		return Binding{}, missing()
	}
	return rows[0], nil
}
