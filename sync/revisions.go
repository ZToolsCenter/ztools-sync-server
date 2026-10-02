package sync

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/store"
)

func compareRevs(a string, b string) int {
	ga := store.ParseGeneration(a)
	gb := store.ParseGeneration(b)
	if ga != gb {
		return ga - gb
	}
	return strings.Compare(a, b)
}

func chooseWinner(leaves []models.DocumentRevision) *models.DocumentRevision {
	if len(leaves) == 0 {
		return nil
	}
	pool := make([]models.DocumentRevision, 0, len(leaves))
	for _, leaf := range leaves {
		if !leaf.Deleted {
			pool = append(pool, leaf)
		}
	}
	if len(pool) == 0 {
		pool = append(pool, leaves...)
	}
	sort.Slice(pool, func(i int, j int) bool {
		return compareRevs(pool[i].Rev, pool[j].Rev) > 0
	})
	return &pool[0]
}

func normalizeDocJSON(raw json.RawMessage, deleted bool) string {
	if deleted || len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	return string(raw)
}

func docRawFromRevision(rev models.DocumentRevision) json.RawMessage {
	if rev.Deleted {
		return nil
	}
	if rev.DocJSON == "" {
		return nil
	}
	return json.RawMessage(rev.DocJSON)
}

func docRawFromRevisionWithAttachments(revision models.DocumentRevision, attachments []models.RevisionAttachment) json.RawMessage {
	if revision.Deleted {
		return nil
	}
	docJSON := revision.DocJSON
	if docJSON == "" {
		return nil
	}
	if len(attachments) > 0 {
		docJSON = injectAttachmentStubs(docJSON, attachments)
	}
	return json.RawMessage(docJSON)
}

func buildDocChangeFromRevision(revision models.DocumentRevision, seq int64, winnerRev string, resolution *Resolution) FullChangeEntry {
	return buildDocChangeFromRevisionWithAttachments(revision, nil, seq, winnerRev, resolution)
}

func buildDocChangeFromRevisionWithAttachments(revision models.DocumentRevision, attachments []models.RevisionAttachment, seq int64, winnerRev string, resolution *Resolution) FullChangeEntry {
	return buildDocChangeFromRevisionWithAttachmentsAndHistory(revision, attachments, nil, seq, winnerRev, resolution)
}

func buildDocChangeFromRevisionWithAttachmentsAndHistory(revision models.DocumentRevision, attachments []models.RevisionAttachment, history []string, seq int64, winnerRev string, resolution *Resolution) FullChangeEntry {
	var parent *string
	if revision.ParentRev != "" {
		parent = &revision.ParentRev
	}
	if winnerRev == "" {
		winnerRev = revision.Rev
	}
	return FullChangeEntry{
		Seq:        seq,
		DocID:      revision.DocID,
		Rev:        revision.Rev,
		ParentRev:  parent,
		Deleted:    revision.Deleted,
		Timestamp:  revision.LastModified,
		Doc:        docRawFromRevisionWithAttachments(revision, attachments),
		DocType:    "doc",
		WinnerRev:  winnerRev,
		IsWinner:   revision.Rev == winnerRev,
		Resolution: resolution,
		RevisionHistory: normalizeRevisionHistory(
			revision.Rev,
			history,
		),
	}
}

func revisionHistoryFromRevisions(currentRev string, revisions []models.DocumentRevision) []string {
	byRev := map[string]models.DocumentRevision{}
	for _, revision := range revisions {
		byRev[revision.Rev] = revision
	}
	history := []string{}
	seen := map[string]bool{}
	rev := currentRev
	for rev != "" && !seen[rev] {
		history = append(history, rev)
		seen[rev] = true
		revision, ok := byRev[rev]
		if !ok {
			break
		}
		rev = revision.ParentRev
	}
	return history
}

/**
 * updateDocumentProjection 重新选举文档 winner，并仅按主键切换旧、新 winner 标记。
 * @param tx 当前数据库事务。
 * @param uid 文档所属用户标识。
 * @param docID 文档标识。
 * @returns 当前 winner、参与选举的叶子 revision 以及可能发生的错误。
 */
func updateDocumentProjection(tx *gorm.DB, uid string, docID string) (*models.DocumentRevision, []models.DocumentRevision, error) {
	// 选举阶段只读取元数据，避免把所有叶子的长文本载荷载入内存。
	var leaves []models.DocumentRevision
	if err := tx.Select("uid", "doc_id", "rev", "parent_rev", "deleted", "last_modified", "created_at", "device_id", "generation", "is_leaf", "is_winner").
		Where("uid = ? AND doc_id = ? AND is_leaf = ?", uid, docID, true).
		Order("generation DESC, rev DESC").
		Find(&leaves).Error; err != nil {
		return nil, nil, err
	}
	winner := chooseWinner(leaves)
	if winner == nil {
		return nil, leaves, nil
	}

	// documents 投影记录提供旧 winner 主键，避免按布尔字段扫描全部历史 revision。
	var projection models.Document
	err := tx.Select("uid", "doc_id", "rev").First(&projection, "uid = ? AND doc_id = ?", uid, docID).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, nil, err
	}
	oldWinnerRev := projection.Rev
	if oldWinnerRev == winner.Rev {
		// 兼容历史异常数据：投影已指向 winner 时仍按主键补齐布尔标记。
		if !winner.IsWinner {
			if err := tx.Model(&models.DocumentRevision{}).
				Where("uid = ? AND doc_id = ? AND rev = ? AND is_winner = ?", uid, docID, winner.Rev, false).
				Update("is_winner", true).Error; err != nil {
				return nil, nil, err
			}
			winner.IsWinner = true
		}
		return winner, leaves, nil
	}

	// winner 变化时只更新旧、新两条主键记录，锁定范围保持为常数级。
	if oldWinnerRev != "" {
		if err := tx.Model(&models.DocumentRevision{}).
			Where("uid = ? AND doc_id = ? AND rev = ? AND is_winner = ?", uid, docID, oldWinnerRev, true).
			Update("is_winner", false).Error; err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Model(&models.DocumentRevision{}).
		Where("uid = ? AND doc_id = ? AND rev = ? AND is_winner = ?", uid, docID, winner.Rev, false).
		Update("is_winner", true).Error; err != nil {
		return nil, nil, err
	}

	// 只有新 winner 需要读取完整载荷并刷新当前文档投影。
	var winnerRevision models.DocumentRevision
	if err := tx.First(&winnerRevision, "uid = ? AND doc_id = ? AND rev = ?", uid, docID, winner.Rev).Error; err != nil {
		return nil, nil, err
	}
	docJSON := winnerRevision.DocJSON
	if docJSON == "" {
		docJSON = "{}"
	}
	docJSONBytes := int64(len(docJSON))
	doc := models.Document{
		UID:          uid,
		DocID:        docID,
		DocJSON:      docJSON,
		DocJSONBytes: &docJSONBytes,
		Rev:          winnerRevision.Rev,
		LastModified: winnerRevision.LastModified,
		Deleted:      winnerRevision.Deleted,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}, {Name: "doc_id"}},
		UpdateAll: true,
	}).Create(&doc).Error; err != nil {
		return nil, nil, err
	}
	return &winnerRevision, leaves, nil
}

func retireOtherLeaves(tx *gorm.DB, uid string, docID string, keepRev string) error {
	return tx.Model(&models.DocumentRevision{}).
		Where("uid = ? AND doc_id = ? AND is_leaf = ? AND rev <> ?", uid, docID, true, keepRev).
		Updates(map[string]interface{}{"is_leaf": false, "is_winner": false}).Error
}

type insertRevisionResult struct {
	Inserted bool
	LastSeq  int64
	Winner   *models.DocumentRevision
	Revision *models.DocumentRevision
}

func normalizeRevisionHistory(currentRev string, raw []string) []string {
	history := make([]string, 0, len(raw)+1)
	seen := map[string]bool{}
	add := func(rev string) {
		rev = strings.TrimSpace(rev)
		if rev == "" || seen[rev] {
			return
		}
		history = append(history, rev)
		seen[rev] = true
	}

	add(currentRev)
	for _, rev := range raw {
		add(rev)
	}
	return history
}

func inferredParentRev(change FullChangeEntry, history []string) string {
	if change.ParentRev != nil && strings.TrimSpace(*change.ParentRev) != "" {
		return strings.TrimSpace(*change.ParentRev)
	}
	if len(history) > 1 {
		return history[1]
	}
	return ""
}

func markParentNonLeaf(tx *gorm.DB, uid string, docID string, parentRev string) error {
	return tx.Model(&models.DocumentRevision{}).
		Where("uid = ? AND doc_id = ? AND rev = ?", uid, docID, parentRev).
		Updates(map[string]interface{}{"is_leaf": false, "is_winner": false}).Error
}

func ensureRevisionHistoryStubs(tx *gorm.DB, uid string, docID string, deviceID string, history []string, timestamp int64) error {
	if len(history) <= 1 {
		return nil
	}
	now := time.Now().UnixMilli()
	if timestamp > 0 {
		now = timestamp
	}

	for i := 1; i < len(history); i++ {
		rev := history[i]
		parentRev := ""
		if i+1 < len(history) {
			parentRev = history[i+1]
		}

		var existing models.DocumentRevision
		err := tx.First(&existing, "uid = ? AND doc_id = ? AND rev = ?", uid, docID, rev).Error
		if err == nil {
			updates := map[string]interface{}{"is_leaf": false, "is_winner": false}
			if existing.ParentRev == "" && parentRev != "" {
				updates["parent_rev"] = parentRev
			}
			if err := tx.Model(&models.DocumentRevision{}).
				Where("uid = ? AND doc_id = ? AND rev = ?", uid, docID, rev).
				Updates(updates).Error; err != nil {
				return err
			}
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}

		stub := models.DocumentRevision{
			UID:          uid,
			DocID:        docID,
			Rev:          rev,
			ParentRev:    parentRev,
			DocJSON:      "{}",
			Deleted:      false,
			LastModified: now,
			CreatedAt:    now,
			DeviceID:     deviceID,
			Generation:   store.ParseGeneration(rev),
			IsLeaf:       false,
			IsWinner:     false,
		}
		if err := tx.Create(&stub).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.DocumentRevision{}).
			Where("uid = ? AND doc_id = ? AND rev = ?", uid, docID, rev).
			Updates(map[string]interface{}{"is_leaf": false, "is_winner": false}).Error; err != nil {
			return err
		}
	}
	return nil
}

func insertRevisionChange(tx *gorm.DB, uid string, deviceID string, change FullChangeEntry, protocolVersion int) (*insertRevisionResult, error) {
	if change.Rev == "" {
		return &insertRevisionResult{Inserted: false}, nil
	}
	revisionHistory := normalizeRevisionHistory(change.Rev, change.RevisionHistory)
	parentRev := inferredParentRev(change, revisionHistory)

	var existing models.DocumentRevision
	err := tx.First(&existing, "uid = ? AND doc_id = ? AND rev = ?", uid, change.DocID, change.Rev).Error
	if err == nil {
		if err := ensureRevisionHistoryStubs(tx, uid, change.DocID, deviceID, revisionHistory, change.Timestamp); err != nil {
			return nil, err
		}
		if parentRev != "" && existing.ParentRev == "" {
			if err := tx.Model(&models.DocumentRevision{}).
				Where("uid = ? AND doc_id = ? AND rev = ?", uid, change.DocID, change.Rev).
				Update("parent_rev", parentRev).Error; err != nil {
				return nil, err
			}
			existing.ParentRev = parentRev
		}
		if parentRev != "" {
			if err := markParentNonLeaf(tx, uid, change.DocID, parentRev); err != nil {
				return nil, err
			}
		}
		winner, _, err := updateDocumentProjection(tx, uid, change.DocID)
		return &insertRevisionResult{Inserted: false, Winner: winner, Revision: &existing}, err
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	now := time.Now().UnixMilli()
	if change.Timestamp > 0 {
		now = change.Timestamp
	}
	if err := ensureRevisionHistoryStubs(tx, uid, change.DocID, deviceID, revisionHistory, now); err != nil {
		return nil, err
	}
	revision := models.DocumentRevision{
		UID:          uid,
		DocID:        change.DocID,
		Rev:          change.Rev,
		ParentRev:    parentRev,
		DocJSON:      normalizeDocJSON(change.Doc, change.Deleted),
		Deleted:      change.Deleted,
		LastModified: now,
		CreatedAt:    now,
		DeviceID:     deviceID,
		Generation:   store.ParseGeneration(change.Rev),
		IsLeaf:       true,
		IsWinner:     false,
	}
	if err := tx.Create(&revision).Error; err != nil {
		return nil, err
	}
	if err := attachRevisionMetadata(tx, uid, change.DocID, change.Rev, change.Doc); err != nil {
		return nil, err
	}
	if parentRev != "" {
		if err := markParentNonLeaf(tx, uid, change.DocID, parentRev); err != nil {
			return nil, err
		}
	}
	if change.Resolution != nil && change.Resolution.RetireOtherLeaves {
		if err := retireOtherLeaves(tx, uid, change.DocID, change.Rev); err != nil {
			return nil, err
		}
	}
	winner, _, err := updateDocumentProjection(tx, uid, change.DocID)
	if err != nil {
		return nil, err
	}
	resolutionJSON := ""
	if change.Resolution != nil {
		raw, _ := json.Marshal(change.Resolution)
		resolutionJSON = string(raw)
	}
	winnerRev := change.Rev
	if winner != nil {
		winnerRev = winner.Rev
	}
	logEntry := models.Changelog{
		UID:             uid,
		DocID:           change.DocID,
		Rev:             change.Rev,
		Deleted:         change.Deleted,
		Timestamp:       now,
		DeviceID:        deviceID,
		DocType:         "doc",
		ParentRev:       parentRev,
		WinnerRev:       winnerRev,
		ProtocolVersion: protocolVersion,
		Resolution:      resolutionJSON,
		DocJSONBytes:    int64(len(revision.DocJSON)),
	}
	if err := tx.Create(&logEntry).Error; err != nil {
		return nil, err
	}
	return &insertRevisionResult{Inserted: true, LastSeq: logEntry.Seq, Winner: winner, Revision: &revision}, nil
}

func parseResolution(raw string) *Resolution {
	if raw == "" {
		return nil
	}
	var resolution Resolution
	if err := json.Unmarshal([]byte(raw), &resolution); err != nil {
		return nil
	}
	return &resolution
}
