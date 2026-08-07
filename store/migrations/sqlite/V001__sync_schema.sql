CREATE TABLE IF NOT EXISTS users (
  uid TEXT NOT NULL PRIMARY KEY,
  nickname TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  avatar_url TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER NOT NULL DEFAULT 0,
  revoked_at INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON refresh_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_uid ON refresh_tokens (uid);

CREATE TABLE IF NOT EXISTS documents (
  uid TEXT NOT NULL,
  doc_id TEXT NOT NULL,
  doc_json TEXT NOT NULL,
  rev TEXT NOT NULL,
  last_modified INTEGER NOT NULL,
  deleted INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (uid, doc_id)
);

CREATE TABLE IF NOT EXISTS document_revisions (
  uid TEXT NOT NULL,
  doc_id TEXT NOT NULL,
  rev TEXT NOT NULL,
  parent_rev TEXT,
  doc_json TEXT NOT NULL,
  deleted INTEGER NOT NULL DEFAULT 0,
  last_modified INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  device_id TEXT,
  generation INTEGER NOT NULL DEFAULT 0,
  is_leaf INTEGER NOT NULL DEFAULT 1,
  is_winner INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (uid, doc_id, rev)
);

CREATE INDEX IF NOT EXISTS idx_document_revisions_uid_doc ON document_revisions (uid, doc_id);
CREATE INDEX IF NOT EXISTS idx_document_revisions_uid_doc_leaf ON document_revisions (uid, doc_id, is_leaf);

CREATE TABLE IF NOT EXISTS document_write_locks (
  uid TEXT NOT NULL,
  doc_id TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (uid, doc_id)
);

CREATE TABLE IF NOT EXISTS changelog (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  uid TEXT NOT NULL,
  doc_id TEXT NOT NULL,
  rev TEXT NOT NULL,
  deleted INTEGER NOT NULL DEFAULT 0,
  timestamp INTEGER NOT NULL,
  device_id TEXT,
  doc_type TEXT DEFAULT 'doc',
  attachment_meta TEXT,
  parent_rev TEXT,
  winner_rev TEXT,
  protocol_version INTEGER DEFAULT 1,
  resolution TEXT,
  doc_json_bytes INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_changelog_uid_seq ON changelog (uid, seq);

CREATE TABLE IF NOT EXISTS device_sync_state (
  uid TEXT NOT NULL,
  device_id TEXT NOT NULL,
  last_seq INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER,
  device_name TEXT,
  PRIMARY KEY (uid, device_id)
);

CREATE TABLE IF NOT EXISTS attachment_blobs (
  uid TEXT NOT NULL,
  digest TEXT NOT NULL,
  content_type TEXT NOT NULL,
  length INTEGER NOT NULL,
  data BLOB NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (uid, digest)
);

CREATE TABLE IF NOT EXISTS revision_attachments (
  uid TEXT NOT NULL,
  doc_id TEXT NOT NULL,
  rev TEXT NOT NULL,
  name TEXT NOT NULL,
  digest TEXT NOT NULL,
  content_type TEXT NOT NULL,
  length INTEGER NOT NULL,
  revpos INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (uid, doc_id, rev, name)
);

CREATE INDEX IF NOT EXISTS idx_revision_attachments_uid_digest ON revision_attachments (uid, digest);
CREATE INDEX IF NOT EXISTS idx_revision_attachments_uid_doc ON revision_attachments (uid, doc_id);

CREATE TABLE IF NOT EXISTS sync_meta (
  key TEXT NOT NULL PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_checkpoints (
  uid TEXT NOT NULL,
  checkpoint_id TEXT NOT NULL,
  source_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  last_seq INTEGER NOT NULL DEFAULT 0,
  checkpoint_json TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (uid, checkpoint_id)
);

CREATE INDEX IF NOT EXISTS idx_sync_checkpoints_uid_updated ON sync_checkpoints (uid, updated_at);
