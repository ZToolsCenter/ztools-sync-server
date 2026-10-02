package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestDocumentByteMigrationPreservesLegacyRows(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureMigrationTable(db, DriverSQLite); err != nil {
		t.Fatal(err)
	}
	migrations, err := loadMigrations("migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, migrations[0], DriverSQLite); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO documents(uid,doc_id,doc_json,rev,last_modified) VALUES('owner','doc',?, '1-a',0)", "{\"text\":\"中文\"}"); err != nil {
		t.Fatal(err)
	}
	if err := RunSQLiteMigrations(db); err != nil {
		t.Fatal(err)
	}
	var size sql.NullInt64
	var body string
	if err := db.QueryRow("SELECT doc_json,doc_json_bytes FROM documents WHERE uid='owner' AND doc_id='doc'").Scan(&body, &size); err != nil {
		t.Fatal(err)
	}
	if size.Valid || body != "{\"text\":\"中文\"}" {
		t.Fatalf("migration rewrote legacy data: body=%s size=%v", body, size)
	}
	if err := RunSQLiteMigrations(db); err != nil {
		t.Fatalf("migration rerun failed: %v", err)
	}
}
