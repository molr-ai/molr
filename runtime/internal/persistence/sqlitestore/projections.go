package sqlitestore

import (
	"bytes"
	"context"
	"encoding/json"
	"molr.ai/runtime/internal/persistence"
	"strings"
	"time"
)

// Private sort keys retain nanoseconds and normalize offsets. The canonical
// domain timestamps remain unchanged. Every Put updates the keys atomically.
func queryProjections(bucket string, data []byte) ([]byte, error) {
	var fields []string
	switch bucket {
	case "security.grants":
		fields = []string{"ExpiresAt"}
	case "im.outbox", "security.audit":
		fields = []string{"At"}
	case "im.messages", "agent.operations", "notifications.records", "business.dream_runs", "security.permission_requests", "weixin.deliveries", "telegram.deliveries", "slack.deliveries", "slack.incoming":
		fields = []string{"CreatedAt"}
	case "business.tasks":
		fields = []string{"UpdatedAt"}
	case "business.dream_sessions":
		fields = []string{"End", "WindowStart", "Deadline"}
	case "debug.summaries", "sandbox.shell_tasks":
		fields = []string{"StartedAt"}
	case "auth.login-links":
		fields = []string{"Link.CreatedAt", "Link.ExpiresAt"}
	case "auth.sessions":
		fields = []string{"Session.LastSeenAt", "Session.ExpiresAt", "RevokedAt"}
	case "auth.challenges":
		fields = []string{"ExpiresAt"}
	case "plugins.http-calls", "mcp.calls":
		fields = []string{"CreatedAt"}
	}
	if len(fields) == 0 {
		return data, nil
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil || row == nil {
		// Shape validation belongs to the domain; optional recordings may be corrupt.
		return data, nil
	}
	for _, field := range fields {
		raw := row[field]
		parts := strings.Split(field, ".")
		if len(parts) == 2 {
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(row[parts[0]], &nested); err != nil {
				return data, nil
			}
			raw = nested[parts[1]]
		}
		var at time.Time
		key := ""
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &at); err != nil {
				return data, nil
			}
			key = persistence.TimeKey(at)
		}
		name := parts[len(parts)-1] + "Key"
		row[name], _ = json.Marshal(key)
	}
	return json.Marshal(row)
}

// initializeQueryProjections builds derived sort keys for records already present
// when these indexes are first installed. The marker and all derived keys commit
// together; subsequent opens do only a primary-key marker lookup.
func (s *Store) initializeQueryProjections() error {
	if err := s.initializeProjectionGroup("timestamp-keys-v1", []string{"security.grants", "im.outbox", "security.audit", "im.messages", "business.tasks", "business.dream_sessions", "debug.summaries", "auth.login-links", "plugins.http-calls"}); err != nil {
		return err
	}
	// Installing the call accounting indexes derives only operation time keys.
	if err := s.initializeProjectionGroup("call-accounting-v1", []string{"agent.operations"}); err != nil {
		return err
	}
	if err := s.initializeProjectionGroup("shell-task-order-v1", []string{"sandbox.shell_tasks"}); err != nil {
		return err
	}
	// Installing the notification order index derives only its own sort keys.
	if err := s.initializeProjectionGroup("notification-order-v1", []string{"notifications.records"}); err != nil {
		return err
	}
	if err := s.initializeProjectionGroup("dream-history-order-v1", []string{"business.dream_runs"}); err != nil {
		return err
	}
	if err := s.initializeProjectionGroup("permission-request-order-v1", []string{"security.permission_requests"}); err != nil {
		return err
	}
	if err := s.initializeProjectionGroup("weixin-delivery-order-v1", []string{"weixin.deliveries"}); err != nil {
		return err
	}
	return s.initializeProjectionGroup("auth-expiry-v1", []string{"auth.sessions", "auth.login-links", "auth.challenges"})
}

func (s *Store) initializeProjectionGroup(marker string, buckets []string) error {
	return s.Write(context.Background(), func(ctx context.Context, tx persistence.Tx) error {
		var initialized bool
		err := tx.Get(ctx, "system.query_indexes", marker, &initialized)
		if err == nil && initialized {
			return nil
		}
		if err != nil && !persistence.Missing(err) {
			return err
		}
		for _, bucket := range buckets {
			after := ""
			for {
				rows, err := tx.Query(ctx, bucket, persistence.Query{Where: []persistence.Filter{{Field: "key", Op: ">", Value: after}}, Limit: 100})
				if err != nil {
					return err
				}
				for _, row := range rows {
					after = row.Key
					projected, err := queryProjections(bucket, row.Data)
					if err != nil {
						return err
					}
					if !bytes.Equal(projected, row.Data) {
						if err := tx.Put(ctx, bucket, row.Key, json.RawMessage(projected)); err != nil {
							return err
						}
					}
				}
				if len(rows) < 100 {
					break
				}
			}
		}
		return tx.Put(ctx, "system.query_indexes", marker, true)
	})
}
