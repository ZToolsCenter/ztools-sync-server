package repository

import "github.com/ZToolsCenter/ztools-sync-server/models"

// DocumentRevisionParents loads only the links needed to reconstruct revision histories.
// Historical document bodies are deliberately excluded, including for stub revisions.
func (r *Repository) DocumentRevisionParents(uid string, docID string) (map[string]string, error) {
	var rows []struct {
		Rev       string
		ParentRev string
	}
	if err := r.db.Model(&models.DocumentRevision{}).
		Select("rev", "parent_rev").
		Where("uid = ? AND doc_id = ?", uid, docID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	parents := make(map[string]string, len(rows))
	for _, row := range rows {
		parents[row.Rev] = row.ParentRev
	}
	return parents, nil
}
