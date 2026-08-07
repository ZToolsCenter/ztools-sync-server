package store

import (
	"path/filepath"
	"testing"
)

func TestOpenSQLiteCreatesStandaloneSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ztools.db")
	db, err := OpenWithOptions(Options{Driver: DriverSQLite, DSN: path})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite handle: %v", err)
	}
	defer sqlDB.Close()

	for _, table := range []string{"users", "documents", "attachment_blobs", "sync_checkpoints"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing standalone table %s", table)
		}
	}
	if db.Migrator().HasTable("plugins") {
		t.Fatal("standalone schema must not include SaaS plugin tables")
	}

	var journalMode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatalf("read sqlite journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected WAL journal mode, got %s", journalMode)
	}
}
