package sqlitestore

import (
	"context"
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
	if q.IncludeEncodedBytes {
		selection += ",length(CAST(value AS BLOB))"
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
		columns := []any{&row.Key, &raw}
		if q.IncludeEncodedBytes {
			columns = append(columns, &row.EncodedBytes)
		}
		if err := rows.Scan(columns...); err != nil {
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
