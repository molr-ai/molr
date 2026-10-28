package watch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"time"
	"unicode/utf8"
)

type message struct {
	Type    string          `json:"type"`
	Version int             `json:"version,omitempty"`
	Key     string          `json:"key,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	State   json.RawMessage `json:"state,omitempty"`
}

func protocolError() error {
	return core.WithReason(core.Invalid("watcher", "invalid watcher protocol message"), "watch_protocol_invalid")
}
func decode(line []byte) (message, error) {
	var m message
	if len(line) == 0 || len(line) > MaxMessage || !utf8.Valid(line) {
		return m, protocolError()
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF {
		return m, protocolError()
	}
	// encoding/json otherwise accepts case-insensitive and duplicate fields.
	// The wire envelope has exact keys and no fields from other message types.
	fields := json.NewDecoder(bytes.NewReader(line))
	if token, err := fields.Token(); err != nil || token != json.Delim('{') {
		return m, protocolError()
	}
	seen := map[string]bool{}
	for fields.More() {
		token, err := fields.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return m, protocolError()
		}
		seen[key] = true
		allowed := key == "type" || m.Type == "ready" && key == "version" || m.Type == "event" && (key == "key" || key == "data" || key == "state") || (m.Type == "checkpoint" || m.Type == "complete") && key == "state"
		if !allowed || fields.Decode(new(json.RawMessage)) != nil {
			return m, protocolError()
		}
	}
	if len(m.State) > MaxState {
		return m, protocolError()
	}
	switch m.Type {
	case "ready":
		if m.Version != 1 || m.Key != "" || len(m.Data) > 0 || len(m.State) > 0 {
			return m, protocolError()
		}
	case "checkpoint":
		if m.Version != 0 || m.Key != "" || len(m.Data) > 0 || len(m.State) == 0 {
			return m, protocolError()
		}
	case "event":
		if m.Version != 0 || m.Key == "" || len(m.Key) > 256 || len(m.Data) == 0 || len(m.Data) > 8192 || bytes.Equal(bytes.TrimSpace(m.Data), []byte("null")) {
			return m, protocolError()
		}
	case "complete":
		if m.Version != 0 || m.Key != "" || len(m.Data) > 0 {
			return m, protocolError()
		}
	default:
		return m, protocolError()
	}
	return m, nil
}

// Begin fences stale processes with a fresh run identity without changing the configuration revision.
func (s *Service) Begin(ctx context.Context, owner, id core.ID, version core.Version) (Watch, error) {
	var w Watch
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		var err error
		w, err = Load(ctx, tx.Store(), owner, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if w.Version != version || w.Paused || w.State == "completed" || w.EndAt != nil && !now.Before(*w.EndAt) || w.StartAt != nil && now.Before(*w.StartAt) {
			return core.NewError(core.FailedPrecondition, "watch is not runnable")
		}
		w.RunID = newID()
		w.LastRunID = w.RunID
		w.State = "starting"
		w.Reason = ""
		w.LastHealthyAt = nil
		w.RetryAt = nil
		if err := tx.Put(ctx, Bucket, persistence.Key(owner, id), w); err != nil {
			return err
		}
		return changed(ctx, tx.Store(), w)
	})
	return w, err
}

// Receive commits observation state and event receipt together before returning an acknowledgement.
func (s *Service) Receive(ctx context.Context, owner, id, run core.ID, line []byte) (json.RawMessage, error) {
	m, err := decode(line)
	if err != nil {
		return nil, err
	}
	err = s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		w, err := Load(ctx, tx.Store(), owner, id)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if w.RunID != run || w.Paused || w.State == "completed" || w.EndAt != nil && !now.Before(*w.EndAt) {
			return core.NewError(core.Canceled, "watch execution superseded")
		}
		if m.Type == "ready" {
			if w.State != "starting" {
				return protocolError()
			}
			w.State = "running"
		} else if w.State != "running" {
			return protocolError()
		}
		if m.Type == "event" {
			hash := sha256.Sum256([]byte(m.Key))
			key := persistence.Key(owner, core.ID(string(id)+"/"+hex.EncodeToString(hash[:])))
			var previous struct{ Data json.RawMessage }
			if err := tx.Get(ctx, "business.watch-dedup", key, &previous); err == nil {
				if !bytes.Equal(previous.Data, m.Data) {
					return protocolError()
				}
				return nil
			} else if !persistence.Missing(err) {
				return err
			}
			pendingBytes := len(m.Data)
			for _, v := range w.Pending {
				pendingBytes += len(v.Data)
			}
			if len(w.Pending) >= MaxPending || pendingBytes > 256<<10 {
				return core.WithReason(core.NewError(core.RateLimited, "watch pending event capacity reached"), "watch_queue_full")
			}
			signal := Signal{Key: m.Key, Data: append(json.RawMessage(nil), m.Data...), At: now}
			w.Pending = append(w.Pending, signal)
			w.Recent = append(w.Recent, signal)
			if len(w.Recent) > 100 {
				w.Recent = w.Recent[len(w.Recent)-100:]
			}
			w.PendingCount = len(w.Pending)
			if err := tx.Put(ctx, "business.watch-dedup", key, struct{ Data json.RawMessage }{m.Data}); err != nil {
				return err
			}
		}
		if len(m.State) > 0 {
			w.Checkpoint = append(json.RawMessage(nil), m.State...)
		}
		if m.Type == "checkpoint" || m.Type == "event" {
			w.Failures = 0
			w.FailureNotified = false
		}
		if m.Type == "complete" {
			w.State = "completed"
		}
		w.LastHealthyAt = &now
		if err := tx.Put(ctx, Bucket, persistence.Key(owner, id), w); err != nil {
			return err
		}
		if m.Type == "checkpoint" {
			return nil
		}
		return changed(ctx, tx.Store(), w)
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"type": "ack", "for": m.Type, "key": m.Key})
}

// End records one failed run. The caller notifies only when this returns true.
func (s *Service) End(ctx context.Context, owner, id, run core.ID, reason string, retry bool) (bool, error) {
	notify := false
	err := s.store.Write(ctx, func(ctx context.Context, tx persistence.Tx) error {
		w, err := Load(ctx, tx.Store(), owner, id)
		if persistence.Missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if w.RunID != run || w.Paused || w.State == "completed" {
			return nil
		}
		now := time.Now().UTC()
		w.RunID = ""
		w.Failures++
		w.Reason = reason
		w.State = "blocked"
		if w.EndAt != nil && !now.Before(*w.EndAt) {
			w.State = "completed"
			w.Reason = "expired"
			if err := tx.Put(ctx, Bucket, persistence.Key(owner, id), w); err != nil {
				return err
			}
			return changed(ctx, tx.Store(), w)
		}
		if retry && w.Failures < 3 {
			next := now.Add(time.Duration(1<<w.Failures) * 5 * time.Second)
			w.RetryAt = &next
			w.State = "retrying"
		} else if !w.FailureNotified {
			notify = true
			w.FailureNotified = true
		}
		if err := tx.Put(ctx, Bucket, persistence.Key(owner, id), w); err != nil {
			return err
		}
		return changed(ctx, tx.Store(), w)
	})
	return notify, err
}
