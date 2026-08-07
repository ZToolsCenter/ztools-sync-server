package transport

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

type AttachmentHandler struct {
	auth *auth.Service
	sync *syncsvc.Service
}

func NewAttachmentHandler(authService *auth.Service, syncService *syncsvc.Service) *AttachmentHandler {
	return &AttachmentHandler{auth: authService, sync: syncService}
}

func (h *AttachmentHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Query().Get("action") == "compact" {
		h.Compact(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	uid, ok := bearerUID(h.auth, r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	rows, err := h.sync.ListAttachments(uid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]interface{}{
			"uid":          row.UID,
			"doc_id":       row.DocID,
			"rev":          row.Rev,
			"name":         row.Name,
			"digest":       row.Digest,
			"md5":          strings.TrimPrefix(row.Digest, "md5-"),
			"content_type": row.ContentType,
			"length":       row.Length,
			"size":         row.Length,
			"revpos":       row.Revpos,
			"created_at":   row.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AttachmentHandler) Item(w http.ResponseWriter, r *http.Request) {
	uid, ok := bearerUID(h.auth, r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	docID := strings.TrimPrefix(r.URL.Path, "/api/sync/attachments/")
	if strings.HasPrefix(docID, "blobs/") {
		h.Blob(w, r, uid, strings.TrimPrefix(docID, "blobs/"))
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
}

func (h *AttachmentHandler) Blob(w http.ResponseWriter, r *http.Request, uid string, digest string) {
	if digest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "digest required"})
		return
	}
	if strings.HasSuffix(digest, "/meta") {
		h.BlobMeta(w, r, uid, strings.TrimSuffix(digest, "/meta"))
		return
	}
	switch r.Method {
	case http.MethodPut:
		data, err := io.ReadAll(io.LimitReader(r.Body, syncsvc.MaxAttachmentSize+1))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if len(data) > syncsvc.MaxAttachmentSize {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "Attachment exceeds 10M"})
			return
		}
		contentType := r.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		blob, err := h.sync.PutAttachmentBlob(uid, digest, contentType, data)
		if errors.Is(err, syncsvc.ErrAttachmentDigestMismatch) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "digest": blob.Digest, "length": blob.Length})
	case http.MethodGet:
		blob, err := h.sync.GetAttachmentBlob(uid, digest)
		if err == gorm.ErrRecordNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Attachment blob not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", blob.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(blob.Data)))
		w.Header().Set("X-Attachment-Digest", blob.Digest)
		w.Header().Set("X-Attachment-Md5", strings.TrimPrefix(blob.Digest, "md5-"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(blob.Data)
	case http.MethodHead:
		blob, err := h.sync.GetAttachmentBlob(uid, digest)
		if err == gorm.ErrRecordNotFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", blob.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(blob.Length, 10))
		w.Header().Set("X-Attachment-Digest", blob.Digest)
		w.WriteHeader(http.StatusOK)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
	}
}

func (h *AttachmentHandler) BlobMeta(w http.ResponseWriter, r *http.Request, uid string, digest string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	blob, err := h.sync.GetAttachmentBlob(uid, digest)
	if err == gorm.ErrRecordNotFound {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Attachment blob not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"digest":       blob.Digest,
		"content_type": blob.ContentType,
		"length":       blob.Length,
	})
}

func (h *AttachmentHandler) Compact(w http.ResponseWriter, r *http.Request) {
	uid, ok := bearerUID(h.auth, r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return
	}
	graceMs := int64(24 * 60 * 60 * 1000)
	if raw := r.URL.Query().Get("graceMs"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid graceMs"})
			return
		}
		graceMs = parsed
	}
	deleted, err := h.sync.CompactAttachmentBlobs(uid, graceMs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "deleted": deleted})
}
