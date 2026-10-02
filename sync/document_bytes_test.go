package sync

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
)

func TestPushMaintainsWinnerDocumentByteLength(t *testing.T) {
	db, err := store.OpenWithOptions(store.Options{Driver: store.DriverSQLite, DSN: filepath.Join(t.TempDir(), "sync.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	service := NewService(repository.New(db))
	parent := ""
	for i, revision := range []string{"1-a", "2-b", "3-c", "4-d"} {
		deleted := i == 2
		body, _ := json.Marshal(map[string]any{"_id": "doc", "_rev": revision, "text": "中文 🧪", "_deleted": deleted})
		change := FullChangeEntry{DocID: "doc", Rev: revision, Doc: body, Deleted: deleted}
		if parent != "" {
			change.ParentRev = &parent
		}
		if _, err := service.Push("owner", "device", []FullChangeEntry{change}, 2); err != nil {
			t.Fatal(err)
		}
		var doc models.Document
		if err := db.First(&doc, "uid = ? AND doc_id = ?", "owner", "doc").Error; err != nil {
			t.Fatal(err)
		}
		if doc.Rev != revision || doc.Deleted != deleted || doc.DocJSONBytes == nil || *doc.DocJSONBytes != int64(len(doc.DocJSON)) {
			t.Fatalf("projection byte count stale after %s: %+v", revision, doc)
		}
		parent = revision
	}
	// A losing branch must not overwrite the current winner's size.
	body := json.RawMessage(`{"_id":"doc","_rev":"1-loser","text":"short"}`)
	if _, err := service.Push("owner", "other", []FullChangeEntry{{DocID: "doc", Rev: "1-loser", Doc: body}}, 2); err != nil {
		t.Fatal(err)
	}
	var doc models.Document
	db.First(&doc, "uid = ? AND doc_id = ?", "owner", "doc")
	if doc.Rev != "4-d" || doc.DocJSONBytes == nil || *doc.DocJSONBytes != int64(len(doc.DocJSON)) {
		t.Fatalf("losing revision changed projection size: %+v", doc)
	}
}
