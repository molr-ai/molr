package sqlitestore

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"regexp"
	"strings"
)

var fieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

func fieldSQL(field string) (string, error) {
	if field == "OwnerKey" {
		return "substr(key,1,instr(key,'/')-1)", nil
	}
	if field == "key" {
		return "key", nil
	}
	if len(field) > 128 || !fieldName.MatchString(field) {
		return "", core.Invalid("query.field", "invalid field")
	}
	expr := "json_extract(value, '$." + field + "')"
	if field == "CreatedAt" {
		expr = "julianday(" + expr + ")"
	}
	return expr, nil
}

// Composite expression indexes cover the hot owner/resource/state/time paths.
// Keep expressions identical to fieldSQL so SQLite can use them for filtering.
func (s *Store) createIndexes() error {
	indexes := []struct {
		bucket string
		fields []string
	}{
		{"", []string{"OwnerID"}},
		{"security.reservations", []string{"OwnerID", "AssistantID", "CreatedAt"}},
		{"security.reservations", []string{"OwnerID", "ModelID", "CreatedAt"}},
		{"security.reservations", []string{"OwnerID", "ModelID", "Reservation.State"}},
		{"security.reservations", []string{"OwnerID", "Reservation.OperationID"}},
		{"security.reservations", []string{"OwnerID", "AssistantID", "Reservation.State"}},
		{"agent.operations", []string{"OwnerID", "Action", "CreatedAt"}},
		{"agent.operations", []string{"OwnerID", "Action", "State"}},
		{"plugins.http-calls", []string{"Connection.ID", "OwnerID", "CreatedAt"}},
		{"plugins.http-calls", []string{"Connection.ID", "OwnerID", "State"}},
		{"security.grants", []string{"OwnerID", "AssistantID", "ResourceID"}},
		{"im.messages", []string{"OwnerKey", "ConversationID", "Sequence"}},
		{"im.visible", []string{"OwnerID", "Sequence"}},
		{"im.visible", []string{"OwnerID", "Topic", "Sequence"}},
		{"agent.jobs", []string{"State", "QueueKey"}},
		{"agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "State", "QueueKey"}},
		{"business.dream_sessions_index", []string{"OwnerID", "AssistantID", "State"}},
		{"business.schedules", []string{"Enabled", "NextAtKey"}},
		{"business.dream_sessions_index", []string{"State", "DeadlineKey"}},
		{"business.plan-links", []string{"HandoffPending", "key"}},
		{"business.tasks", []string{"OwnerID", "Plan.Kind", "Plan.ID"}},
		{"notifications.records", []string{"Kind", "ResolvedAt"}},
	}
	for i, index := range indexes {
		parts := []string{"bucket"}
		for _, field := range index.fields {
			expr, err := fieldSQL(field)
			if err != nil {
				return err
			}
			parts = append(parts, expr)
		}
		where := ""
		if index.bucket != "" {
			where = " WHERE bucket='" + index.bucket + "'"
		}
		if _, err := s.db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS records_query_%d ON records (%s)%s", i, strings.Join(parts, ","), where)); err != nil {
			return err
		}
	}
	return nil
}
func querySQL(bucket string, q persistence.Query) (string, []any, error) {
	if err := valid(bucket, "query"); err != nil {
		return "", nil, err
	}
	if q.Limit < 0 || len(q.Where) > 32 || len(q.Any) > 16 {
		return "", nil, core.Invalid("query", "invalid bounds")
	}
	args := []any{bucket}
	clauses := []string{"bucket=?"}
	if q.Prefix != "" {
		clauses = append(clauses, "key>=? AND key<?")
		args = append(args, q.Prefix, q.Prefix+"\U0010ffff")
	}
	filters := func(items []persistence.Filter) (string, error) {
		if len(items) == 0 || len(items) > 32 {
			return "", core.Invalid("query", "empty or oversized filter")
		}
		parts := []string{}
		for _, f := range items {
			expr, err := fieldSQL(f.Field)
			if err != nil {
				return "", err
			}
			switch f.Op {
			case "=", "!=", "<", "<=", ">", ">=":
			default:
				return "", core.Invalid("query.operator", "unsupported operator")
			}
			if f.Value == nil && (f.Op == "=" || f.Op == "!=") {
				op := " IS NULL"
				if f.Op == "!=" {
					op = " IS NOT NULL"
				}
				parts = append(parts, expr+op)
				continue
			}
			parameter := "?"
			if f.Field == "CreatedAt" {
				parameter = "julianday(?)"
			}
			parts = append(parts, expr+f.Op+parameter)
			args = append(args, f.Value)
		}
		return strings.Join(parts, " AND "), nil
	}
	if len(q.Where) > 0 {
		part, err := filters(q.Where)
		if err != nil {
			return "", nil, err
		}
		clauses = append(clauses, part)
	}
	if len(q.Any) > 0 {
		// Separate period and pending-state branches so each can use its own range
		// index. A plain OR plus ORDER BY key otherwise invites a whole-bucket scan.
		base := strings.Join(clauses, " AND ")
		baseArgs := append([]any{}, args...)
		union := []string{}
		unionArgs := []any{bucket}
		for _, fs := range q.Any {
			args = append([]any{}, baseArgs...)
			part, err := filters(fs)
			if err != nil {
				return "", nil, err
			}
			union = append(union, "SELECT key FROM records WHERE "+base+" AND "+part)
			unionArgs = append(unionArgs, args...)
		}
		clauses = []string{"bucket=?", "key IN (" + strings.Join(union, " UNION ALL ") + ")"}
		args = unionArgs
	}
	order := "key"
	if q.OrderBy != "" {
		var err error
		order, err = fieldSQL(q.OrderBy)
		if err != nil {
			return "", nil, err
		}
	}
	if q.Descending {
		order += " DESC"
	}
	selection := "value"
	if len(q.Fields) > 0 {
		if len(q.Fields) > 32 {
			return "", nil, core.Invalid("query.fields", "too many fields")
		}
		parts := []string{"'{}'"}
		for _, field := range q.Fields {
			if len(field) > 128 || !fieldName.MatchString(field) {
				return "", nil, core.Invalid("query.field", "invalid field")
			}
			parts = append(parts, "'$."+field+"'", "json_extract(value, '$."+field+"')")
		}
		selection = "json_set(" + strings.Join(parts, ",") + ")"
	}
	query := "SELECT key," + selection + " FROM records WHERE " + strings.Join(clauses, " AND ")
	if !q.Unordered {
		query += " ORDER BY " + order
	}
	if q.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, q.Limit)
	}
	return query, args, nil
}
func (tx *transaction) Query(ctx context.Context, bucket string, q persistence.Query) ([]persistence.Record, error) {
	if err := tx.check(ctx); err != nil {
		return nil, err
	}
	query, args, err := querySQL(bucket, q)
	if err != nil {
		return nil, err
	}
	rows, err := tx.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := []persistence.Record{}
	for rows.Next() {
		var row persistence.Record
		var raw []byte
		if err := rows.Scan(&row.Key, &raw); err != nil {
			return nil, err
		}
		row.Data = raw
		out = append(out, row)
	}
	return out, translate(ctx, rows.Err())
}
