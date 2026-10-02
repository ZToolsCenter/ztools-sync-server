package models

type User struct {
	UID          string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	Nickname     string `gorm:"column:nickname;type:varchar(255);not null" json:"nickname"`
	PasswordHash string `gorm:"column:password_hash" json:"-"`
	AvatarURL    string `gorm:"column:avatar_url;type:varchar(512);not null;default:''" json:"avatarUrl"`
	CreatedAt    int64  `gorm:"column:created_at;not null" json:"created_at"`
}

func (User) TableName() string { return "users" }

type RefreshToken struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UID       string `gorm:"column:uid;type:varchar(191);not null" json:"uid"`
	TokenHash string `gorm:"column:token_hash;type:varchar(64);not null" json:"-"`
	ExpiresAt int64  `gorm:"column:expires_at;not null" json:"expiresAt"`
	UsedAt    int64  `gorm:"column:used_at;not null;default:0" json:"usedAt"`
	RevokedAt int64  `gorm:"column:revoked_at;not null;default:0" json:"revokedAt"`
	CreatedAt int64  `gorm:"column:created_at;not null" json:"createdAt"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }

type Document struct {
	UID          string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	DocID        string `gorm:"column:doc_id;type:varchar(255);primaryKey" json:"doc_id"`
	DocJSON      string `gorm:"column:doc_json;not null" json:"doc_json"`
	DocJSONBytes *int64 `gorm:"column:doc_json_bytes" json:"-"`
	Rev          string `gorm:"column:rev;type:varchar(191);not null" json:"rev"`
	LastModified int64  `gorm:"column:last_modified;not null" json:"last_modified"`
	Deleted      bool   `gorm:"column:deleted;default:0" json:"deleted"`
}

func (Document) TableName() string { return "documents" }

type DocumentRevision struct {
	UID          string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	DocID        string `gorm:"column:doc_id;type:varchar(255);primaryKey" json:"doc_id"`
	Rev          string `gorm:"column:rev;type:varchar(191);primaryKey" json:"rev"`
	ParentRev    string `gorm:"column:parent_rev;type:varchar(191)" json:"parent_rev"`
	DocJSON      string `gorm:"column:doc_json;not null" json:"doc_json"`
	Deleted      bool   `gorm:"column:deleted;not null;default:0" json:"deleted"`
	LastModified int64  `gorm:"column:last_modified;not null" json:"last_modified"`
	CreatedAt    int64  `gorm:"column:created_at;not null" json:"created_at"`
	DeviceID     string `gorm:"column:device_id;type:varchar(191)" json:"device_id"`
	Generation   int    `gorm:"column:generation;not null;default:0" json:"generation"`
	IsLeaf       bool   `gorm:"column:is_leaf;not null;default:1" json:"is_leaf"`
	IsWinner     bool   `gorm:"column:is_winner;not null;default:0" json:"is_winner"`
}

func (DocumentRevision) TableName() string { return "document_revisions" }

type DocumentWriteLock struct {
	UID       string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	DocID     string `gorm:"column:doc_id;type:varchar(255);primaryKey" json:"doc_id"`
	UpdatedAt int64  `gorm:"column:updated_at;not null" json:"updated_at"`
}

/**
 * TableName 返回文档写锁使用的数据库表名。
 * @returns 文档写锁表名。
 */
func (DocumentWriteLock) TableName() string { return "document_write_locks" }

type Changelog struct {
	Seq             int64  `gorm:"column:seq;primaryKey;autoIncrement" json:"seq"`
	UID             string `gorm:"column:uid;type:varchar(191);not null" json:"uid"`
	DocID           string `gorm:"column:doc_id;type:varchar(255);not null" json:"doc_id"`
	Rev             string `gorm:"column:rev;type:varchar(191);not null" json:"rev"`
	Deleted         bool   `gorm:"column:deleted;default:0" json:"deleted"`
	Timestamp       int64  `gorm:"column:timestamp;not null" json:"timestamp"`
	DeviceID        string `gorm:"column:device_id;type:varchar(191)" json:"device_id"`
	DocType         string `gorm:"column:doc_type;type:varchar(32);default:doc" json:"doc_type"`
	AttachmentMeta  string `gorm:"column:attachment_meta" json:"attachment_meta"`
	ParentRev       string `gorm:"column:parent_rev;type:varchar(191)" json:"parent_rev"`
	WinnerRev       string `gorm:"column:winner_rev;type:varchar(191)" json:"winner_rev"`
	ProtocolVersion int    `gorm:"column:protocol_version;default:1" json:"protocol_version"`
	Resolution      string `gorm:"column:resolution" json:"resolution"`
	DocJSONBytes    int64  `gorm:"column:doc_json_bytes;not null;default:0" json:"doc_json_bytes"`
}

func (Changelog) TableName() string { return "changelog" }

type DeviceSyncState struct {
	UID        string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	DeviceID   string `gorm:"column:device_id;type:varchar(191);primaryKey" json:"device_id"`
	LastSeq    int64  `gorm:"column:last_seq;default:0" json:"last_seq"`
	LastSeen   int64  `gorm:"column:last_seen" json:"last_seen"`
	DeviceName string `gorm:"column:device_name;type:varchar(255)" json:"device_name"`
}

func (DeviceSyncState) TableName() string { return "device_sync_state" }

type DailyActivity struct {
	ActivityDate  string `gorm:"column:activity_date;type:date;primaryKey" json:"activityDate"`
	ActivityType  string `gorm:"column:activity_type;type:varchar(32);primaryKey" json:"activityType"`
	SubjectID     string `gorm:"column:subject_id;type:varchar(191);primaryKey" json:"subjectId"`
	UID           string `gorm:"column:uid;type:varchar(191);not null;default:''" json:"uid"`
	DeviceID      string `gorm:"column:device_id;type:varchar(191);not null;default:''" json:"deviceId"`
	IP            string `gorm:"column:ip;type:varchar(64);not null;default:''" json:"ip"`
	UserAgent     string `gorm:"column:user_agent;type:varchar(512);not null;default:''" json:"userAgent"`
	ZToolsVersion string `gorm:"column:ztools_version;type:varchar(64);not null;default:''" json:"ztoolsVersion"`
	LastSeenAt    int64  `gorm:"column:last_seen_at;not null" json:"lastSeenAt"`
	CreatedAt     int64  `gorm:"column:created_at;not null" json:"createdAt"`
}

func (DailyActivity) TableName() string { return "daily_activity" }

type DailyActivityStats struct {
	ActivityDate      string `gorm:"column:activity_date;type:date;primaryKey" json:"activityDate"`
	DeviceActiveCount int64  `gorm:"column:device_active_count;not null;default:0" json:"deviceActive"`
	UserActiveCount   int64  `gorm:"column:user_active_count;not null;default:0" json:"userActive"`
	CalculatedAt      int64  `gorm:"column:calculated_at;not null" json:"calculatedAt"`
}

func (DailyActivityStats) TableName() string { return "daily_activity_stats" }

type AttachmentBlob struct {
	UID         string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	Digest      string `gorm:"column:digest;type:varchar(191);primaryKey" json:"digest"`
	ContentType string `gorm:"column:content_type;type:varchar(191);not null" json:"content_type"`
	Length      int64  `gorm:"column:length;not null" json:"length"`
	Data        []byte `gorm:"column:data;not null" json:"-"`
	CreatedAt   int64  `gorm:"column:created_at;not null" json:"created_at"`
}

func (AttachmentBlob) TableName() string { return "attachment_blobs" }

type RevisionAttachment struct {
	UID         string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	DocID       string `gorm:"column:doc_id;type:varchar(255);primaryKey" json:"doc_id"`
	Rev         string `gorm:"column:rev;type:varchar(191);primaryKey" json:"rev"`
	Name        string `gorm:"column:name;type:varchar(120);primaryKey" json:"name"`
	Digest      string `gorm:"column:digest;type:varchar(191);not null" json:"digest"`
	ContentType string `gorm:"column:content_type;type:varchar(191);not null" json:"content_type"`
	Length      int64  `gorm:"column:length;not null" json:"length"`
	Revpos      int    `gorm:"column:revpos;not null" json:"revpos"`
	CreatedAt   int64  `gorm:"column:created_at;not null" json:"created_at"`
}

func (RevisionAttachment) TableName() string { return "revision_attachments" }

type SyncMeta struct {
	Key   string `gorm:"column:key;type:varchar(191);primaryKey" json:"key"`
	Value string `gorm:"column:value;not null" json:"value"`
}

func (SyncMeta) TableName() string { return "sync_meta" }

type SyncCheckpoint struct {
	UID            string `gorm:"column:uid;type:varchar(191);primaryKey" json:"uid"`
	CheckpointID   string `gorm:"column:checkpoint_id;type:varchar(191);primaryKey" json:"checkpoint_id"`
	SourceID       string `gorm:"column:source_id;type:varchar(191);not null" json:"source_id"`
	TargetID       string `gorm:"column:target_id;type:varchar(191);not null" json:"target_id"`
	LastSeq        int64  `gorm:"column:last_seq;not null;default:0" json:"last_seq"`
	CheckpointJSON string `gorm:"column:checkpoint_json;not null" json:"checkpoint_json"`
	UpdatedAt      int64  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (SyncCheckpoint) TableName() string { return "sync_checkpoints" }
