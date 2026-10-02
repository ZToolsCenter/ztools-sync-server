ALTER TABLE documents ADD COLUMN doc_json_bytes BIGINT NULL, ALGORITHM=INSTANT;

ALTER TABLE documents ADD INDEX idx_documents_deleted_bytes (deleted, doc_json_bytes), ALGORITHM=INPLACE, LOCK=NONE;

INSERT INTO sync_meta (`key`, value) VALUES ('document_json_bytes_backfill', '{}');
