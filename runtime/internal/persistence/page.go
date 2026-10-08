package persistence

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"molr.ai/runtime/internal/core"
	"strings"
)

// Page uses a query-bound keyset cursor and bounded reads. accept is reserved
// for predicates not representable in SQL, such as Unicode case-folded text.
func Page[T any](ctx context.Context, store Store, bucket string, q Query, request core.PageRequest, binding any, accept func(T) bool) (core.Page[T], error) {
	out := core.Page[T]{Items: []T{}}
	p, err := request.Normalize()
	if err != nil {
		return out, err
	}
	if q.OrderBy == "" {
		q.OrderBy = "key"
	}
	if q.OrderBy != "key" && q.ThenBy == "" {
		q.ThenBy = "key"
		q.ThenDescending = q.Descending
	}
	q.Unordered = false
	raw, err := json.Marshal(struct {
		Bucket  string
		Query   Query
		Binding any
	}{bucket, q, binding})
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	scope := hex.EncodeToString(sum[:])
	var cursor struct {
		Scope string
		After []any
	}
	if p.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Scope != scope || len(cursor.After) == 0 {
			return out, core.Invalid("cursor", "query mismatch")
		}
	}
	q.After = cursor.After
	var last []any
	for {
		q.Limit = p.Limit + 1
		var rows []Record
		err := store.Read(ctx, func(ctx context.Context, tx Tx) error {
			var err error
			rows, err = tx.Query(ctx, bucket, q)
			return err
		})
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			position, err := pagePosition(row, q)
			if err != nil {
				return out, err
			}
			q.After = position
			value, err := Decode[T](row)
			if err != nil {
				return out, err
			}
			if accept != nil && !accept(value) {
				continue
			}
			if len(out.Items) == p.Limit {
				cursor.Scope, cursor.After = scope, last
				raw, _ := json.Marshal(cursor)
				out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
				return out, nil
			}
			out.Items = append(out.Items, value)
			last = position
		}
		if len(rows) < q.Limit {
			return out, nil
		}
	}
}
func pagePosition(row Record, q Query) ([]any, error) {
	fields := []string{}
	if q.OrderBy != "key" {
		fields = append(fields, q.OrderBy)
	}
	if q.ThenBy != "" && q.ThenBy != "key" {
		fields = append(fields, q.ThenBy)
	}
	position := []any{}
	if len(fields) > 0 {
		var value map[string]any
		if err := json.Unmarshal(row.Data, &value); err != nil {
			return nil, err
		}
		for _, field := range fields {
			var v any = value
			for _, part := range strings.Split(field, ".") {
				m, ok := v.(map[string]any)
				if !ok {
					return nil, core.Invalid("cursor", "missing order field")
				}
				v = m[part]
			}
			if v == nil {
				return nil, core.Invalid("cursor", "missing order field")
			}
			position = append(position, v)
		}
	}
	return append(position, row.Key), nil
}
