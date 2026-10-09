package contextstore

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

type readCounter struct {
	persistence.Tx
	gets, chains *int
}

func (t readCounter) Get(ctx context.Context, b, k string, out any) error {
	*t.gets++
	return t.Tx.Get(ctx, b, k, out)
}
func (t readCounter) Ancestors(ctx context.Context, b, k, p string, n int) ([]persistence.Record, error) {
	*t.chains++
	return t.Tx.Ancestors(ctx, b, k, p, n)
}
func TestRecursiveReadUsesOneChainQueryAndRejectsMissingNodes(t *testing.T) {
	s := fixture(t)
	save(t, s, ref("alice", "one"), doc("system", "one", "two"))
	var r reference
	write(t, s, func(ctx context.Context, tx persistence.Tx) error {
		key, _ := refKey(ref("alice", "one"))
		return tx.Get(ctx, references, key, &r)
	})
	gets, chains := 0, 0
	err := s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var out any
		return Load(ctx, readCounter{tx, &gets, &chains}, ref("alice", "one"), &out)
	})
	if err != nil || gets != 2 || chains != 1 {
		t.Fatal(err, gets, chains)
	}
	var d object
	write(t, s, func(ctx context.Context, tx persistence.Tx) error { return tx.Get(ctx, objects, r.DocumentID, &d) })
	var tail object
	write(t, s, func(ctx context.Context, tx persistence.Tx) error {
		return tx.Get(ctx, objects, d.Trees[0].NodeID, &tail)
	})
	write(t, s, func(ctx context.Context, tx persistence.Tx) error { return tx.Delete(ctx, objects, tail.Parent) })
	err = s.Read(t.Context(), func(ctx context.Context, tx persistence.Tx) error {
		var out any
		return Load(ctx, tx, ref("alice", "one"), &out)
	})
	if core.CodeOf(err) != core.Internal {
		t.Fatal("missing ancestor must fail closed", err)
	}
}

// This comparison intentionally retains the former algorithm only in benchmarks.
func readIndividually(ctx context.Context, tx persistence.Tx, id string, n int) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, n)
	for i := n - 1; i >= 0; i-- {
		var o object
		if err := tx.Get(ctx, objects, id, &o); err != nil {
			return nil, err
		}
		if o.Kind != "message" || nodeID(o.Parent, o.Data) != id {
			return nil, fmt.Errorf("corrupt")
		}
		out[i] = o.Data
		id = o.Parent
	}
	if id != Root {
		return nil, fmt.Errorf("bad root")
	}
	return out, nil
}
func BenchmarkContextRead(b *testing.B) {
	for _, n := range []int{100, 1000, 4096} {
		b.Run(fmt.Sprintf("messages=%d", n), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "read.db")
			s, err := sqlitestore.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			id := Root
			err = s.Write(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
				for i := 0; i < n; i++ {
					raw, _ := canonical([]byte(fmt.Sprintf(`{"Role":"user","Text":"%d %s"}`, i, strings.Repeat("x", 1024))))
					next := nodeID(id, raw)
					if err := insert(ctx, tx, next, object{Kind: "message", Parent: id, Data: raw}); err != nil {
						return err
					}
					id = next
				}
				return nil
			})
			if err != nil {
				b.Fatal(err)
			}
			for _, cold := range []bool{false, true} {
				for _, recursive := range []bool{false, true} {
					mode := "repeated"
					if cold {
						mode = "reopened"
					}
					name := "individual"
					if recursive {
						name = "recursive"
					}
					b.Run(mode+"/"+name, func(b *testing.B) {
						b.ReportAllocs()
						b.ReportMetric(float64(n*1024), "input-B/op")
						for i := 0; i < b.N; i++ {
							active := s
							if cold {
								b.StopTimer()
								active, err = sqlitestore.Open(path)
								if err != nil {
									b.Fatal(err)
								}
								b.StartTimer()
							}
							err = active.Read(b.Context(), func(ctx context.Context, tx persistence.Tx) error {
								var e error
								if recursive {
									_, e = messages(ctx, tx, id, n)
								} else {
									_, e = readIndividually(ctx, tx, id, n)
								}
								return e
							})
							if err != nil {
								b.Fatal(err)
							}
							if cold {
								b.StopTimer()
								active.Close()
								b.StartTimer()
							}
						}
					})
				}
			}
		})
	}
}
