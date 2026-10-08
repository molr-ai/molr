package persistence

import (
	"context"
	"time"
)

// Query is a trusted repository query, never an HTTP request or SQL fragment.
// Where is conjunctive; Any adds a disjunction of conjunctive alternatives.
// Field names are validated by the adapter. Limit zero means all matching rows.
type Query struct {
	Unordered      bool     // Aggregations do not require a key sort.
	Fields         []string // Optional projection; omitted fields are never loaded into the caller.
	Prefix         string
	Where          []Filter
	Any            [][]Filter
	OrderBy        string
	ThenBy         string
	ThenDescending bool
	After          []any
	Descending     bool
	Limit          int
}
type Filter struct {
	Field, Op string
	Value     any
}

func Eq(field string, value any) Filter { return Filter{field, "=", value} }

func Select[T any](ctx context.Context, store Store, bucket string, q Query) ([]T, error) {
	values := []T{}
	err := store.Read(ctx, func(ctx context.Context, tx Tx) error {
		rows, err := tx.Query(ctx, bucket, q)
		if err != nil {
			return err
		}
		for _, row := range rows {
			value, err := Decode[T](row)
			if err != nil {
				return err
			}
			values = append(values, value)
		}
		return nil
	})
	return values, err
}

// TimeKey preserves instant ordering, including offsets and nanoseconds, in private indexes.
func TimeKey(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000000000Z") }
