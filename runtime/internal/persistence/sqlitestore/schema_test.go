package sqlitestore

import (
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
)

func TestNamedIndexesSurviveReorderingAndRetireOnlyManagedIndexes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "named.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec(`INSERT INTO records VALUES ('fixture','one','{"Value":1}');
		CREATE INDEX records_query_obsolete ON records(bucket);
		CREATE INDEX application_custom ON records(key);`); err != nil {
		t.Fatal(err)
	}
	original := recordIndexes
	recordIndexes = slices.Clone(recordIndexes)
	slices.Reverse(recordIndexes)
	defer func() { recordIndexes = original }()
	if err := s.createIndexes(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='index' AND name GLOB 'records_query_*'").Scan(&count); err != nil || count != len(recordIndexes) {
		t.Fatal("index order changed schema", count, err)
	}
	for _, name := range []string{"application_custom", "auth_device_session", "records_query_im_messages_ownerkey_conversationid_sequence"} {
		if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='index' AND name=?", name).Scan(&count); err != nil || count != 1 {
			t.Fatal("required index lost", name, count, err)
		}
	}
	var value string
	if err := s.db.QueryRow("SELECT value FROM records WHERE bucket='fixture' AND key='one'").Scan(&value); err != nil || value != `{"Value":1}` {
		t.Fatal("index reconciliation changed data", value, err)
	}
}

func TestQueryIndexCreationFailureRollsBackAndCanRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "indexes.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE records (bucket TEXT NOT NULL, key TEXT NOT NULL, value BLOB NOT NULL, PRIMARY KEY(bucket,key));
		INSERT INTO records VALUES ('auth.sessions','one','{"Session":{"AccountID":"owner"},"DeviceDigest":"same"}');
		INSERT INTO records VALUES ('auth.sessions','two','{"Session":{"AccountID":"owner"},"DeviceDigest":"same"}');`); err != nil {
		t.Fatal(err)
	}
	// The final unique index fails after all query indexes have been created.
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("duplicate device sessions accepted")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name GLOB 'records_query_*'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial query schema survived failed initialization: count=%d err=%v", count, err)
	}
	if _, err := db.Exec("DELETE FROM records WHERE key='two'"); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='auth_device_session'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry did not install unique index: count=%d err=%v", count, err)
	}
}
