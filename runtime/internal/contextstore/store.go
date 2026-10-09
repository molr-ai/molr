// Package contextstore stores immutable, content-addressed message prefixes and
// documents. Only trusted domain repositories may use it; public APIs resolve
// authorized owner/assistant references, never arbitrary content hashes.
package contextstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const Root = "0000000000000000000000000000000000000000000000000000000000000000"
const objects = "context.objects"
const counts = "context.counts"
const references = "context.references"

type Reference struct {
	OwnerID     core.ID
	AssistantID core.ID
	Kind        string
	ID          string
}
type reference struct {
	Reference
	DocumentID string
}
type slot struct {
	Path   []string
	NodeID string
	Length int
}
type valueSlot struct {
	Path []string
	ID   string
}
type object struct {
	Kind   string
	Parent string `json:",omitempty"`
	Data   json.RawMessage
	Trees  []slot      `json:",omitempty"`
	Values []valueSlot `json:",omitempty"`
}
type counter struct{ Count int64 }

func canonical(raw []byte) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, core.Invalid("context_json", "valid JSON required")
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func hash(domain string, data ...[]byte) string {
	h := sha256.New()
	h.Write([]byte(domain))
	for _, b := range data {
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func nodeID(parent string, message []byte) string {
	p, _ := hex.DecodeString(parent)
	return hash("molr.context-node:v1\x00", p, message)
}
func refKey(ref Reference) (string, error) {
	if err := ref.OwnerID.Validate(); err != nil {
		return "", err
	}
	if err := ref.AssistantID.Validate(); err != nil {
		return "", err
	}
	if ref.Kind == "" || ref.ID == "" {
		return "", core.Invalid("context_reference", "kind and id required")
	}
	data, err := json.Marshal(ref)
	if err != nil {
		return "", err
	}
	return hash("molr.context-reference:v1\x00", data), nil
}
func exists(ctx context.Context, tx persistence.Tx, id string) (bool, error) {
	var n counter
	err := tx.Get(ctx, counts, id, &n)
	if persistence.Missing(err) {
		return false, nil
	}
	return err == nil, err
}
func retain(ctx context.Context, tx persistence.Tx, id string, delta int64) error {
	if id == Root {
		return nil
	}
	var n counter
	if err := tx.Get(ctx, counts, id, &n); err != nil {
		return err
	}
	n.Count += delta
	if n.Count < 0 {
		return core.NewError(core.Internal, "negative context reference count")
	}
	return tx.Put(ctx, counts, id, n)
}
func insert(ctx context.Context, tx persistence.Tx, id string, o object) error {
	present, err := exists(ctx, tx, id)
	if err != nil || present {
		return err
	}
	if err = tx.Put(ctx, objects, id, o); err != nil {
		return err
	}
	if err = tx.Put(ctx, counts, id, counter{}); err != nil {
		return err
	}
	if o.Parent != "" {
		if err = retain(ctx, tx, o.Parent, 1); err != nil {
			return err
		}
	}
	for _, s := range o.Trees {
		if err = retain(ctx, tx, s.NodeID, 1); err != nil {
			return err
		}
	}
	for _, v := range o.Values {
		if err = retain(ctx, tx, v.ID, 1); err != nil {
			return err
		}
	}
	return nil
}

// Intern reuses the longest existing prefix. Hash equality is content identity;
// SHA-256 is never truncated. The caller must install a reference in this same
// write transaction before returning, otherwise the new nodes are collectible.
func Intern(ctx context.Context, tx persistence.Tx, messages []json.RawMessage) (string, error) {
	if len(messages) > 1_000_000 {
		return "", core.Invalid("context_messages", "too many messages")
	}
	ids := make([]string, len(messages)+1)
	ids[0] = Root
	encoded := make([]json.RawMessage, len(messages))
	for i, m := range messages {
		if err := persistence.CheckContext(ctx); err != nil {
			return "", err
		}
		raw, err := canonical(m)
		if err != nil {
			return "", err
		}
		encoded[i] = raw
		ids[i+1] = nodeID(ids[i], raw)
	}
	last := len(messages)
	if last == 0 {
		return Root, nil
	}
	found, err := exists(ctx, tx, ids[last])
	if err != nil {
		return "", err
	}
	if found {
		return ids[last], nil
	}
	lo, hi := 0, last
	for lo+1 < hi {
		mid := (lo + hi) / 2
		found, err = exists(ctx, tx, ids[mid])
		if err != nil {
			return "", err
		}
		if found {
			lo = mid
		} else {
			hi = mid
		}
	}
	for i := lo; i < last; i++ {
		if err = insert(ctx, tx, ids[i+1], object{Kind: "message", Parent: ids[i], Data: encoded[i]}); err != nil {
			return "", err
		}
	}
	return ids[last], nil
}
func messages(ctx context.Context, tx persistence.Tx, id string, length int) ([]json.RawMessage, error) {
	if length < 0 || length > 1_000_000 {
		return nil, core.NewError(core.Internal, "invalid context path length")
	}
	if length == 0 {
		if id != Root {
			return nil, core.NewError(core.Internal, "invalid context root")
		}
		return []json.RawMessage{}, nil
	}
	rows, err := tx.Ancestors(ctx, objects, id, "Parent", length)
	if err != nil {
		return nil, err
	}
	if len(rows) != length {
		return nil, core.NewError(core.Internal, "incomplete context path")
	}
	result := make([]json.RawMessage, length)
	for i, row := range rows {
		if err := persistence.CheckContext(ctx); err != nil {
			return nil, err
		}
		o, err := persistence.Decode[object](row)
		if err != nil {
			return nil, err
		}
		if row.Key != id || o.Kind != "message" || nodeID(o.Parent, o.Data) != id {
			return nil, core.NewError(core.Internal, "corrupt context node")
		}
		result[length-1-i] = o.Data
		id = o.Parent
	}
	if id != Root {
		return nil, core.NewError(core.Internal, "invalid context path length")
	}
	return result, nil
}
func messageArray(key string, v any) bool {
	if key != "Inputs" && key != "messages" && key != "input" {
		return false
	}
	list, ok := v.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return false
		}
		role, ok := m["Role"].(string)
		if !ok {
			role, ok = m["role"].(string)
		}
		if !ok {
			return false
		}
		switch role {
		case "system", "developer", "user", "assistant", "tool":
		default:
			return false
		}
	}
	return true
}
func pathAppend(path []string, key string) []string { return append(append([]string{}, path...), key) }
func split(ctx context.Context, tx persistence.Tx, v any, path []string, trees *[]slot, values *[]valueSlot) (any, error) {
	switch value := v.(type) {
	case map[string]any:
		// Map traversal order must not affect document identity or hash lookups.
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			item := value[key]
			p := pathAppend(path, key)
			if messageArray(key, item) {
				list := item.([]any)
				raw := make([]json.RawMessage, len(list))
				for i, m := range list {
					raw[i], _ = json.Marshal(m)
				}
				id, err := Intern(ctx, tx, raw)
				if err != nil {
					return nil, err
				}
				*trees = append(*trees, slot{Path: p, NodeID: id, Length: len(list)})
				value[key] = nil
			} else if item != nil && (key == "Tools" || key == "Definitions" || key == "tools" || key == "Automatic") {
				data, err := json.Marshal(item)
				if err != nil {
					return nil, err
				}
				id := hash("molr.context-value:v1\x00", data)
				if err = insert(ctx, tx, id, object{Kind: "value", Data: data}); err != nil {
					return nil, err
				}
				*values = append(*values, valueSlot{Path: p, ID: id})
				value[key] = nil
			} else {
				next, err := split(ctx, tx, item, p, trees, values)
				if err != nil {
					return nil, err
				}
				value[key] = next
			}
		}
	case []any:
		for i, item := range value {
			next, err := split(ctx, tx, item, pathAppend(path, strconv.Itoa(i)), trees, values)
			if err != nil {
				return nil, err
			}
			value[i] = next
		}
	}
	return v, nil
}
func assign(v any, path []string, value any) error {
	if len(path) == 0 {
		return core.NewError(core.Internal, "invalid context slot")
	}
	switch row := v.(type) {
	case map[string]any:
		if len(path) == 1 {
			if _, ok := row[path[0]]; !ok {
				return core.NewError(core.Internal, "missing context slot")
			}
			row[path[0]] = value
			return nil
		}
		return assign(row[path[0]], path[1:], value)
	case []any:
		i, err := strconv.Atoi(path[0])
		if err != nil || i < 0 || i >= len(row) {
			return core.NewError(core.Internal, "invalid context slot index")
		}
		if len(path) == 1 {
			row[i] = value
			return nil
		}
		return assign(row[i], path[1:], value)
	}
	return core.NewError(core.Internal, "invalid context document")
}

// Save persists a document and swaps a scoped external reference atomically.
// Message arrays share prefixes; the rest of the document is also addressed by
// content so identical request bodies/tool definitions need only one copy.
func Save(ctx context.Context, tx persistence.Tx, ref Reference, value any) error {
	key, err := refKey(ref)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var tree any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err = d.Decode(&tree); err != nil {
		return err
	}
	var trees []slot
	var values []valueSlot
	tree, err = split(ctx, tx, tree, nil, &trees, &values)
	if err != nil {
		return err
	}
	data, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	o := object{Kind: "document", Data: data, Trees: trees, Values: values}
	encoded, err := json.Marshal(o)
	if err != nil {
		return err
	}
	id := hash("molr.context-document:v1\x00", encoded)
	var old reference
	err = tx.Get(ctx, references, key, &old)
	if err != nil && !persistence.Missing(err) {
		return err
	}
	if err == nil && old.Reference != ref {
		return core.NewError(core.Forbidden, "context reference unavailable")
	}
	if old.DocumentID == id {
		return nil
	}
	if err = insert(ctx, tx, id, o); err != nil {
		return err
	}
	if err = retain(ctx, tx, id, 1); err != nil {
		return err
	}
	if old.DocumentID != "" {
		if err = retain(ctx, tx, old.DocumentID, -1); err != nil {
			return err
		}
	}
	return tx.Put(ctx, references, key, reference{Reference: ref, DocumentID: id})
}
func Load(ctx context.Context, tx persistence.Tx, ref Reference, out any) error {
	key, err := refKey(ref)
	if err != nil {
		return err
	}
	var r reference
	if err = tx.Get(ctx, references, key, &r); err != nil {
		return err
	}
	if r.Reference != ref {
		return core.NewError(core.Forbidden, "context reference unavailable")
	}
	var o object
	if err = tx.Get(ctx, objects, r.DocumentID, &o); err != nil {
		return err
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	if o.Kind != "document" || hash("molr.context-document:v1\x00", raw) != r.DocumentID {
		return core.NewError(core.Internal, "corrupt context document")
	}
	var tree any
	d := json.NewDecoder(bytes.NewReader(o.Data))
	d.UseNumber()
	if err = d.Decode(&tree); err != nil {
		return err
	}
	for _, s := range o.Trees {
		list, err := messages(ctx, tx, s.NodeID, s.Length)
		if err != nil {
			return err
		}
		if err = assign(tree, s.Path, list); err != nil {
			return err
		}
	}
	for _, v := range o.Values {
		var value object
		if err = tx.Get(ctx, objects, v.ID, &value); err != nil {
			return err
		}
		if value.Kind != "value" || hash("molr.context-value:v1\x00", value.Data) != v.ID {
			return core.NewError(core.Internal, "corrupt context value")
		}
		if err = assign(tree, v.Path, value.Data); err != nil {
			return err
		}
	}
	data, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func Release(ctx context.Context, tx persistence.Tx, ref Reference) error {
	key, err := refKey(ref)
	if err != nil {
		return err
	}
	var old reference
	err = tx.Get(ctx, references, key, &old)
	if persistence.Missing(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if old.Reference != ref {
		return core.NewError(core.Forbidden, "context reference unavailable")
	}
	if err = retain(ctx, tx, old.DocumentID, -1); err != nil {
		return err
	}
	return tx.Delete(ctx, references, key)
}

// Collect does bounded work. New references and GC serialize in write
// transactions; an unreferenced leaf can never be resurrected halfway through
// deletion. A failed collection rolls back and can be retried safely.
func Collect(ctx context.Context, store persistence.Store, limit int) (int, error) {
	if limit < 1 || limit > 10000 {
		return 0, core.Invalid("context_gc_limit", "1–10000 required")
	}
	deleted := 0
	err := store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		for deleted < limit {
			rows, err := tx.Query(ctx, counts, persistence.Query{Where: []persistence.Filter{persistence.Eq("Count", 0)}, Limit: min(100, limit-deleted)})
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				var o object
				if err = tx.Get(ctx, objects, row.Key, &o); err != nil {
					return err
				}
				if o.Parent != "" {
					if err = retain(ctx, tx, o.Parent, -1); err != nil {
						return err
					}
				}
				for _, s := range o.Trees {
					if err = retain(ctx, tx, s.NodeID, -1); err != nil {
						return err
					}
				}
				for _, v := range o.Values {
					if err = retain(ctx, tx, v.ID, -1); err != nil {
						return err
					}
				}
				if err = tx.Delete(ctx, objects, row.Key); err != nil {
					return err
				}
				if err = tx.Delete(ctx, counts, row.Key); err != nil {
					return err
				}
				deleted++
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("collect context objects: %w", err)
	}
	return deleted, nil
}

// ReleaseKind drops a trusted domain's references for one assistant. Other
// owners, assistants and reference purposes remain pinned.
func ReleaseKind(ctx context.Context, tx persistence.Tx, owner, assistant core.ID, kind string) error {
	if err := owner.Validate(); err != nil {
		return err
	}
	if err := assistant.Validate(); err != nil {
		return err
	}
	if kind == "" {
		return core.Invalid("context_kind", "required")
	}
	q := persistence.Query{Where: []persistence.Filter{persistence.Eq("OwnerID", owner), persistence.Eq("AssistantID", assistant), persistence.Eq("Kind", kind)}, Limit: 100}
	for {
		rows, err := tx.Query(ctx, references, q)
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := persistence.Decode[reference](row)
			if err != nil {
				return err
			}
			if err = Release(ctx, tx, r.Reference); err != nil {
				return err
			}
		}
		if len(rows) < q.Limit {
			return nil
		}
	}
}

// Append starts from a known stored prefix; callers with an unchanged prefix
// need only encode the new suffix. As with Intern, pin the result in the same
// transaction before it becomes visible to a collector.
func Append(ctx context.Context, tx persistence.Tx, parent string, suffix []json.RawMessage) (string, error) {
	if parent != Root {
		ok, err := exists(ctx, tx, parent)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", core.NewError(core.NotFound, "context prefix unavailable")
		}
	}
	for _, message := range suffix {
		raw, err := canonical(message)
		if err != nil {
			return "", err
		}
		id := nodeID(parent, raw)
		if err = insert(ctx, tx, id, object{Kind: "message", Parent: parent, Data: raw}); err != nil {
			return "", err
		}
		parent = id
	}
	return parent, nil
}
