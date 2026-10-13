package sqlitestore

import (
	"context"
	"fmt"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/persistence"
	"reflect"
	"regexp"
	"strings"
)

var fieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

func fieldSQL(field string) (string, error) {
	if field == "RevisionScope" {
		return "substr(key,1,instr(key,'/')+instr(substr(key,instr(key,'/')+1),'/')-1)", nil
	}
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
		{"agent.operations", []string{"OwnerID", "IdempotencyKey"}},
		{"agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "ExecutionID"}},
		{"agent.operations", []string{"OwnerID", "AssistantID", "ExecutionID"}},
	}
	// New indexes include key for deterministic scoped pagination without a full-bucket sort.
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"", []string{"OwnerID", "key"}},
		{"auth.accounts", []string{"Account.Username", "key"}},
		{"auth.accounts", []string{"Account.GroupID", "Account.Disabled", "key"}},
		{"auth.sessions", []string{"TokenDigest", "key"}},
		{"auth.sessions", []string{"Session.AccountID", "key"}},
		{"auth.tokens", []string{"Digest", "key"}},
		{"auth.tokens", []string{"AccountID", "key"}},
		{"auth.login-links", []string{"AccountID", "Link.ID", "key"}},
		{"auth.challenges", []string{"AccountID", "key"}},
		{"security.credentials", []string{"Metadata.OwnerID", "key"}},
		{"agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "key"}},
		{"agent.jobs", []string{"Event.OwnerID", "JoinedTo", "key"}},
		{"agent.events", []string{"OwnerID", "AssistantID", "Kind", "key"}},
		{"agent.events", []string{"OwnerID", "AssistantID", "key"}},
		{"agent.operations", []string{"OwnerID", "ExecutionID", "key"}},
		{"agent.operations", []string{"State", "key"}},
		{"security.grants", []string{"OwnerID", "AssistantID", "ResourceID", "key"}},
		{"security.grants", []string{"OwnerID", "AssistantID", "key"}},
		{"business.assistants", []string{"State", "key"}},
		{"business.assistants", []string{"OwnerID", "GroupID", "key"}},
		{"business.assistants", []string{"OwnerID", "State", "key"}},
		{"business.tasks", []string{"OwnerID", "AssistantID", "key"}},
		{"business.tasks", []string{"Plan.ID", "key"}},
		{"business.todos", []string{"OwnerID", "AssistantID", "key"}},
		{"business.schedules", []string{"OwnerID", "AssistantID", "key"}},
		{"business.resources", []string{"OwnerID", "Kind", "key"}},
		{"business.resources", []string{"Kind", "State", "key"}},
		{"business.resources", []string{"OwnerID", "Provider", "key"}},
		{"business.resources", []string{"SandboxID", "key"}},
		{"business.bindings", []string{"CredentialID", "key"}},
		{"business.dream_runs", []string{"OwnerID", "AssistantID", "key"}},
		{"business.dream_sessions", []string{"OwnerID", "AssistantID", "State", "key"}},
		{"business.dream_sessions", []string{"OwnerID", "AssistantID", "key"}},
		{"sandbox.connections", []string{"OwnerID", "View.Endpoint", "key"}},
		{"plugins.connections", []string{"OwnerID", "PluginID", "key"}},
		{"plugins.http-calls", []string{"State", "key"}},
		{"plugins.http-calls", []string{"Connection.ID", "CreatedAt", "key"}},
		{"debug.summaries", []string{"State", "key"}},
		{"im.outbox", []string{"ID", "key"}},
		{"im.outbox", []string{"OwnerID", "ConversationID", "key"}},
		{"weixin.bindings", []string{"BotID", "key"}},
		{"weixin.bindings", []string{"UserID", "key"}},
		{"weixin.deliveries", []string{"BindingID", "State", "key"}},
		{"storage.files", []string{"State", "key"}},
		{"sandbox.operations", []string{"Reply.State", "key"}},
	}...)
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"im.outbox", []string{"AtKey", "ID", "key"}},
		{"security.audit", []string{"AtKey", "ID", "key"}},
		{"security.audit", []string{"OwnerID", "AtKey", "ID", "key"}},
		{"im.messages", []string{"OwnerKey", "ConversationID", "CreatedAtKey", "key"}},
		{"business.tasks", []string{"OwnerID", "AssistantID", "UpdatedAtKey", "key"}},
		{"auth.login-links", []string{"AccountID", "CreatedAtKey", "Link.ID", "key"}},
		{"debug.summaries", []string{"OwnerID", "StartedAtKey", "FirstRevision", "ID", "key"}},
		{"debug.summaries", []string{"OwnerID", "StartedAtKey", "ID", "key"}},
		{"plugins.http-calls", []string{"Connection.ID", "CreatedAtKey", "key"}},
		{"security.permission_requests", []string{"OwnerID", "AssistantID", "key"}},
		{"sandbox.shell_tasks", []string{"OwnerID", "AssistantID", "key"}},
		{"business.groups", []string{"OwnerID", "Position", "key"}},
	}...)
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"weixin.bindings", []string{"State", "key"}},
		{"weixin.deliveries", []string{"State", "key"}},
		{"sandbox.shell_tasks", []string{"State", "key"}},
		{"agent.jobs", []string{"Event.Kind", "State", "key"}},
		{"agent.jobs", []string{"Event.OwnerID", "Event.AssistantID", "State", "key"}},
		{"agent.jobs", []string{"Event.OwnerID", "key"}},
		{"business.plan-links", []string{"OwnerID", "Ref.Kind", "Ref.ID", "key"}},
		{"business.dream_runs", []string{"OwnerID", "AssistantID", "State", "key"}},
		{"business.dream_sessions", []string{"OwnerID", "AssistantID", "EndKey", "key"}},
		{"business.dream_sessions", []string{"OwnerID", "AssistantID", "State", "DeadlineKey", "key"}},
		{"business.dream_sessions", []string{"OwnerID", "DeadlineKey", "key"}},
		{"business.dream_sessions", []string{"OwnerID", "AssistantID", "DeadlineKey", "key"}},
		{"business.revisions", []string{"RevisionScope", "Number", "key"}},
		{"storage.files", []string{"State", "Size"}},
	}...)
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"debug.summaries", []string{"OwnerID", "State", "key"}},
		{"debug.summaries", []string{"OwnerID", "StartedAtKey", "FirstRevision", "key"}},
		{"debug.summaries", []string{"OwnerID", "Trigger", "key"}},
		{"debug.summaries", []string{"OwnerID", "ID", "key"}},
	}...)
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{"security.audit", []string{"OwnerID", "AtKey DESC", "ID", "key DESC"}})
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"security.grants", []string{"OwnerID", "AssistantID", "ResourceID", "RevokedAt", "ExpiresAtKey", "key"}},
		{"security.grants", []string{"OwnerID", "AssistantID", "RevokedAt", "ExpiresAtKey", "key"}},
	}...)

	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"agent.operations", []string{"OwnerID", "Action", "CreatedAtKey", "State", "ID"}},
		{"agent.operations", []string{"OwnerID", "Action", "State", "CreatedAtKey", "ID"}},
		{"plugins.http-calls", []string{"Connection.ID", "OwnerID", "CreatedAtKey", "State", "ID"}},
		{"plugins.http-calls", []string{"Connection.ID", "OwnerID", "State", "CreatedAtKey", "ID"}},
		{"plugins.http-calls", []string{"Connection.ID", "State", "OwnerID", "CreatedAtKey", "ID"}},
		{"plugins.http-calls", []string{"OwnerID", "State", "Connection.ID", "CreatedAtKey", "ID"}},
		{"plugins.http-calls", []string{"Connection.ID", "CreatedAtKey", "State", "OwnerID", "ID"}},
		{"notifications.records", []string{"OwnerID", "CreatedAtKey DESC", "ID DESC", "key DESC"}},
		{"notifications.records", []string{"OwnerID", "ResolvedAt", "DismissedAt", "CreatedAtKey DESC", "ID DESC", "key DESC"}},
		{"notifications.records", []string{"OwnerID", "ResolvedAt", "DismissedAt", "ReadAt", "CreatedAtKey DESC", "ID DESC", "key DESC"}},
	}...)
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{"im.conversations", []string{"OwnerID", "PeerID", "key"}})
	// Retention aggregates must not read source previews from every summary.
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{
		"debug.summaries", []string{"OwnerID", "Trigger", "Bytes"},
	})
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"sandbox.shell_tasks", []string{"OwnerID", "AssistantID", "StartedAtKey", "key"}},
		{"sandbox.shell_tasks", []string{"OwnerID", "AssistantID", "State", "StartedAtKey", "key"}},
		{"context.counts", []string{"Count", "key"}},
		{"context.references", []string{"OwnerID", "AssistantID", "Kind", "key"}},
		{"agent.model_retries", []string{"OwnerID", "AssistantID", "EventID", "key"}},
	}...)
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{"im.messages", []string{"OwnerKey", "ConversationID", "Sender", "Sequence", "key"}})
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{"business.dream_runs", []string{"OwnerID", "AssistantID", "CreatedAtKey", "key"}})
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"security.permission_requests", []string{"OwnerID", "AssistantID", "State", "CreatedAtKey DESC", "ID", "key DESC"}},
		{"security.permission_requests", []string{"OwnerID", "AssistantID", "CreatedAtKey DESC", "ID", "key DESC", "State"}},
		{"plugins.installations", []string{"OwnerID", "Deleted", "key"}},
		{"plugins.connections", []string{"OwnerID", "PluginID", "Deleted", "key"}},
	}...)
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{"weixin.deliveries", []string{"OwnerKey", "BindingID", "CreatedAtKey", "key"}})
	indexes = append(indexes, []struct {
		bucket string
		fields []string
	}{
		{"auth.sessions", []string{"Session.AccountID", "DeviceDigest", "key"}},
		{"auth.sessions", []string{"Session.AccountID", "RevokedAt", "LastSeenAtKey DESC", "Session.ID DESC", "key DESC"}},
		{"auth.sessions", []string{"RevokedAt", "ExpiresAtKey", "key"}},
		{"auth.sessions", []string{"RevokedAt", "LastSeenAtKey", "key"}},
		{"auth.sessions", []string{"RevokedAtKey", "key"}},
		{"auth.login-links", []string{"ExpiresAtKey", "key"}},
		{"auth.challenges", []string{"ExpiresAtKey", "key"}},
	}...)
	// Schema creation is one atomic commit instead of syncing each index.
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	indexes = append(indexes, struct {
		bucket string
		fields []string
	}{"security.reservations", []string{"OwnerID", "CreatedAt"}})
	for i, index := range indexes {
		parts := []string{"bucket"}
		for _, field := range index.fields {
			name, desc := strings.CutSuffix(field, " DESC")
			expr, err := fieldSQL(name)
			if desc {
				expr += " DESC"
			}
			if err != nil {
				return err
			}
			parts = append(parts, expr)
		}
		where := ""
		if index.bucket != "" {
			where = " WHERE bucket='" + index.bucket + "'"
		}
		if _, err := tx.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS records_query_%d ON records (%s)%s", i, strings.Join(parts, ","), where)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS auth_device_session ON records (json_extract(value, '$.Session.AccountID'), json_extract(value, '$.DeviceDigest')) WHERE bucket='auth.sessions' AND json_extract(value, '$.DeviceDigest') != ''`); err != nil {
		return err
	}
	return tx.Commit()
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
			if f.Op == "has" {
				term, ok := f.Value.(string)
				if !ok {
					return "", core.Invalid("query", "string membership required")
				}
				parts = append(parts, "key IN (SELECT key FROM record_terms WHERE bucket=? AND field=? AND term=?)")
				args = append(args, bucket, f.Field, term)
				continue
			}
			if f.Op == "in" {
				values := reflect.ValueOf(f.Value)
				if !values.IsValid() || values.Kind() != reflect.Slice || values.Len() == 0 || values.Len() > 16384 {
					return "", core.Invalid("query", "invalid membership filter")
				}
				parameters := make([]string, values.Len())
				for i := range parameters {
					parameters[i] = "?"
					args = append(args, values.Index(i).Interface())
				}
				parts = append(parts, expr+" IN ("+strings.Join(parameters, ",")+")")
				continue
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
	orders := []struct {
		field string
		desc  bool
	}{}
	if q.OrderBy != "" && q.OrderBy != "key" {
		orders = append(orders, struct {
			field string
			desc  bool
		}{q.OrderBy, q.Descending})
	}
	if q.ThenBy != "" && q.ThenBy != "key" {
		orders = append(orders, struct {
			field string
			desc  bool
		}{q.ThenBy, q.ThenDescending})
	}
	if q.OrderBy == "" || q.OrderBy == "key" || q.ThenBy != "" || len(q.After) > 0 {
		orders = append(orders, struct {
			field string
			desc  bool
		}{"key", q.Descending})
	}
	orderParts := []string{}
	for _, o := range orders {
		expr, err := fieldSQL(o.field)
		if err != nil {
			return "", nil, err
		}
		if o.desc {
			expr += " DESC"
		}
		orderParts = append(orderParts, expr)
	}
	if len(q.After) > 0 {
		if len(q.After) != len(orders) {
			return "", nil, core.Invalid("cursor", "invalid order position")
		}
		branches := []string{}
		for i, o := range orders {
			terms := []string{}
			for j := 0; j < i; j++ {
				expr, _ := fieldSQL(orders[j].field)
				terms = append(terms, expr+"=?")
				args = append(args, q.After[j])
			}
			expr, _ := fieldSQL(o.field)
			op := ">"
			if o.desc {
				op = "<"
			}
			terms = append(terms, expr+op+"?")
			args = append(args, q.After[i])
			branches = append(branches, "("+strings.Join(terms, " AND ")+")")
		}
		clauses = append(clauses, "("+strings.Join(branches, " OR ")+")")
		if len(orders) > 1 {
			expr, _ := fieldSQL(orders[0].field)
			op := ">="
			if orders[0].desc {
				op = "<="
			}
			clauses = append(clauses, expr+op+"?")
			args = append(args, q.After[0])
		}
	}
	order := strings.Join(orderParts, ",")
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
			parts = append(parts, "'$."+field+"'", "value -> '$."+field+"'")
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

// Count applies the same scope and limit as Query without loading its records.
func (tx *transaction) Count(ctx context.Context, bucket string, q persistence.Query) (int64, error) {
	if err := tx.check(ctx); err != nil {
		return 0, err
	}
	q.Unordered = true
	q.Fields = nil
	query, args, err := querySQL(bucket, q)
	if err != nil {
		return 0, err
	}
	var total int64
	err = tx.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+query+")", args...).Scan(&total)
	return total, translate(ctx, err)
}

// Sum aggregates in SQLite without decoding each matching record in the caller.
func (tx *transaction) Sum(ctx context.Context, bucket string, q persistence.Query, field string) (int64, error) {
	if err := tx.check(ctx); err != nil {
		return 0, err
	}
	expr, err := fieldSQL(field)
	if err != nil {
		return 0, err
	}
	q.Unordered = true
	q.Fields = nil
	query, args, err := querySQL(bucket, q)
	if err != nil {
		return 0, err
	}
	var total int64
	err = tx.sql.QueryRowContext(ctx, "SELECT COALESCE(SUM("+expr+"),0) FROM ("+query+")", args...).Scan(&total)
	return total, translate(ctx, err)
}

// EncodedBytes measures the original encoded values without transferring them
// to the caller. Cast also preserves byte semantics if a value is stored as TEXT.
func (tx *transaction) EncodedBytes(ctx context.Context, bucket string, q persistence.Query) (int64, error) {
	if err := tx.check(ctx); err != nil {
		return 0, err
	}
	if q.Limit == 0 {
		q.Unordered = true
	}
	q.Fields = nil
	query, args, err := querySQL(bucket, q)
	if err != nil {
		return 0, err
	}
	var total int64
	err = tx.sql.QueryRowContext(ctx, "SELECT COALESCE(SUM(length(CAST(value AS BLOB))),0) FROM ("+query+")", args...).Scan(&total)
	return total, translate(ctx, err)
}

// DeleteMatching is used for whole-scope cleanup without loading discarded values.
func (tx *transaction) DeleteMatching(ctx context.Context, bucket string, q persistence.Query) (int64, error) {
	if err := tx.check(ctx); err != nil {
		return 0, err
	}
	if !tx.writable {
		return 0, core.NewError(core.FailedPrecondition, "read-only transaction")
	}
	query, args, err := querySQL(bucket, q)
	if err != nil {
		return 0, err
	}
	args = append([]any{bucket}, args...)
	result, err := tx.sql.ExecContext(ctx, "DELETE FROM records WHERE bucket=? AND key IN (SELECT key FROM ("+query+"))", args...)
	if err != nil {
		return 0, translate(ctx, err)
	}
	n, err := result.RowsAffected()
	return n, translate(ctx, err)
}
