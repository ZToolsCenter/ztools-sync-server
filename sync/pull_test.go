package sync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"gorm.io/gorm"
)

func seedPullChain(t testing.TB, db *gorm.DB, count int) {
	t.Helper()
	revisions := make([]models.DocumentRevision, count)
	logs := make([]models.Changelog, count)
	for i := range revisions {
		rev := fmt.Sprintf("%d-fixture", i+1)
		parent := ""
		if i > 0 {
			parent = revisions[i-1].Rev
		}
		revisions[i] = models.DocumentRevision{UID: "owner", DocID: "doc", Rev: rev, ParentRev: parent, Generation: i + 1, DocJSON: "{}"}
		logs[i] = models.Changelog{UID: "owner", DocID: "doc", Rev: rev, ProtocolVersion: 2}
	}
	if err := db.CreateInBatches(&revisions, 128).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&logs, 128).Error; err != nil {
		t.Fatal(err)
	}
}

func TestLargePullRetainsBoundedHistoryMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("large history regression")
	}
	db := openRevisionHistoryDB(t)
	const count = 12000
	seedPullChain(t, db, count)
	service := NewService(repository.New(db))
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	observed, pages := 0, 0
	if err := db.Callback().Query().After("gorm:query").Register("bounded_pull_pages", func(tx *gorm.DB) {
		if tx.Statement.Table == "changelog" {
			pages++
			if _, ok := tx.Statement.Clauses["LIMIT"]; !ok && len(tx.Statement.Selects) == 0 {
				t.Error("pull queried an unbounded changelog")
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	seq, err := service.StreamChanges(context.Background(), "owner", 0, 2, func(change FullChangeEntry) error {
		observed++
		if change.Seq != int64(observed) || len(change.RevisionHistory) != observed || change.RevisionHistory[0] != change.Rev || change.RevisionHistory[observed-1] != "1-fixture" {
			t.Fatalf("history or sequence changed at revision %d", observed)
		}
		if observed%3000 == 0 {
			runtime.GC()
			var current runtime.MemStats
			runtime.ReadMemStats(&current)
			if current.HeapAlloc > before.HeapAlloc+64*1024*1024 {
				t.Fatalf("pull retained previous ancestor arrays: heap growth %.1f MiB", float64(current.HeapAlloc-before.HeapAlloc)/1048576)
			}
		}
		return nil
	})
	if err != nil || seq != count || observed != count || pages < count/pullPageSize {
		t.Fatalf("large pull: seq=%d observed=%d pages=%d err=%v", seq, observed, pages, err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	t.Logf("12,000-revision pull: retained %.1f MiB; cumulative allocation %.1f MiB", float64(after.HeapAlloc-before.HeapAlloc)/1048576, float64(after.TotalAlloc-before.TotalAlloc)/1048576)
}

func TestVeryLongHistoryIsCompleteAndLoadedInPages(t *testing.T) {
	if testing.Short() {
		t.Skip("120,060-parent history regression")
	}
	db := openRevisionHistoryDB(t)
	const count = 120060
	rows := make([]models.DocumentRevision, 500)
	for start := 0; start < count; start += len(rows) {
		batch := len(rows)
		if count-start < batch {
			batch = count - start
		}
		for i := 0; i < batch; i++ {
			generation := start + i + 1
			parent := ""
			if generation > 1 {
				parent = fmt.Sprintf("%d-fixture", generation-1)
			}
			rows[i] = models.DocumentRevision{UID: "owner", DocID: "doc", Rev: fmt.Sprintf("%d-fixture", generation), ParentRev: parent, Generation: generation, DocJSON: "{}"}
		}
		if err := db.Create(rows[:batch]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.Changelog{UID: "owner", DocID: "doc", Rev: fmt.Sprintf("%d-fixture", count), ProtocolVersion: 2}).Error; err != nil {
		t.Fatal(err)
	}
	pages := 0
	if err := db.Callback().Query().After("gorm:query").Register("parent_page_count", func(tx *gorm.DB) {
		if reflect.DeepEqual(tx.Statement.Selects, []string{"rev", "parent_rev"}) {
			pages++
			if _, ok := tx.Statement.Clauses["LIMIT"]; !ok {
				t.Error("long history was queried without a page limit")
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	seen := 0
	seq, err := NewService(repository.New(db)).StreamChanges(context.Background(), "owner", 0, 2, func(change FullChangeEntry) error {
		seen++
		if len(change.RevisionHistory) != count || change.RevisionHistory[count-1] != "1-fixture" || change.RevisionHistory[0] != fmt.Sprintf("%d-fixture", count) {
			t.Fatal("long history was truncated")
		}
		runtime.GC()
		var during runtime.MemStats
		runtime.ReadMemStats(&during)
		growth := float64(during.HeapAlloc-before.HeapAlloc) / 1048576
		t.Logf("120,060-parent history: retained %.1f MiB", growth)
		if growth > 96 {
			t.Fatalf("long history exceeded its bounded working set: %.1f MiB", growth)
		}
		return nil
	})
	if err != nil || seen != 1 || seq != 1 || pages < count/revisionParentPageSize {
		t.Fatalf("long history: seen=%d seq=%d pages=%d err=%v", seen, seq, pages, err)
	}
}

func TestStreamPullCancellationAndHighWaterMark(t *testing.T) {
	db := openRevisionHistoryDB(t)
	seedPullChain(t, db, pullPageSize+2)
	service := NewService(repository.New(db))
	ctx, cancel := context.WithCancel(context.Background())
	observed := 0
	seq, err := service.StreamChanges(ctx, "owner", 0, 2, func(FullChangeEntry) error {
		observed++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || seq != 0 || observed != 1 {
		t.Fatalf("canceled pull kept querying or advanced sequence: %d %d %v", observed, seq, err)
	}
	observed = 0
	seq, err = service.StreamChanges(context.Background(), "owner", 0, 2, func(FullChangeEntry) error {
		observed++
		if observed == 1 {
			if err := db.Create(&models.DocumentRevision{UID: "owner", DocID: "later", Rev: "1-later", DocJSON: "{}"}).Error; err != nil {
				return err
			}
			return db.Create(&models.Changelog{UID: "owner", DocID: "later", Rev: "1-later", ProtocolVersion: 2}).Error
		}
		return nil
	})
	if err != nil || seq != pullPageSize+2 || observed != pullPageSize+2 {
		t.Fatalf("pull chased concurrent writes: %d %d %v", observed, seq, err)
	}
	var after []FullChangeEntry
	next, err := service.StreamChanges(context.Background(), "owner", seq, 2, collectChanges(&after))
	if err != nil || len(after) != 1 || next != seq+1 || after[0].DocID != "later" {
		t.Fatalf("next pull lost concurrent write: %d %v %v", next, after, err)
	}
	consumerErr := errors.New("socket closed")
	seq, err = service.StreamChanges(context.Background(), "owner", 0, 2, func(FullChangeEntry) error { return consumerErr })
	if seq != 0 || !errors.Is(err, consumerErr) {
		t.Fatalf("consumer failure advanced sequence: %d %v", seq, err)
	}
}

func TestSnapshotPagesAndConcurrentSequence(t *testing.T) {
	db := openRevisionHistoryDB(t)
	const count = pullPageSize + 10
	for _, uid := range []string{"owner", "other"} {
		for i := 0; i < count; i++ {
			docID := fmt.Sprintf("doc-%03d", i)
			if err := db.Create(&models.DocumentRevision{UID: uid, DocID: docID, Rev: "1-root", DocJSON: "{}", Generation: 1, IsLeaf: true}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Create(&models.Changelog{UID: "owner", DocID: "doc-000", Rev: "1-root", ProtocolVersion: 2}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(repository.New(db))
	observed := map[string]bool{}
	seq, err := service.StreamSnapshotChanges(context.Background(), "owner", func(change FullChangeEntry) error {
		if observed[change.DocID] {
			t.Fatalf("snapshot repeated a key at a page boundary: %s", change.DocID)
		}
		observed[change.DocID] = true
		if len(observed) == 1 {
			return db.Create(&models.Changelog{UID: "owner", DocID: "doc-000", Rev: "1-root", ProtocolVersion: 2}).Error
		}
		return nil
	})
	if err != nil || seq != 1 || len(observed) != count {
		t.Fatalf("snapshot crossed user or high-water boundary: seq=%d docs=%d err=%v", seq, len(observed), err)
	}
}

func TestPullLimiterQueuesPerUserWithoutBlockingOtherUsers(t *testing.T) {
	limiter := newPullLimiter(2)
	first, err := limiter.acquire(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		release, err := limiter.acquire(ctx, "owner")
		if err == nil {
			release()
		}
		done <- err
	}()
	other, err := limiter.acquire(ctx, "other")
	if err != nil {
		t.Fatal("queued reconnect occupied the second global slot", err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("same-user pull was not canceled: %v", err)
	}
	first()
	other()
	if len(limiter.users) != 0 || len(limiter.slots) != 0 {
		t.Fatal("pull admission leaked users or slots")
	}
}

func TestHistoryCacheEvictsAndPreservesNormalization(t *testing.T) {
	db := openRevisionHistoryDB(t)
	reader := newRevisionHistoryReader(repository.New(db), "owner")
	for i := 0; i < maxCachedHistoryDocuments+1; i++ {
		docID := fmt.Sprintf("doc-%d", i)
		if err := db.Create(&models.DocumentRevision{UID: "owner", DocID: docID, Rev: " 2-child ", ParentRev: "1-parent", DocJSON: "{}"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := reader.WithHistory(docID, " 2-child ", func(history []string) error {
			if len(history) != 2 || history[0] != "2-child" || history[1] != "1-parent" {
				t.Fatalf("normalization changed: %v", history)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(reader.entries) != maxCachedHistoryDocuments || reader.entries["doc-0"] != nil {
		t.Fatal("history cache retained every document in the request")
	}
}

func TestHistoryBudgetFailsWithoutTruncatingAncestry(t *testing.T) {
	db := openRevisionHistoryDB(t)
	seedPullChain(t, db, 3)
	reader := newRevisionHistoryReader(repository.New(db), "owner")
	reader.maxParents = 2
	called := false
	err := reader.WithHistory("doc", "3-fixture", func([]string) error { called = true; return nil })
	if !errors.Is(err, ErrPullHistoryTooLarge) || called || len(reader.entries) != 0 {
		t.Fatalf("over-budget history was silently truncated or cached: called=%t entries=%d err=%v", called, len(reader.entries), err)
	}
}

func TestSnapshotWinnerPreservesLegacyComparison(t *testing.T) {
	db := openRevisionHistoryDB(t)
	rows := []models.DocumentRevision{
		{UID: "owner", DocID: "doc", Rev: "2-Z", Generation: 999, IsLeaf: true, IsWinner: true, DocJSON: "{}"},
		{UID: "owner", DocID: "doc", Rev: "2-a", IsLeaf: true, DocJSON: "{}"},
		{UID: "owner", DocID: "doc", Rev: "3-deleted", Generation: 3, IsLeaf: true, Deleted: true, DocJSON: "{}"},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(repository.New(db))
	_, err := service.StreamSnapshotChanges(context.Background(), "owner", func(change FullChangeEntry) error {
		if change.WinnerRev != "2-a" || change.IsWinner != (change.Rev == "2-a") {
			t.Fatalf("winner changed with collation or stale generation flags: %+v", change)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
