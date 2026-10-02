package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	"gorm.io/gorm"
)

func TestDocumentByteBackfillResumesAndPreservesSyncWrites(t *testing.T) {
	db := openDocumentBytesDB(t)
	knownBytes := int64(99)
	docs := []models.Document{
		{UID: "a", DocID: "1", DocJSON: "{\"text\":\"中文\"}", Rev: "1-a"},
		{UID: "a", DocID: "2", DocJSON: "{}", Rev: "1-b", DocJSONBytes: &knownBytes},
		{UID: "b", DocID: "1", DocJSON: "", Rev: "1-c", Deleted: true},
		{UID: "b", DocID: "2", DocJSON: "{\"text\":\"emoji: 🧪\"}", Rev: "1-d"},
	}
	if err := db.Create(&docs).Error; err != nil {
		t.Fatal(err)
	}
	complete, err := New(db).BackfillDocumentJSONBytes(context.Background(), 2)
	if err != nil || complete {
		t.Fatalf("first batch: complete=%v err=%v", complete, err)
	}
	var remaining int64
	db.Model(&models.Document{}).Where("doc_json_bytes IS NULL").Count(&remaining)
	if remaining != 2 {
		t.Fatalf("batch did not stop at two keys: remaining=%d", remaining)
	}

	// A new repository instance must continue from the persisted cursor, not start from zero.
	resumed := New(db)
	complete, err = resumed.BackfillDocumentJSONBytes(context.Background(), 2)
	if err != nil || complete {
		t.Fatalf("second batch: complete=%v err=%v", complete, err)
	}
	complete, err = resumed.BackfillDocumentJSONBytes(context.Background(), 2)
	if err != nil || !complete {
		t.Fatalf("final batch: complete=%v err=%v", complete, err)
	}
	var actual []models.Document
	if err := db.Order("uid, doc_id").Find(&actual).Error; err != nil {
		t.Fatal(err)
	}
	for i, doc := range actual {
		expected := int64(len(docs[i].DocJSON))
		if i == 1 {
			expected = knownBytes
		}
		if doc.DocJSONBytes == nil || *doc.DocJSONBytes != expected {
			t.Fatalf("%s/%s bytes=%v want=%d", doc.UID, doc.DocID, doc.DocJSONBytes, expected)
		}
	}
	if complete, err := resumed.BackfillDocumentJSONBytes(context.Background(), 2); err != nil || !complete {
		t.Fatalf("completed backfill was not idempotent: %v %v", complete, err)
	}
}

func TestDocumentByteBackfillRollsBackWithCursor(t *testing.T) {
	db := openDocumentBytesDB(t)
	if err := db.Create(&models.Document{UID: "owner", DocID: "doc", DocJSON: "{}", Rev: "1-a"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_cursor BEFORE UPDATE ON sync_meta BEGIN SELECT RAISE(ABORT, 'cursor write failed'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db).BackfillDocumentJSONBytes(context.Background(), 1); err == nil {
		t.Fatal("expected cursor write error")
	}
	var doc models.Document
	if err := db.First(&doc).Error; err != nil || doc.DocJSONBytes != nil {
		t.Fatalf("document update did not roll back: doc=%+v err=%v", doc, err)
	}
	if err := db.Exec("DROP TRIGGER reject_cursor").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := New(db).BackfillDocumentJSONBytes(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(db).BackfillDocumentJSONBytes(ctx, 1); err == nil {
		t.Fatal("expected cancellation to stop the batch")
	}
}

func openDocumentBytesDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.OpenWithOptions(store.Options{Driver: store.DriverSQLite, DSN: filepath.Join(t.TempDir(), "bytes.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	return db
}
