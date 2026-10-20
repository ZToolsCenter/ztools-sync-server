package sync

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	"gorm.io/gorm"
)

func openRevisionHistoryDB(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := store.OpenWithOptions(store.Options{Driver: store.DriverSQLite, DSN: filepath.Join(t.TempDir(), "history.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestRevisionHistoryBranchesAndIncompleteAncestry(t *testing.T) {
	parents := map[string]string{
		"1-root": "", "2-a": "1-root", "2-b": "1-root", "3-leaf": "2-a",
		"4-gap": "3-missing", "cycle-a": "cycle-b", "cycle-b": "cycle-a",
	}
	for _, test := range []struct {
		name string
		rev  string
		want []string
	}{
		{"root", "1-root", []string{"1-root"}},
		{"branch", "3-leaf", []string{"3-leaf", "2-a", "1-root"}},
		{"sibling", "2-b", []string{"2-b", "1-root"}},
		{"missing parent", "4-gap", []string{"4-gap", "3-missing"}},
		{"unknown revision", "unknown", []string{"unknown"}},
		{"cycle", "cycle-a", []string{"cycle-a", "cycle-b"}},
		{"empty", "", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := revisionHistoryFromParents(test.rev, parents); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("history=%v want=%v", got, test.want)
			}
		})
	}
}

func TestPullHistoriesReuseMetadataWithoutChangingRevisions(t *testing.T) {
	db := openRevisionHistoryDB(t)
	repo := repository.New(db)
	service := NewService(repo)
	rootBody := `{"text":"` + strings.Repeat("large historical body", 4096) + `"}`
	rows := []models.DocumentRevision{
		{UID: "owner", DocID: "doc", Rev: "1-root", DocJSON: rootBody, Generation: 1},
		{UID: "owner", DocID: "doc", Rev: "2-a", ParentRev: "1-root", DocJSON: `{"text":"branch a"}`, Generation: 2, IsLeaf: true},
		{UID: "owner", DocID: "doc", Rev: "2-b", ParentRev: "1-root", DocJSON: `{"text":"branch b"}`, Generation: 2, IsLeaf: true, IsWinner: true},
		{UID: "owner", DocID: "deleted", Rev: "2-tombstone", ParentRev: "1-stub", DocJSON: "{}", Generation: 2, Deleted: true, IsLeaf: true, IsWinner: true},
		{UID: "owner", DocID: "deleted", Rev: "1-stub", DocJSON: "{}", Generation: 1},
		{UID: "other", DocID: "doc", Rev: "2-b", ParentRev: "1-other", DocJSON: "{}", Generation: 2, IsLeaf: true},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// Explicitly clear ancestors: GORM's default:true would otherwise make them leaves.
	if err := db.Model(&models.DocumentRevision{}).Where("rev IN ?", []string{"1-root", "1-stub"}).Update("is_leaf", false).Error; err != nil {
		t.Fatal(err)
	}
	logs := []models.Changelog{
		{UID: "owner", DocID: "doc", Rev: "1-root", ProtocolVersion: 2},
		{UID: "owner", DocID: "doc", Rev: "2-a", WinnerRev: "2-a", ProtocolVersion: 2},
		{UID: "owner", DocID: "doc", Rev: "2-b", WinnerRev: "2-b", ProtocolVersion: 2, Resolution: `{"retireOtherLeaves":true}`},
		{UID: "owner", DocID: "deleted", Rev: "2-tombstone", WinnerRev: "2-tombstone", ProtocolVersion: 2},
		{UID: "owner", DocID: "missing", Rev: "1-gone", ProtocolVersion: 2},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	attachment := models.RevisionAttachment{UID: "owner", DocID: "doc", Rev: "2-b", Name: "file.txt", Digest: "md5-fixture", ContentType: "text/plain", Length: 12, Revpos: 2}
	if err := db.Create(&attachment).Error; err != nil {
		t.Fatal(err)
	}

	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("check_parent_projection", func(tx *gorm.DB) {
		if tx.Statement.Table != "document_revisions" || !reflect.DeepEqual(tx.Statement.Selects, []string{"rev", "parent_rev"}) {
			return
		}
		queries++
		if strings.Contains(tx.Statement.SQL.String(), "doc_json") || strings.Contains(tx.Statement.SQL.String(), "ORDER BY") {
			t.Error("history query loaded bodies or sorted the full history")
		}
	}); err != nil {
		t.Fatal(err)
	}

	changes, seq, err := service.Changes("owner", 0, 2)
	if err != nil || len(changes) != 4 || seq != logs[3].Seq || queries != 2 {
		t.Fatalf("pull: changes=%d seq=%d queries=%d err=%v", len(changes), seq, queries, err)
	}
	for index, change := range changes {
		wantHistory := []string{rows[index].Rev}
		if rows[index].ParentRev != "" {
			wantHistory = append(wantHistory, rows[index].ParentRev)
		}
		if !reflect.DeepEqual(change.RevisionHistory, wantHistory) || change.Seq != logs[index].Seq {
			t.Fatalf("revision or sequence changed: %+v", change)
		}
		if index < 2 && string(change.Doc) != rows[index].DocJSON {
			t.Fatalf("body changed for %s", change.Rev)
		}
	}
	if changes[2].Resolution == nil || !changes[2].Resolution.RetireOtherLeaves || !strings.Contains(string(changes[2].Doc), `"digest":"md5-fixture"`) {
		t.Fatal("resolution or attachment stubs were lost")
	}
	if !changes[3].Deleted || changes[3].Doc != nil {
		t.Fatal("tombstone semantics changed")
	}

	queries = 0
	snapshot, err := service.SnapshotChanges("owner")
	if err != nil || len(snapshot) != 3 || queries != 2 {
		t.Fatalf("snapshot: changes=%d queries=%d err=%v", len(snapshot), queries, err)
	}
	for _, change := range snapshot {
		if change.DocID == "doc" && (change.WinnerRev != "2-b" || len(change.RevisionHistory) != 2 || change.RevisionHistory[1] != "1-root") {
			t.Fatalf("snapshot winner or branch changed: %+v", change)
		}
	}

	// A later pull must observe newly repaired ancestry rather than a process-wide cache.
	if err := db.Model(&models.DocumentRevision{}).Where("uid = ? AND doc_id = ? AND rev = ?", "owner", "doc", "1-root").Update("parent_rev", "0-repaired").Error; err != nil {
		t.Fatal(err)
	}
	changes, _, err = service.Changes("owner", logs[0].Seq, 2)
	if err != nil || len(changes) != 3 || !reflect.DeepEqual(changes[0].RevisionHistory, []string{"2-a", "1-root", "0-repaired"}) {
		t.Fatalf("subsequent pull reused stale history: changes=%d err=%v", len(changes), err)
	}
	otherHistory, err := newRevisionHistoryReader(repo, "other").History("doc", "2-b")
	if err != nil || !reflect.DeepEqual(otherHistory, []string{"2-b", "1-other"}) {
		t.Fatalf("cross-user history contamination: %v %v", otherHistory, err)
	}
	full, err := repo.DocumentRevisions("owner", "doc")
	if err != nil || len(full) != 3 || full[2].DocJSON != rootBody {
		t.Fatal("full document history used by administration no longer includes bodies")
	}
	checkpoint := logs[4].Seq
	empty, seq, err := service.Changes("owner", checkpoint, 2)
	if err != nil || empty == nil || len(empty) != 0 || seq != checkpoint {
		t.Fatalf("empty pull changed: %v %d %v", empty, seq, err)
	}
}

func TestPullHistoryQueryFailureDoesNotAdvanceSequence(t *testing.T) {
	db := openRevisionHistoryDB(t)
	service := NewService(repository.New(db))
	for _, rev := range []string{"1-a", "2-b"} {
		if err := db.Create(&models.DocumentRevision{UID: "owner", DocID: "doc", Rev: rev, DocJSON: "{}"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.Changelog{UID: "owner", DocID: "doc", Rev: rev, ProtocolVersion: 2}).Error; err != nil {
			t.Fatal(err)
		}
	}
	wantErr := errors.New("parent links unavailable")
	if err := db.Callback().Query().Before("gorm:query").Register("fail_parent_query", func(tx *gorm.DB) {
		if reflect.DeepEqual(tx.Statement.Selects, []string{"rev", "parent_rev"}) {
			tx.AddError(wantErr)
		}
	}); err != nil {
		t.Fatal(err)
	}
	changes, seq, err := service.Changes("owner", 1, 2)
	if !errors.Is(err, wantErr) || changes != nil || seq != 1 {
		t.Fatalf("failed history advanced checkpoint: changes=%v seq=%d err=%v", changes, seq, err)
	}
	if _, err := service.SnapshotChanges("owner"); !errors.Is(err, wantErr) {
		t.Fatalf("snapshot swallowed history failure: %v", err)
	}
}

func BenchmarkPullHistoryLookup(b *testing.B) {
	db := openRevisionHistoryDB(b)
	repo := repository.New(db)
	const count = 256
	body := `{"text":"` + strings.Repeat("x", 64*1024) + `"}`
	rows := make([]models.DocumentRevision, count)
	for index := range rows {
		rows[index] = models.DocumentRevision{UID: "owner", DocID: "doc", Rev: fmt.Sprintf("%d-rev", index+1), DocJSON: body, Generation: index + 1}
		if index > 0 {
			rows[index].ParentRev = rows[index-1].Rev
		}
	}
	if err := db.CreateInBatches(&rows, 16).Error; err != nil {
		b.Fatal(err)
	}
	b.Run("full_history_per_change", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			for change := 0; change < 8; change++ {
				if _, err := repo.DocumentRevisions("owner", "doc"); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("parent_links_per_pull", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			reader := newRevisionHistoryReader(repo, "owner")
			for change := 0; change < 8; change++ {
				if _, err := reader.History("doc", rows[count-1-change].Rev); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
