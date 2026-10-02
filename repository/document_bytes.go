package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const documentBytesBackfillKey = "document_json_bytes_backfill"

type documentBytesCursor struct {
	UID      string `json:"uid"`
	DocID    string `json:"docId"`
	Complete bool   `json:"complete"`
}

// BackfillDocumentJSONBytes fills a bounded primary-key range and commits its cursor atomically.
// Existing values are never overwritten, including values maintained by concurrent sync writes.
func (r *Repository) BackfillDocumentJSONBytes(ctx context.Context, batchSize int) (complete bool, err error) {
	if batchSize < 1 || batchSize > 1000 {
		return false, fmt.Errorf("document byte backfill batch size must be between 1 and 1000")
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var meta models.SyncMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&meta, "`key` = ?", documentBytesBackfillKey).Error; err != nil {
			return err
		}
		var cursor documentBytesCursor
		if err := json.Unmarshal([]byte(meta.Value), &cursor); err != nil {
			return fmt.Errorf("decode document byte backfill cursor: %w", err)
		}
		if cursor.Complete {
			complete = true
			return nil
		}

		// Read keys only. The lexicographic range avoids OFFSET and repeated scans of repaired rows.
		query := tx.Model(&models.Document{}).Select("uid", "doc_id")
		if cursor.UID != "" || cursor.DocID != "" {
			query = query.Where("uid > ? OR (uid = ? AND doc_id > ?)", cursor.UID, cursor.UID, cursor.DocID)
		}
		var keys []models.Document
		if err := query.Order("uid ASC, doc_id ASC").Limit(batchSize).Find(&keys).Error; err != nil {
			return err
		}
		if len(keys) == 0 {
			cursor.Complete = true
		} else {
			last := keys[len(keys)-1]
			update := tx.Table("documents").Where("doc_json_bytes IS NULL").
				Where("uid < ? OR (uid = ? AND doc_id <= ?)", last.UID, last.UID, last.DocID)
			if cursor.UID != "" || cursor.DocID != "" {
				update = update.Where("uid > ? OR (uid = ? AND doc_id > ?)", cursor.UID, cursor.UID, cursor.DocID)
			}
			expression := "OCTET_LENGTH(doc_json)"
			if tx.Dialector.Name() == "sqlite" {
				expression = "LENGTH(CAST(doc_json AS BLOB))"
			}
			// Calculate against the locked current row, so a concurrent winner update cannot go stale.
			if err := update.UpdateColumn("doc_json_bytes", gorm.Expr(expression)).Error; err != nil {
				return err
			}
			cursor.UID, cursor.DocID = last.UID, last.DocID
		}
		value, err := json.Marshal(cursor)
		if err != nil {
			return err
		}
		if err := tx.Model(&models.SyncMeta{}).Where("`key` = ?", documentBytesBackfillKey).
			UpdateColumn("value", string(value)).Error; err != nil {
			return err
		}
		complete = cursor.Complete
		return nil
	})
	return complete, err
}
