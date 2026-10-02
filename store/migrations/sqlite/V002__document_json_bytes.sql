ALTER TABLE documents ADD COLUMN doc_json_bytes INTEGER;

CREATE INDEX idx_documents_deleted_bytes ON documents (deleted, doc_json_bytes);

INSERT INTO sync_meta (`key`, value) VALUES ('document_json_bytes_backfill', '{}');
