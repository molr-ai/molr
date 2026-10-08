package credential

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"strconv"
	"strings"
)

type repository struct{ store persistence.Store }

func NewRepository(store persistence.Store) Repository { return &repository{store} }
func (r *repository) Get(ctx context.Context, owner, id core.ID) (EncryptedValue, error) {
	return persistence.LoadCatalog[EncryptedValue](ctx, r.store, "security.credentials", owner, id)
}
func (r *repository) List(ctx context.Context, owner core.ID, p core.PageRequest) (core.Page[Credential], error) {
	owner, err := persistence.CatalogOwner(ctx, r.store, owner)
	if err != nil {
		return core.Page[Credential]{}, err
	}
	rows, err := persistence.Page[EncryptedValue](ctx, r.store, "security.credentials", persistence.Query{Where: []persistence.Filter{persistence.Eq("Metadata.OwnerID", owner)}, Fields: []string{"Metadata"}}, p, owner, nil)
	out := core.Page[Credential]{Items: []Credential{}, NextCursor: rows.NextCursor}
	for _, row := range rows.Items {
		out.Items = append(out.Items, row.Metadata)
	}
	return out, err
}

func (r *repository) Save(ctx context.Context, v EncryptedValue, version core.Version) error {
	if v.Metadata.ID.Validate() != nil || v.Metadata.OwnerID.Validate() != nil || len(v.Ciphertext) == 0 || v.KeyID == "" || v.FormatVersion != 1 {
		return core.Invalid("encrypted_value", "invalid encrypted envelope")
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old EncryptedValue
		key := persistence.Key(v.Metadata.OwnerID, v.Metadata.ID)
		e := tx.Get(ctx, "security.credentials", key, &old)
		if e != nil && !persistence.Missing(e) {
			return e
		}
		if (e == nil && (version == 0 || old.Metadata.Version != version)) || (persistence.Missing(e) && version != 0) {
			return persistence.Conflict("credential version mismatch")
		}
		v.Metadata.Version = version + 1
		return tx.Put(ctx, "security.credentials", key, v)
	})
}
func (r *repository) Delete(ctx context.Context, owner, id core.ID, version core.Version) error {
	var err error
	owner, err = persistence.CatalogOwner(ctx, r.store, owner)
	if err != nil {
		return err
	}
	return r.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var old EncryptedValue
		key := persistence.Key(owner, id)
		if e := tx.Get(ctx, "security.credentials", key, &old); e != nil {
			return e
		}
		if old.Metadata.Version != version {
			return persistence.Conflict("credential version mismatch")
		}
		return tx.Delete(ctx, "security.credentials", key)
	})
}

func pageSlice[T any](items []T, p core.PageRequest, binding string) (core.Page[T], error) {
	p, e := p.Normalize()
	if e != nil {
		return core.Page[T]{}, e
	}
	h := sha256.Sum256([]byte(binding))
	tag := hex.EncodeToString(h[:])
	start := 0
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		if e != nil {
			return core.Page[T]{}, core.Invalid("cursor", "invalid")
		}
		parts := strings.Split(string(b), ":")
		if len(parts) != 2 || parts[0] != tag {
			return core.Page[T]{}, core.Invalid("cursor", "wrong query")
		}
		start, e = strconv.Atoi(parts[1])
		if e != nil || start < 0 || start > len(items) {
			return core.Page[T]{}, core.Invalid("cursor", "invalid position")
		}
	}
	end := start + p.Limit
	if end > len(items) {
		end = len(items)
	}
	out := core.Page[T]{Items: items[start:end]}
	if end < len(items) {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%d", tag, end)))
	}
	return out, nil
}
