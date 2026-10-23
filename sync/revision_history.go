package sync

import (
	"container/list"
	"context"
	"errors"
	"strings"

	"github.com/ZToolsCenter/ztools-sync-server/repository"
)

const revisionParentPageSize = 4096
const maxCachedRevisionParents = 250000
const maxCachedHistoryDocuments = 64

var ErrPullHistoryTooLarge = errors.New("document revision history exceeds the server safety limit")

type revisionHistoryCacheEntry struct {
	docID   string
	parents map[string]string
}

// revisionHistoryReader keeps only parent links, scoped to one user's pull request.
// It never retains document bodies or shares cached history across requests.
type revisionHistoryReader struct {
	repo       *repository.Repository
	uid        string
	ctx        context.Context
	entries    map[string]*list.Element
	order      list.List
	linkCount  int
	maxParents int
	history    []string
	visited    map[string]bool
	normalized map[string]bool
}

func newRevisionHistoryReader(repo *repository.Repository, uid string) *revisionHistoryReader {
	return &revisionHistoryReader{
		repo:       repo,
		uid:        uid,
		ctx:        repo.DB().Statement.Context,
		entries:    make(map[string]*list.Element),
		visited:    make(map[string]bool),
		normalized: make(map[string]bool),
		maxParents: maxCachedRevisionParents,
	}
}

// History loads each document's links once and follows the requested branch.
func (r *revisionHistoryReader) History(docID string, rev string) ([]string, error) {
	parents, err := r.parents(docID)
	if err != nil {
		return nil, err
	}
	return revisionHistoryFromParents(rev, parents), nil
}

// WithHistory reuses one branch buffer. The callback must not retain the slice.
// Normalization matches normalizeRevisionHistory without making another full copy.
func (r *revisionHistoryReader) WithHistory(docID string, rev string, consume func([]string) error) error {
	parents, err := r.parents(docID)
	if err != nil {
		return err
	}
	r.history = r.history[:0]
	clear(r.visited)
	clear(r.normalized)
	for steps := 0; rev != "" && !r.visited[rev]; steps++ {
		if steps%256 == 0 {
			if err := r.ctx.Err(); err != nil {
				return err
			}
		}
		r.visited[rev] = true
		if normalized := strings.TrimSpace(rev); normalized != "" && !r.normalized[normalized] {
			r.history = append(r.history, normalized)
			r.normalized[normalized] = true
		}
		rev = parents[rev]
	}
	return consume(r.history)
}

func (r *revisionHistoryReader) parents(docID string) (map[string]string, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if element := r.entries[docID]; element != nil {
		r.order.MoveToFront(element)
		return element.Value.(revisionHistoryCacheEntry).parents, nil
	}
	parents := make(map[string]string)
	after := ""
	for first := true; ; first = false {
		rows, err := r.repo.RevisionParentsPage(r.uid, docID, after, first, revisionParentPageSize)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if len(parents) >= r.maxParents {
				return nil, ErrPullHistoryTooLarge
			}
			parents[row.Rev] = row.ParentRev
		}
		if len(rows) < revisionParentPageSize {
			break
		}
		after = rows[len(rows)-1].Rev
	}
	// Retain a bounded working set instead of every document visited by a pull.
	for r.order.Len() >= maxCachedHistoryDocuments || r.linkCount+len(parents) > r.maxParents {
		oldest := r.order.Back()
		entry := oldest.Value.(revisionHistoryCacheEntry)
		delete(r.entries, entry.docID)
		r.linkCount -= len(entry.parents)
		r.order.Remove(oldest)
	}
	r.entries[docID] = r.order.PushFront(revisionHistoryCacheEntry{docID: docID, parents: parents})
	r.linkCount += len(parents)
	return parents, nil
}

func revisionHistoryFromParents(rev string, parents map[string]string) []string {
	history := []string{}
	seen := map[string]bool{}
	for rev != "" && !seen[rev] {
		history = append(history, rev)
		seen[rev] = true
		rev = parents[rev]
	}
	return history
}
