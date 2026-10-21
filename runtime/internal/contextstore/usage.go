package contextstore

import (
	"context"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

// Usage measures encoded object values, excluding database/index overhead and
// external reference metadata. It never exposes message contents or object IDs.
type Usage struct {
	Bytes    int64
	Messages int64
}

// AccountUsage traverses only graph metadata in bounded batches. Payload bytes
// are measured by storage without decoding message bodies into the Runtime.
func AccountUsage(ctx context.Context, tx persistence.Tx, owner core.ID) (Usage, error) {
	var out Usage
	if err := owner.Validate(); err != nil {
		return out, err
	}
	seen := map[string]bool{Root: true, "": true}
	pending := []string{}
	enqueue := func(id string) {
		if !seen[id] {
			seen[id] = true
			pending = append(pending, id)
		}
	}
	refs, err := tx.Query(ctx, references, persistence.Query{Unordered: true, Fields: []string{"DocumentID"}, Where: []persistence.Filter{persistence.Eq("OwnerID", owner)}})
	if err != nil {
		return out, err
	}
	for _, row := range refs {
		r, err := persistence.Decode[reference](row)
		if err != nil {
			return Usage{}, err
		}
		enqueue(r.DocumentID)
	}
	for len(pending) > 0 {
		n := min(len(pending), 512)
		batch := pending[:n]
		pending = pending[n:]
		rows, err := tx.Query(ctx, objects, persistence.Query{Unordered: true, IncludeEncodedBytes: true, Fields: []string{"Kind", "Parent", "Trees", "Values"}, Where: []persistence.Filter{{Field: "key", Op: "in", Value: batch}}})
		if err != nil {
			return Usage{}, err
		}
		if len(rows) != len(batch) {
			return Usage{}, core.NewError(core.NotFound, "context object missing")
		}
		for _, row := range rows {
			o, err := persistence.Decode[object](row)
			if err != nil {
				return Usage{}, err
			}
			out.Bytes += row.EncodedBytes
			if o.Kind == "message" {
				out.Messages++
			}
			enqueue(o.Parent)
			for _, s := range o.Trees {
				enqueue(s.NodeID)
			}
			for _, v := range o.Values {
				enqueue(v.ID)
			}
		}
	}
	return out, nil
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
