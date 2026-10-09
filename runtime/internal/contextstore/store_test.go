package contextstore

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"molr.ai/runtime/internal/persistence/sqlitestore"
)

func fixture(t *testing.T) *sqlitestore.Store {
	t.Helper()
	s, e := sqlitestore.Open(filepath.Join(t.TempDir(), "context.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func write(t *testing.T, s persistence.Store, fn func(context.Context, persistence.Tx) error) {
	t.Helper()
	if e := s.Write(t.Context(), fn); e != nil {
		t.Fatal(e)
	}
}
func ref(owner, id string) Reference {
	return Reference{OwnerID: core.ID(owner), AssistantID: core.ID("assistant"), Kind: "test", ID: id}
}
func doc(texts ...string) map[string]any {
	list := []any{}
	for i, s := range texts {
		role := "user"
		if i == 0 {
			role = "system"
		}
		list = append(list, map[string]any{"Role": role, "Text": s})
	}
	return map[string]any{"Inputs": list, "Metadata": map[string]any{"value": json.Number("9007199254740993")}}
}
func save(t *testing.T, s persistence.Store, r Reference, d any) {
	write(t, s, func(c context.Context, tx persistence.Tx) error { return Save(c, tx, r, d) })
}
func load(t *testing.T, s persistence.Store, r Reference) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	e := s.Read(t.Context(), func(c context.Context, tx persistence.Tx) error { return Load(c, tx, r, &raw) })
	if e != nil {
		t.Fatal(e)
	}
	return raw
}
func total(t *testing.T, s persistence.Store, bucket string) int64 {
	t.Helper()
	var n int64
	e := s.Read(t.Context(), func(c context.Context, tx persistence.Tx) error {
		var e error
		n, e = tx.Count(c, bucket, persistence.Query{})
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func gc(t *testing.T, s persistence.Store) {
	t.Helper()
	for {
		n, e := Collect(t.Context(), s, 3)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			return
		}
	}
}
func TestSharedPrefixesScopedReferencesAndCollection(t *testing.T) {
	s := fixture(t)
	a, b := ref("alice", "one"), ref("bob", "two")
	save(t, s, a, doc("system", "hello"))
	save(t, s, b, doc("system", "hello", "branch"))
	// Three message nodes plus two different document templates.
	if n := total(t, s, objects); n != 5 {
		t.Fatal(n)
	}
	expected, _ := canonical(mustJSON(t, doc("system", "hello", "branch")))
	if string(load(t, s, b)) != string(expected) {
		t.Fatal("round trip mismatch")
	}
	var raw json.RawMessage
	if e := s.Read(t.Context(), func(c context.Context, tx persistence.Tx) error { return Load(c, tx, ref("alice", "two"), &raw) }); !persistence.Missing(e) {
		t.Fatal("cross-owner read", e)
	}
	write(t, s, func(c context.Context, tx persistence.Tx) error { return Release(c, tx, a) })
	gc(t, s)
	if n := total(t, s, objects); n != 4 {
		t.Fatal("shared ancestor removed or orphan retained", n)
	}
	_ = load(t, s, b)
	save(t, s, b, doc("system", "new branch"))
	gc(t, s)
	if n := total(t, s, objects); n != 3 {
		t.Fatal("old suffix not reclaimed", n)
	}
	write(t, s, func(c context.Context, tx persistence.Tx) error { return Release(c, tx, b) })
	gc(t, s)
	if n := total(t, s, objects); n != 0 {
		t.Fatal("unreachable tree retained", n)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestStableCanonicalIdentityAndRollback(t *testing.T) {
	s := fixture(t)
	r := ref("owner", "one")
	first := json.RawMessage(`{"Inputs":[{"Text":" x ","Role":"system","Calls":[{"Arguments":"{\"a\": 1}","ID":"call"}]}],"a":9007199254740993}`)
	same := json.RawMessage(`{ "a":9007199254740993,"Inputs":[{"Role":"system","Calls":[{"ID":"call","Arguments":"{\"a\": 1}"}],"Text":" x "}]}`)
	save(t, s, r, first)
	n := total(t, s, objects)
	save(t, s, ref("other", "copy"), same)
	if total(t, s, objects) != n {
		t.Fatal("canonical encoding did not deduplicate")
	}
	before := string(load(t, s, r))
	s.FailNextCommit(core.NewError(core.Unavailable, "disk full"))
	e := s.Write(t.Context(), func(c context.Context, tx persistence.Tx) error { return Save(c, tx, r, doc("different")) })
	if e == nil {
		t.Fatal("write failure hidden")
	}
	if string(load(t, s, r)) != before || total(t, s, objects) != n {
		t.Fatal("failed commit changed state")
	}
	s.FailNextCommit(core.NewError(core.Unavailable, "disk full"))
	if e = s.Write(t.Context(), func(c context.Context, tx persistence.Tx) error { return Release(c, tx, r) }); e == nil {
		t.Fatal("release failure hidden")
	}
	gc(t, s)
	if string(load(t, s, r)) != before {
		t.Fatal("rollback lost pin")
	}
}
func TestConcurrentReferencesSurviveGC(t *testing.T) {
	s := fixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 9)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := ref("owner", fmt.Sprint(i))
			errs <- s.Write(t.Context(), func(c context.Context, tx persistence.Tx) error { return Save(c, tx, r, doc("shared", "hello")) })
		}(i)
	}
	wg.Add(1)
	go func() { defer wg.Done(); _, e := Collect(t.Context(), s, 100); errs <- e }()
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	gc(t, s)
	if total(t, s, objects) != 3 {
		t.Fatal("duplicate tree")
	}
	for i := 0; i < 8; i++ {
		_ = load(t, s, ref("owner", fmt.Sprint(i)))
	}
}
func TestMessageFieldsAndEmptyValuesAreLossless(t *testing.T) {
	s := fixture(t)
	value := json.RawMessage(`{"Checkpoint":{"Context":{"Inputs":[{"Role":"assistant","Text":"","Calls":[{"ID":"a","Name":"tool","Arguments":" {} "}],"ProviderState":{"Data":{"x":1}},"Files":[{"ID":"f"}]},{"Role":"tool","ToolCallID":"a","Text":"return"}],"Current":{"Step":2}}},"Request":{"messages":[{"role":"system","content":"system"}]},"empty":{"Inputs":[]},"nil":{"Inputs":null}}`)
	save(t, s, ref("owner", "state"), value)
	expected, _ := canonical(value)
	if string(load(t, s, ref("owner", "state"))) != string(expected) {
		t.Fatal("message field lost")
	}
	var actual, want any
	json.Unmarshal(load(t, s, ref("owner", "state")), &actual)
	json.Unmarshal(value, &want)
	if !reflect.DeepEqual(actual, want) {
		t.Fatal(actual)
	}
}

type observedTx struct {
	persistence.Tx
	lookups int
}

func (t *observedTx) Get(c context.Context, b, k string, out any) error {
	if b == counts {
		t.lookups++
	}
	return t.Tx.Get(c, b, k, out)
}
func TestLongestPrefixUsesBinarySearch(t *testing.T) {
	s := fixture(t)
	list := make([]json.RawMessage, 4096)
	for i := range list {
		list[i] = json.RawMessage(fmt.Sprintf(`{"Role":"user","Text":"%d"}`, i))
	}
	write(t, s, func(c context.Context, tx persistence.Tx) error { _, e := Intern(c, tx, list[:4095]); return e })
	write(t, s, func(c context.Context, tx persistence.Tx) error {
		observed := &observedTx{Tx: tx}
		_, e := Intern(c, observed, list)
		if observed.lookups > 17 {
			t.Fatal("prefix search became linear", observed.lookups)
		}
		return e
	})
}

func TestAppendIdentityAndLinearStorage(t *testing.T) {
	s := fixture(t)
	list := []json.RawMessage{json.RawMessage(`{"Role":"system","Text":"s"}`), json.RawMessage(`{"Role":"user","Text":"u"}`)}
	write(t, s, func(c context.Context, tx persistence.Tx) error {
		prefix, err := Intern(c, tx, list[:1])
		if err != nil {
			return err
		}
		a, err := Append(c, tx, prefix, list[1:])
		if err != nil {
			return err
		}
		b, err := Intern(c, tx, list)
		if err != nil {
			return err
		}
		if a != b {
			t.Fatal("append and intern identity differ")
		}
		return nil
	})
	gc(t, s)
	texts := []string{}
	naive := 0
	for i := 0; i < 100; i++ {
		texts = append(texts, fmt.Sprintf("message-%03d %s", i, strings.Repeat("payload ", 32)))
		d := doc(texts...)
		naive += len(mustJSON(t, d))
		save(t, s, ref("owner", fmt.Sprint(i)), d)
	}
	var size int
	if err := s.Read(t.Context(), func(c context.Context, tx persistence.Tx) error {
		rows, e := tx.Scan(c, objects)
		for _, r := range rows {
			size += len(r.Data)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if n := total(t, s, objects); n != 200 {
		t.Fatal("prefixes duplicated", n)
	}
	if size >= naive/3 {
		t.Fatal("context storage did not remove repeated prefixes", size, naive)
	}
	t.Logf("100 growing snapshots: %d stored object bytes vs %d full-snapshot bytes", size, naive)
}

func TestToolDefinitionsShareContentAcrossSnapshots(t *testing.T) {
	s := fixture(t)
	a, b := ref("one", "request"), ref("two", "request")
	tools := []any{map[string]any{"Name": "read", "InputSchema": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}}}
	first, second := doc("system", "a"), doc("system", "b")
	first["Tools"] = tools
	second["Tools"] = tools
	save(t, s, a, first)
	save(t, s, b, second)
	var n int64
	if err := s.Read(t.Context(), func(c context.Context, tx persistence.Tx) error {
		var err error
		n, err = tx.Count(c, objects, persistence.Query{Where: []persistence.Filter{persistence.Eq("Kind", "value")}})
		return err
	}); err != nil || n != 1 {
		t.Fatal("tool definitions duplicated", n, err)
	}
	write(t, s, func(c context.Context, tx persistence.Tx) error { return Release(c, tx, a) })
	gc(t, s)
	expected, _ := canonical(mustJSON(t, second))
	if string(load(t, s, b)) != string(expected) {
		t.Fatal("shared tools lost")
	}
	write(t, s, func(c context.Context, tx persistence.Tx) error { return Release(c, tx, b) })
	gc(t, s)
	if total(t, s, objects) != 0 {
		t.Fatal("tool definitions leaked")
	}
}
