package repository

import (
	"context"

	"github.com/ZToolsCenter/ztools-sync-server/models"
)

// WithContext binds every query made by the returned repository to a request.
func (r *Repository) WithContext(ctx context.Context) *Repository {
	return New(r.db.WithContext(ctx))
}

// ChangelogPage reads a bounded page inside a fixed replication high-water mark.
func (r *Repository) ChangelogPage(uid string, after int64, through int64, limit int) ([]models.Changelog, error) {
	var rows []models.Changelog
	err := r.db.Where("uid = ? AND seq > ? AND seq <= ?", uid, after, through).
		Order("seq ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// RevisionParent contains no document payload.
type RevisionParent struct {
	Rev       string
	ParentRev string
}

// RevisionParentsPage follows the primary key instead of sorting full document bodies.
func (r *Repository) RevisionParentsPage(uid string, docID string, afterRev string, first bool, limit int) ([]RevisionParent, error) {
	query := r.db.Model(&models.DocumentRevision{}).Select("rev", "parent_rev").
		Where("uid = ? AND doc_id = ?", uid, docID)
	if !first {
		query = query.Where("rev > ?", afterRev)
	}
	var rows []RevisionParent
	err := query.Order("rev ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// LeafRevisionHeadersPage enumerates snapshot keys without retaining their bodies.
func (r *Repository) LeafRevisionHeadersPage(uid string, afterDocID string, afterRev string, first bool, limit int) ([]models.DocumentRevision, error) {
	query := r.db.Model(&models.DocumentRevision{}).Select("doc_id", "rev").
		Where("uid = ? AND is_leaf = ?", uid, true)
	if !first {
		query = query.Where("(doc_id > ? OR (doc_id = ? AND rev > ?))", afterDocID, afterDocID, afterRev)
	}
	var rows []models.DocumentRevision
	err := query.Order("doc_id ASC, rev ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// DocumentLeafHeadersPage supports winner election with the protocol's Go revision
// comparator, independently of database collation or legacy generation columns.
func (r *Repository) DocumentLeafHeadersPage(uid string, docID string, afterRev string, first bool, limit int) ([]models.DocumentRevision, error) {
	query := r.db.Model(&models.DocumentRevision{}).Select("rev", "deleted").
		Where("uid = ? AND doc_id = ? AND is_leaf = ?", uid, docID, true)
	if !first {
		query = query.Where("rev > ?", afterRev)
	}
	var rows []models.DocumentRevision
	err := query.Order("rev ASC").Limit(limit).Find(&rows).Error
	return rows, err
}
