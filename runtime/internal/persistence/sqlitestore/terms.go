package sqlitestore

import "fmt"

// SQLite's scalar expression indexes cannot index members of a JSON array.
// This derived index keeps those memberships transactionally aligned even when
// a caller updates/deletes a record through a different domain service.
func (s *Store) createTermIndexes() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='record_terms')").Scan(&exists); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS record_terms (
 bucket TEXT NOT NULL, key TEXT NOT NULL, field TEXT NOT NULL, term TEXT NOT NULL,
 PRIMARY KEY(bucket,field,term,key), FOREIGN KEY(bucket,key) REFERENCES records(bucket,key) ON DELETE CASCADE
 ); CREATE INDEX IF NOT EXISTS record_terms_source ON record_terms(bucket,key)`); err != nil {
		return err
	}
	for _, spec := range []struct{ bucket, field, path, expression string }{
		{"im.messages", "Content.AttachmentIDs", "$.Content.AttachmentIDs", "CAST(j.value AS TEXT)"},
		{"business.plan-links", "Dependencies", "$.Dependencies", "json_extract(j.value,'$.Kind') || '/' || json_extract(j.value,'$.ID')"},
		{"business.dream_runs", "MemorySources", "$.MemorySources", "json_extract(j.value,'$.Kind') || '/' || json_extract(j.value,'$.ID')"},
		{"business.dream_runs", "Sources.Evidence", "$.Sources", "json_extract(j.value,'$.Source.Kind') || '/' || json_extract(j.value,'$.Source.ID') || '/' || json_extract(j.value,'$.Version')"},
		{"business.dream_runs", "Sources.Source", "$.Sources", "json_extract(j.value,'$.Source.Kind') || '/' || json_extract(j.value,'$.Source.ID')"},
	} {
		name := fmt.Sprintf("terms_%x", spec.bucket+"/"+spec.field)
		insert := fmt.Sprintf("INSERT OR IGNORE INTO record_terms(bucket,key,field,term) SELECT NEW.bucket,NEW.key,'%s',%s FROM json_each(NEW.value,'%s') j WHERE %s IS NOT NULL;", spec.field, spec.expression, spec.path, spec.expression)
		for _, action := range []string{"INSERT", "UPDATE"} {
			statement := fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS %s_%s AFTER %s ON records WHEN NEW.bucket='%s' BEGIN DELETE FROM record_terms WHERE bucket=NEW.bucket AND key=NEW.key AND field='%s'; %s END", name, action, action, spec.bucket, spec.field, insert)
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
		if !exists {
			statement := fmt.Sprintf("INSERT OR IGNORE INTO record_terms(bucket,key,field,term) SELECT r.bucket,r.key,'%s',%s FROM records r,json_each(r.value,'%s') j WHERE r.bucket=? AND %s IS NOT NULL", spec.field, spec.expression, spec.path, spec.expression)
			if _, err := tx.Exec(statement, spec.bucket); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
