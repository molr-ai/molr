package sqlitestore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

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
