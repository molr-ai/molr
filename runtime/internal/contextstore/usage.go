package contextstore

import (
	"context"
	"encoding/json"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Usage measures encoded object values, excluding database/index overhead and
// external reference metadata. It never exposes message contents or object IDs.
type Usage struct {
	Bytes    int64
	Messages int64
}

func (u *Usage) add(raw json.RawMessage) (object, error) {
	var o object
	if err := json.Unmarshal(raw, &o); err != nil {
		return o, err
	}
	u.Bytes += int64(len(raw))
	if o.Kind == "message" {
		u.Messages++
	}
	return o, nil
}

// AccountUsage counts the union reachable from this owner's references. Shared
// prefixes and documents count once within an account, regardless of ref count.
func AccountUsage(ctx context.Context, tx persistence.Tx, owner core.ID) (Usage, error) {
	var out Usage
	if err := owner.Validate(); err != nil {
		return out, err
	}
	seen := map[string]bool{Root: true}
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}, OrderBy: "key", Limit: 200}
	for {
		rows, err := tx.Query(ctx, references, q)
		if err != nil {
			return Usage{}, err
		}
		for _, row := range rows {
			r, err := persistence.Decode[reference](row)
			if err != nil {
				return Usage{}, err
			}
			pending := []string{r.DocumentID}
			for len(pending) > 0 {
				if err := persistence.CheckContext(ctx); err != nil {
					return Usage{}, err
				}
				id := pending[len(pending)-1]
				pending = pending[:len(pending)-1]
				if seen[id] {
					continue
				}
				seen[id] = true
				var raw json.RawMessage
				if err := tx.Get(ctx, objects, id, &raw); err != nil {
					return Usage{}, err
				}
				o, err := out.add(raw)
				if err != nil {
					return Usage{}, err
				}
				if o.Parent != "" {
					pending = append(pending, o.Parent)
				}
				for _, s := range o.Trees {
					pending = append(pending, s.NodeID)
				}
				for _, v := range o.Values {
					pending = append(pending, v.ID)
				}
			}
		}
		if len(rows) < q.Limit {
			return out, nil
		}
		q.After = []any{rows[len(rows)-1].Key}
	}
}

// GlobalUsage counts each stored object once, including objects awaiting GC.
// The caller must authorize administrator access before invoking it.
func GlobalUsage(ctx context.Context, tx persistence.Tx) (Usage, error) {
	bytes, err := tx.EncodedBytes(ctx, objects, persistence.Query{})
	if err != nil {
		return Usage{}, err
	}
	messages, err := tx.Count(ctx, objects, persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", "message")}})
	if err != nil {
		return Usage{}, err
	}
	return Usage{Bytes: bytes, Messages: messages}, nil
}
