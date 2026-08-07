package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ZToolsCenter/ztools-sync-server/models"
)

// Repository provides persistence operations required by the sync protocol.
type Repository struct {
	db *gorm.DB
}

// New creates a sync repository backed by the provided GORM database.
func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB returns the underlying database for transactional sync operations.
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// MaxSeq returns the latest committed changelog sequence for a user.
func (r *Repository) MaxSeq(uid string) (int64, error) {
	var seq int64
	err := r.db.Model(&models.Changelog{}).
		Where("uid = ?", uid).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&seq).Error
	return seq, err
}

// SyncMeta loads one server-level synchronization metadata value.
func (r *Repository) SyncMeta(key string) (models.SyncMeta, error) {
	var meta models.SyncMeta
	err := r.db.First(&meta, "`key` = ?", key).Error
	return meta, err
}

// UpsertDeviceState creates or replaces a device checkpoint summary.
func (r *Repository) UpsertDeviceState(state models.DeviceSyncState) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}, {Name: "device_id"}},
		UpdateAll: true,
	}).Create(&state).Error
}

// UpdateDeviceSeq advances a device's acknowledged server sequence.
func (r *Repository) UpdateDeviceSeq(uid string, deviceID string, seq int64, lastSeen int64) error {
	return r.db.Model(&models.DeviceSyncState{}).
		Where("uid = ? AND device_id = ?", uid, deviceID).
		Updates(map[string]interface{}{"last_seq": seq, "last_seen": lastSeen}).Error
}

// Transaction executes a sync operation in one database transaction.
func (r *Repository) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}

// AcquireDocumentWriteLock serializes writes for one user document in a transaction.
func AcquireDocumentWriteLock(tx *gorm.DB, uid string, docID string, updatedAt int64) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}, {Name: "doc_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
	}).Create(&models.DocumentWriteLock{
		UID:       uid,
		DocID:     docID,
		UpdatedAt: updatedAt,
	}).Error
}

// ChangelogSince returns ordered changes newer than the supplied sequence.
func (r *Repository) ChangelogSince(uid string, since int64) ([]models.Changelog, error) {
	var logs []models.Changelog
	err := r.db.Where("uid = ? AND seq > ?", uid, since).Order("seq ASC").Find(&logs).Error
	return logs, err
}

// Revision loads one exact document revision.
func (r *Repository) Revision(uid string, docID string, rev string) (models.DocumentRevision, error) {
	var revision models.DocumentRevision
	err := r.db.First(&revision, "uid = ? AND doc_id = ? AND rev = ?", uid, docID, rev).Error
	return revision, err
}

// LeafRevisions returns every current leaf revision for snapshot synchronization.
func (r *Repository) LeafRevisions(uid string) ([]models.DocumentRevision, error) {
	var rows []models.DocumentRevision
	err := r.db.Where("uid = ? AND is_leaf = ?", uid, true).
		Order("doc_id ASC, generation DESC, rev DESC").
		Find(&rows).Error
	return rows, err
}

// DocumentRevisions returns every stored revision for one user document.
func (r *Repository) DocumentRevisions(uid string, docID string) ([]models.DocumentRevision, error) {
	var rows []models.DocumentRevision
	err := r.db.Where("uid = ? AND doc_id = ?", uid, docID).
		Order("generation DESC, rev DESC").
		Find(&rows).Error
	return rows, err
}

// Attachments lists attachments referenced by current winning revisions.
func (r *Repository) Attachments(uid string) ([]models.RevisionAttachment, error) {
	var rows []models.RevisionAttachment
	err := r.db.Table("revision_attachments AS ra").
		Select("ra.*").
		Joins(currentRevisionAttachmentJoin()).
		Where("ra.uid = ?", uid).
		Order("ra.created_at DESC").
		Find(&rows).Error
	return rows, err
}

// SyncCheckpoint loads one replication checkpoint.
func (r *Repository) SyncCheckpoint(uid string, checkpointID string) (models.SyncCheckpoint, error) {
	var checkpoint models.SyncCheckpoint
	err := r.db.First(&checkpoint, "uid = ? AND checkpoint_id = ?", uid, checkpointID).Error
	return checkpoint, err
}

// UpsertSyncCheckpoint stores the latest state for one replication checkpoint.
func (r *Repository) UpsertSyncCheckpoint(checkpoint models.SyncCheckpoint) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "uid"}, {Name: "checkpoint_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_id", "target_id", "last_seq", "checkpoint_json", "updated_at",
		}),
	}).Create(&checkpoint).Error
}

// AttachmentBlob loads a content-addressed attachment blob.
func (r *Repository) AttachmentBlob(uid string, digest string) (models.AttachmentBlob, error) {
	var blob models.AttachmentBlob
	err := r.db.First(&blob, "uid = ? AND digest = ?", uid, digest).Error
	return blob, err
}

// AttachmentBlobDigestSet reports which requested digests already exist.
func (r *Repository) AttachmentBlobDigestSet(uid string, digests []string) (map[string]bool, error) {
	result := make(map[string]bool, len(digests))
	for _, digest := range digests {
		result[digest] = false
	}
	if len(digests) == 0 {
		return result, nil
	}
	var rows []models.AttachmentBlob
	err := r.db.Select("digest").Where("uid = ? AND digest IN ?", uid, digests).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Digest] = true
	}
	return result, nil
}

// RevisionAttachments lists the attachment stubs attached to one revision.
func (r *Repository) RevisionAttachments(uid string, docID string, rev string) ([]models.RevisionAttachment, error) {
	var rows []models.RevisionAttachment
	err := r.db.Where("uid = ? AND doc_id = ? AND rev = ?", uid, docID, rev).
		Order("name ASC").
		Find(&rows).Error
	return rows, err
}

// UpsertAttachmentBlob stores one blob without replacing existing content.
func UpsertAttachmentBlob(tx *gorm.DB, blob models.AttachmentBlob) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}, {Name: "digest"}},
		DoNothing: true,
	}).Create(&blob).Error
}

// UpsertRevisionAttachment stores one attachment reference for a revision.
func UpsertRevisionAttachment(tx *gorm.DB, attachment models.RevisionAttachment) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}, {Name: "doc_id"}, {Name: "rev"}, {Name: "name"}},
		UpdateAll: true,
	}).Create(&attachment).Error
}

// ReplaceRevisionAttachments atomically replaces all attachment references for a revision.
func ReplaceRevisionAttachments(tx *gorm.DB, uid string, docID string, rev string, attachments []models.RevisionAttachment) error {
	if err := tx.Delete(&models.RevisionAttachment{}, "uid = ? AND doc_id = ? AND rev = ?", uid, docID, rev).Error; err != nil {
		return err
	}
	for _, attachment := range attachments {
		if err := UpsertRevisionAttachment(tx, attachment); err != nil {
			return err
		}
	}
	return nil
}

// CreateChangelog appends one committed revision event.
func CreateChangelog(tx *gorm.DB, logEntry *models.Changelog) error {
	return tx.Create(logEntry).Error
}

// DeleteUnreferencedAttachmentBlobs removes old blobs no revision references.
func DeleteUnreferencedAttachmentBlobs(tx *gorm.DB, uid string, olderThanOrEqual int64) (int64, error) {
	result := tx.Exec(`
DELETE FROM attachment_blobs
WHERE uid = ?
  AND created_at <= ?
  AND NOT EXISTS (
    SELECT 1
    FROM revision_attachments ra
    WHERE ra.uid = attachment_blobs.uid
      AND ra.digest = attachment_blobs.digest
  )`, uid, olderThanOrEqual)
	return result.RowsAffected, result.Error
}

func currentRevisionAttachmentJoin() string {
	return `
JOIN documents d
  ON d.uid = ra.uid
 AND d.doc_id = ra.doc_id
 AND d.rev = ra.rev`
}
