package sync

import (
	"context"
	"errors"
	"time"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	"gorm.io/gorm"
)

const pullPageSize = 128
const MaxPullDuration = 5 * time.Minute

var errMissingPullRevision = errors.New("pull revision no longer exists")

// ChangeConsumer consumes one change before the next is built. RevisionHistory
// is borrowed until the callback returns; consumers retaining changes must copy it.
type ChangeConsumer func(FullChangeEntry) error

// StreamChanges emits the existing complete changes response with bounded memory.
// It never advances a device checkpoint; only the client can acknowledge a result.
func (s *Service) StreamChanges(ctx context.Context, uid string, since int64, clientProtocolVersion int, consume ChangeConsumer) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, MaxPullDuration)
	defer cancel()
	release, err := s.pulls.acquire(ctx, uid)
	if err != nil {
		return since, err
	}
	defer release()
	repo := s.repo.WithContext(ctx)
	through, err := repo.MaxSeq(uid)
	if err != nil {
		return since, err
	}
	histories := newRevisionHistoryReader(repo, uid)
	after, lastSeq := since, since
	for after < through {
		logs, err := repo.ChangelogPage(uid, after, through, pullPageSize)
		if err != nil {
			return since, err
		}
		if len(logs) == 0 {
			break
		}
		for _, entry := range logs {
			after = entry.Seq
			if clientProtocolVersion < store.ProtocolVersion && entry.ProtocolVersion < store.ProtocolVersion {
				continue
			}
			err := emitRevision(ctx, repo, histories, uid, entry.DocID, entry.Rev, entry.Seq, entry.WinnerRev, parseResolution(entry.Resolution), consume)
			if errors.Is(err, errMissingPullRevision) {
				continue
			}
			if err != nil {
				return since, err
			}
			lastSeq = entry.Seq
		}
	}
	return lastSeq, ctx.Err()
}

// StreamSnapshotChanges emits current leaves without loading all snapshot bodies.
// The sequence is captured before enumeration so concurrent writes are never skipped.
func (s *Service) StreamSnapshotChanges(ctx context.Context, uid string, consume ChangeConsumer) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, MaxPullDuration)
	defer cancel()
	release, err := s.pulls.acquire(ctx, uid)
	if err != nil {
		return 0, err
	}
	defer release()
	repo := s.repo.WithContext(ctx)
	seq, err := repo.MaxSeq(uid)
	if err != nil {
		return 0, err
	}
	histories := newRevisionHistoryReader(repo, uid)
	afterDoc, afterRev, currentDoc, winnerRev := "", "", "", ""
	for first := true; ; first = false {
		rows, err := repo.LeafRevisionHeadersPage(uid, afterDoc, afterRev, first, pullPageSize)
		if err != nil {
			return 0, err
		}
		for index, row := range rows {
			if first && index == 0 || row.DocID != currentDoc {
				currentDoc = row.DocID
				winnerRev, err = snapshotWinnerRev(ctx, repo, uid, currentDoc)
				if err != nil {
					return 0, err
				}
			}
			if err := emitRevision(ctx, repo, histories, uid, row.DocID, row.Rev, 0, winnerRev, nil, consume); err != nil {
				return 0, err
			}
		}
		if len(rows) < pullPageSize {
			break
		}
		last := rows[len(rows)-1]
		afterDoc, afterRev = last.DocID, last.Rev
	}
	return seq, ctx.Err()
}

func snapshotWinnerRev(ctx context.Context, repo *repository.Repository, uid string, docID string) (string, error) {
	var winner models.DocumentRevision
	found, after := false, ""
	for first := true; ; first = false {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		leaves, err := repo.DocumentLeafHeadersPage(uid, docID, after, first, pullPageSize)
		if err != nil {
			return "", err
		}
		for _, leaf := range leaves {
			if !found || winner.Deleted && !leaf.Deleted || winner.Deleted == leaf.Deleted && compareRevs(leaf.Rev, winner.Rev) > 0 {
				winner, found = leaf, true
			}
		}
		if len(leaves) < pullPageSize {
			break
		}
		after = leaves[len(leaves)-1].Rev
	}
	if !found {
		return "", gorm.ErrRecordNotFound
	}
	return winner.Rev, nil
}

func emitRevision(ctx context.Context, repo *repository.Repository, histories *revisionHistoryReader, uid string, docID string, rev string, seq int64, winnerRev string, resolution *Resolution, consume ChangeConsumer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	revision, err := repo.Revision(uid, docID, rev)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errMissingPullRevision
	}
	if err != nil {
		return err
	}
	attachments, err := repo.RevisionAttachments(uid, docID, rev)
	if err != nil {
		return err
	}
	return histories.WithHistory(docID, rev, func(history []string) error {
		change := buildDocChangeFromRevisionWithAttachmentsAndHistory(revision, attachments, nil, seq, winnerRev, resolution)
		change.RevisionHistory = history
		return consume(change)
	})
}

// collectChanges preserves the in-process slice API. Network handlers use the
// streaming APIs so they never retain the complete response or all ancestor lists.
func collectChanges(changes *[]FullChangeEntry) ChangeConsumer {
	return func(change FullChangeEntry) error {
		change.RevisionHistory = append([]string(nil), change.RevisionHistory...)
		*changes = append(*changes, change)
		return nil
	}
}
