package sync

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
)

const defaultAttachmentName = "default"

type CouchAttachmentMeta struct {
	Stub        bool   `json:"stub,omitempty"`
	Digest      string `json:"digest,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Length      int64  `json:"length,omitempty"`
	Revpos      int    `json:"revpos,omitempty"`
}

func normalizeDigest(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "md5-") {
		return value
	}
	return "md5-" + value
}

func trimMD5Prefix(value string) string {
	return strings.TrimPrefix(value, "md5-")
}

func attachmentDigest(data []byte) string {
	sum := md5.Sum(data)
	return "md5-" + hex.EncodeToString(sum[:])
}

func parseDocAttachments(raw json.RawMessage, fallbackRev string) ([]models.RevisionAttachment, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	rawAttachments, ok := doc["_attachments"]
	if !ok || len(rawAttachments) == 0 || string(rawAttachments) == "null" {
		return nil, nil
	}
	attachments := map[string]CouchAttachmentMeta{}
	if err := json.Unmarshal(rawAttachments, &attachments); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	revpos := store.ParseGeneration(fallbackRev)
	rows := make([]models.RevisionAttachment, 0, len(attachments))
	for name, meta := range attachments {
		digest := normalizeDigest(meta.Digest)
		if digest == "" {
			continue
		}
		if meta.Revpos > 0 {
			revpos = meta.Revpos
		}
		rows = append(rows, models.RevisionAttachment{
			Name:        name,
			Digest:      digest,
			ContentType: meta.ContentType,
			Length:      meta.Length,
			Revpos:      revpos,
			CreatedAt:   now,
		})
	}
	return rows, nil
}

func injectAttachmentStubs(raw string, attachments []models.RevisionAttachment) string {
	if len(attachments) == 0 {
		return raw
	}
	doc := map[string]interface{}{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &doc)
	}
	attachmentMap := map[string]CouchAttachmentMeta{}
	for _, item := range attachments {
		attachmentMap[item.Name] = CouchAttachmentMeta{
			Stub:        true,
			Digest:      item.Digest,
			ContentType: item.ContentType,
			Length:      item.Length,
			Revpos:      item.Revpos,
		}
	}
	doc["_attachments"] = attachmentMap
	next, err := json.Marshal(doc)
	if err != nil {
		return raw
	}
	return string(next)
}

func attachRevisionMetadata(tx *gorm.DB, uid string, docID string, rev string, raw json.RawMessage) error {
	attachments, err := parseDocAttachments(raw, rev)
	if err != nil {
		return err
	}
	for i := range attachments {
		attachments[i].UID = uid
		attachments[i].DocID = docID
		attachments[i].Rev = rev
	}
	return repository.ReplaceRevisionAttachments(tx, uid, docID, rev, attachments)
}
