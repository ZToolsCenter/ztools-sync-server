CREATE TABLE IF NOT EXISTS users (
  uid VARCHAR(191) NOT NULL PRIMARY KEY,
  nickname VARCHAR(255) NOT NULL,
  password_hash LONGTEXT NOT NULL,
  avatar_url VARCHAR(512) NOT NULL DEFAULT '',
  created_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  uid VARCHAR(191) NOT NULL,
  token_hash VARCHAR(64) NOT NULL,
  expires_at BIGINT NOT NULL,
  used_at BIGINT NOT NULL DEFAULT 0,
  revoked_at BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  UNIQUE INDEX idx_refresh_tokens_token_hash (token_hash),
  INDEX idx_refresh_tokens_uid (uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS documents (
  uid VARCHAR(191) NOT NULL,
  doc_id VARCHAR(255) NOT NULL,
  doc_json LONGTEXT NOT NULL,
  rev VARCHAR(191) NOT NULL,
  last_modified BIGINT NOT NULL,
  deleted BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY (uid, doc_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS document_revisions (
  uid VARCHAR(191) NOT NULL,
  doc_id VARCHAR(255) NOT NULL,
  rev VARCHAR(191) NOT NULL,
  parent_rev VARCHAR(191),
  doc_json LONGTEXT NOT NULL,
  deleted BOOLEAN NOT NULL DEFAULT FALSE,
  last_modified BIGINT NOT NULL,
  created_at BIGINT NOT NULL,
  device_id VARCHAR(191),
  generation BIGINT NOT NULL DEFAULT 0,
  is_leaf BOOLEAN NOT NULL DEFAULT TRUE,
  is_winner BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY (uid, doc_id, rev),
  INDEX idx_document_revisions_uid_doc (uid, doc_id),
  INDEX idx_document_revisions_uid_doc_leaf (uid, doc_id, is_leaf)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS document_write_locks (
  uid VARCHAR(191) NOT NULL,
  doc_id VARCHAR(255) NOT NULL,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (uid, doc_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS changelog (
  seq BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  uid VARCHAR(191) NOT NULL,
  doc_id VARCHAR(255) NOT NULL,
  rev VARCHAR(191) NOT NULL,
  deleted BOOLEAN NOT NULL DEFAULT FALSE,
  timestamp BIGINT NOT NULL,
  device_id VARCHAR(191),
  doc_type VARCHAR(32) DEFAULT 'doc',
  attachment_meta LONGTEXT,
  parent_rev VARCHAR(191),
  winner_rev VARCHAR(191),
  protocol_version BIGINT DEFAULT 1,
  resolution LONGTEXT,
  doc_json_bytes BIGINT NOT NULL DEFAULT 0,
  INDEX idx_changelog_uid_seq (uid, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS device_sync_state (
  uid VARCHAR(191) NOT NULL,
  device_id VARCHAR(191) NOT NULL,
  last_seq BIGINT NOT NULL DEFAULT 0,
  last_seen BIGINT,
  device_name VARCHAR(255),
  PRIMARY KEY (uid, device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS attachment_blobs (
  uid VARCHAR(191) NOT NULL,
  digest VARCHAR(191) NOT NULL,
  content_type VARCHAR(191) NOT NULL,
  length BIGINT NOT NULL,
  data LONGBLOB NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (uid, digest)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS revision_attachments (
  uid VARCHAR(191) NOT NULL,
  doc_id VARCHAR(255) NOT NULL,
  rev VARCHAR(191) NOT NULL,
  name VARCHAR(120) NOT NULL,
  digest VARCHAR(191) NOT NULL,
  content_type VARCHAR(191) NOT NULL,
  length BIGINT NOT NULL,
  revpos BIGINT NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (uid, doc_id, rev, name),
  INDEX idx_revision_attachments_uid_digest (uid, digest),
  INDEX idx_revision_attachments_uid_doc (uid, doc_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS sync_meta (
  `key` VARCHAR(191) NOT NULL PRIMARY KEY,
  value LONGTEXT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS sync_checkpoints (
  uid VARCHAR(191) NOT NULL,
  checkpoint_id VARCHAR(191) NOT NULL,
  source_id VARCHAR(191) NOT NULL,
  target_id VARCHAR(191) NOT NULL,
  last_seq BIGINT NOT NULL DEFAULT 0,
  checkpoint_json LONGTEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (uid, checkpoint_id),
  INDEX idx_sync_checkpoints_uid_updated (uid, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
