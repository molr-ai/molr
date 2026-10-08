package realtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"sort"

	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
)

const visibleBucket = "im.visible"
const countersBucket = "im.counters"

type stored struct{ store persistence.Store }

// NewStore uses the caller's transaction when its context or Store is bound.
func NewStore(store persistence.Store) Store { return &stored{store: store} }
func signalKey(owner core.ID) persistence.SignalKey {
	return persistence.SignalKey{Namespace: visibleBucket, Key: string(owner)}
}
func (s *stored) Watch(owner core.ID) (persistence.Watch, error) {
	if err := owner.Validate(); err != nil {
		return nil, err
	}
	return s.store.Watch(signalKey(owner))
}
func (s *stored) Append(ctx context.Context, e Visible) error {
	for _, id := range []core.ID{e.ID, e.OwnerID, e.Object.ID} {
		if err := id.Validate(); err != nil {
			return err
		}
	}
	if e.Topic == "" || len(e.Topic) > 128 || e.Object.Kind == "" || e.CreatedAt.IsZero() {
		return core.Invalid("event", "topic, object and creation time required")
	}
	return s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		key := persistence.Key(e.OwnerID, e.ID)
		var old Visible
		err := tx.Get(ctx, visibleBucket, key, &old)
		if err == nil {
			e.Sequence = old.Sequence
			if reflect.DeepEqual(old, e) {
				return nil
			}
			return core.NewError(core.Conflict, "visible event ID already used")
		}
		if !persistence.Missing(err) {
			return err
		}
		var seq uint64
		counter := persistence.Key(e.OwnerID, "visible")
		err = tx.Get(ctx, countersBucket, counter, &seq)
		if err != nil && !persistence.Missing(err) {
			return err
		}
		if seq == ^uint64(0) {
			return core.NewError(core.FailedPrecondition, "event stream exhausted")
		}
		e.Sequence = seq + 1
		if err = tx.Put(ctx, visibleBucket, key, e); err != nil {
			return err
		}
		if err := tx.Put(ctx, countersBucket, counter, e.Sequence); err != nil {
			return err
		}
		return tx.NotifyAfterCommit(ctx, signalKey(e.OwnerID))
	})
}

type cursor struct {
	Owner    core.ID
	Topics   []string
	Sequence uint64
}

func normalizedTopics(topics []string) ([]string, error) {
	if len(topics) > 16 {
		return nil, core.Invalid("topics", "at most 16 topics allowed")
	}
	result := append([]string{}, topics...)
	sort.Strings(result)
	for i, topic := range result {
		if topic == "" || len(topic) > 128 {
			return nil, core.Invalid("topics", "invalid topic")
		}
		if i > 0 && topic == result[i-1] {
			return nil, core.Invalid("topics", "duplicate topic")
		}
	}
	return result, nil
}
func encodeCursor(owner core.ID, topics []string, seq uint64) string {
	data, _ := json.Marshal(cursor{Owner: owner, Topics: topics, Sequence: seq})
	return base64.RawURLEncoding.EncodeToString(data)
}
func parseCursor(value string, owner core.ID, topics []string) (uint64, error) {
	if value == "" {
		return 0, nil
	}
	// Covers 16 JSON-escaped 128-byte topics plus owner and sequence.
	if len(value) > 32<<10 {
		return 0, core.Invalid("cursor", "invalid cursor")
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return 0, core.Invalid("cursor", "invalid cursor")
	}
	var c cursor
	if json.Unmarshal(data, &c) != nil || c.Owner != owner || !reflect.DeepEqual(c.Topics, topics) {
		return 0, core.Invalid("cursor", "cursor does not match stream")
	}
	return c.Sequence, nil
}
func (s *stored) Replay(ctx context.Context, owner core.ID, q Query) (result core.Page[Visible], err error) {
	q, seq, err := normalizeQuery(owner, q)
	if err != nil {
		return result, err
	}
	topics := q.Topics
	err = s.store.Read(ctx, func(ctx context.Context, tx persistence.Tx) error {
		items := []Visible{}
		read := func(topic string) error {
			filters := []persistence.Filter{persistence.Eq("OwnerID", owner), {Field: "Sequence", Op: ">", Value: seq}}
			if topic != "" {
				filters = append(filters, persistence.Eq("Topic", topic))
			}
			rows, err := tx.Query(ctx, visibleBucket, persistence.Query{Where: filters, OrderBy: "Sequence", Limit: q.Page.Limit + 1})
			if err != nil {
				return err
			}
			for _, row := range rows {
				e, err := persistence.Decode[Visible](row)
				if err != nil {
					return err
				}
				items = append(items, e)
			}
			return nil
		}
		if len(topics) == 0 {
			if err := read(""); err != nil {
				return err
			}
		} else {
			for _, topic := range topics {
				if err := read(topic); err != nil {
					return err
				}
			}
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Sequence < items[j].Sequence })
		if len(items) > q.Page.Limit {
			items = items[:q.Page.Limit]
			result.NextCursor = encodeCursor(owner, topics, items[len(items)-1].Sequence)
		}
		result.Items = items
		return nil
	})
	return result, err
}

// Cursor produces the owner- and topic-bound replay position for an SSE id.
// Authorization is still required on every replay; the cursor grants no access.
func Cursor(owner core.ID, topics []string, sequence uint64) (string, error) {
	if err := owner.Validate(); err != nil {
		return "", err
	}
	normalized, err := normalizedTopics(topics)
	if err != nil {
		return "", err
	}
	return encodeCursor(owner, normalized, sequence), nil
}

// normalizeQuery validates cursors without reading event history.
func normalizeQuery(owner core.ID, q Query) (Query, uint64, error) {
	if err := owner.Validate(); err != nil {
		return q, 0, err
	}
	var err error
	q.Page, err = q.Page.Normalize()
	if err != nil {
		return q, 0, err
	}
	q.Topics, err = normalizedTopics(q.Topics)
	if err != nil {
		return q, 0, err
	}
	if q.Page.Cursor != "" {
		if q.After != "" && q.After != q.Page.Cursor {
			return q, 0, core.Invalid("cursor", "conflicting cursors")
		}
		q.After = q.Page.Cursor
	}
	seq, err := parseCursor(q.After, owner, q.Topics)
	return q, seq, err
}
