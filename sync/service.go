package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	stdsync "sync"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/ZToolsCenter/ztools-sync-server/models"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
)

const MaxAttachmentSize = 10 * 1024 * 1024
const maxPushTransactionChanges = 20
const maxPushTransactionAttempts = 3
const maxConcurrentPushes = 16

var ErrAttachmentDigestMismatch = errors.New("attachment digest mismatch")
var ErrInvalidCheckpoint = errors.New("invalid checkpoint")

type Broadcast struct {
	UID            string
	SenderDeviceID string
	Change         FullChangeEntry
}

type PushResult struct {
	LastSeq        int64
	Broadcasts     []Broadcast
	MissingDigests []string
}

type Service struct {
	repo                      *repository.Repository
	documentLocks             *documentLockManager
	pushSlots                 chan struct{}
	retryableTransactionError func(error) bool
}

type ServiceOptions struct {
	MaxConcurrentPushes       int
	RetryableTransactionError func(error) bool
}

type documentLockKey struct {
	uid   string
	docID string
}

type documentLockEntry struct {
	mu   stdsync.Mutex
	refs int
}

type documentLockManager struct {
	mu      stdsync.Mutex
	entries map[documentLockKey]*documentLockEntry
}

/**
 * newDocumentLockManager 创建按用户和文档隔离的进程内锁管理器。
 * @returns 初始化后的文档锁管理器。
 */
func newDocumentLockManager() *documentLockManager {
	return &documentLockManager{entries: make(map[documentLockKey]*documentLockEntry)}
}

/**
 * lock 按稳定顺序锁定一组文档，并返回负责解锁和清理引用的函数。
 * @param uid 文档所属用户标识。
 * @param docIDs 已去重的文档标识列表。
 * @returns 完成当前分段后必须调用的解锁函数。
 */
func (m *documentLockManager) lock(uid string, docIDs []string) func() {
	keys := make([]documentLockKey, 0, len(docIDs))
	entries := make([]*documentLockEntry, 0, len(docIDs))

	// 先登记全部引用，防止等待期间锁条目被其他请求提前清理。
	m.mu.Lock()
	for _, docID := range docIDs {
		key := documentLockKey{uid: uid, docID: docID}
		entry := m.entries[key]
		if entry == nil {
			entry = &documentLockEntry{}
			m.entries[key] = entry
		}
		entry.refs++
		keys = append(keys, key)
		entries = append(entries, entry)
	}
	m.mu.Unlock()

	// docIDs 已按字典序排列，所有请求以相同顺序加锁以规避进程内死锁。
	for _, entry := range entries {
		entry.mu.Lock()
	}

	return func() {
		// 逆序释放锁，缩短后续等待者进入临界区的时间。
		for index := len(entries) - 1; index >= 0; index-- {
			entries[index].mu.Unlock()
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for index, key := range keys {
			entries[index].refs--
			if entries[index].refs == 0 {
				delete(m.entries, key)
			}
		}
	}
}

/**
 * NewService 创建带文档级串行锁和全局并发保护的同步服务。
 * @param repo 同步服务使用的数据仓储。
 * @returns 初始化后的同步服务。
 */
func NewService(repo *repository.Repository) *Service {
	return NewServiceWithOptions(repo, ServiceOptions{
		MaxConcurrentPushes:       maxConcurrentPushes,
		RetryableTransactionError: isRetryableMySQLTransactionError,
	})
}

func NewServiceWithOptions(repo *repository.Repository, options ServiceOptions) *Service {
	if options.MaxConcurrentPushes <= 0 {
		options.MaxConcurrentPushes = 1
	}
	if options.RetryableTransactionError == nil {
		options.RetryableTransactionError = func(error) bool { return false }
	}
	return &Service{
		repo:                      repo,
		documentLocks:             newDocumentLockManager(),
		pushSlots:                 make(chan struct{}, options.MaxConcurrentPushes),
		retryableTransactionError: options.RetryableTransactionError,
	}
}

func (s *Service) MaxSeq(uid string) (int64, error) {
	return s.repo.MaxSeq(uid)
}

func (s *Service) SyncEpoch() int64 {
	meta, err := s.repo.SyncMeta("sync_epoch")
	if err != nil {
		return 0
	}
	value, _ := strconv.ParseInt(meta.Value, 10, 64)
	return value
}

func (s *Service) ServerInstanceID() string {
	meta, err := s.repo.SyncMeta("server_instance_id")
	if err != nil {
		return ""
	}
	return meta.Value
}

func (s *Service) UpsertDeviceState(uid string, deviceID string, deviceName string) error {
	return s.repo.UpsertDeviceState(models.DeviceSyncState{
		UID:        uid,
		DeviceID:   deviceID,
		LastSeq:    0,
		LastSeen:   time.Now().UnixMilli(),
		DeviceName: deviceName,
	})
}

func (s *Service) UpdateDeviceSeq(uid string, deviceID string, seq int64) error {
	return s.repo.UpdateDeviceSeq(uid, deviceID, seq, time.Now().UnixMilli())
}

func (s *Service) GetCheckpoint(uid string, checkpointID string) (*CheckpointPayload, error) {
	if checkpointID == "" {
		return nil, ErrInvalidCheckpoint
	}
	row, err := s.repo.SyncCheckpoint(uid, checkpointID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload CheckpointPayload
	if err := json.Unmarshal([]byte(row.CheckpointJSON), &payload); err != nil {
		payload = CheckpointPayload{}
	}
	payload.ID = row.CheckpointID
	payload.SourceID = row.SourceID
	payload.TargetID = row.TargetID
	payload.LastSeq = row.LastSeq
	payload.UpdatedAt = row.UpdatedAt
	return &payload, nil
}

func (s *Service) PutCheckpoint(uid string, checkpoint CheckpointPayload) (*CheckpointPayload, error) {
	if checkpoint.ID == "" {
		return nil, ErrInvalidCheckpoint
	}
	if checkpoint.LastSeq == 0 && checkpoint.RemotePullSeq > 0 {
		checkpoint.LastSeq = checkpoint.RemotePullSeq
	}
	checkpoint.UpdatedAt = time.Now().UnixMilli()
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return nil, err
	}
	row := models.SyncCheckpoint{
		UID:            uid,
		CheckpointID:   checkpoint.ID,
		SourceID:       checkpoint.SourceID,
		TargetID:       checkpoint.TargetID,
		LastSeq:        checkpoint.LastSeq,
		CheckpointJSON: string(raw),
		UpdatedAt:      checkpoint.UpdatedAt,
	}
	if err := s.repo.UpsertSyncCheckpoint(row); err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

/**
 * Push 校验并按有限大小的事务顺序写入客户端 revision，保持旧客户端协议不变。
 * @param uid 当前认证用户标识。
 * @param deviceID 发起同步的设备标识。
 * @param changes 客户端提交的 revision 列表。
 * @param protocolVersion 客户端使用的同步协议版本。
 * @returns 已提交的序号、广播列表、缺失附件以及可能发生的错误。
 */
func (s *Service) Push(uid string, deviceID string, changes []FullChangeEntry, protocolVersion int) (PushResult, error) {
	// 全局并发门控制进入同步写路径的请求数量，避免请求风暴耗尽数据库连接。
	s.pushSlots <- struct{}{}
	defer func() { <-s.pushSlots }()

	// 在任何写入前完成附件校验，避免缺少 blob 时产生部分提交。
	missing, err := s.missingAttachmentDigests(uid, changes)
	if err != nil {
		return PushResult{}, err
	}
	if len(missing) > 0 {
		return PushResult{MissingDigests: missing}, nil
	}

	// 按原顺序分段提交，限制单个事务持有文档锁和 undo/binlog 资源的时间。
	result := PushResult{}
	for start := 0; start < len(changes); start += maxPushTransactionChanges {
		end := start + maxPushTransactionChanges
		if end > len(changes) {
			end = len(changes)
		}
		chunkResult, err := s.pushChunk(uid, deviceID, changes[start:end], protocolVersion)
		if chunkResult.LastSeq > 0 {
			result.LastSeq = chunkResult.LastSeq
		}
		result.Broadcasts = append(result.Broadcasts, chunkResult.Broadcasts...)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

/**
 * pushChunk 在一个短事务内锁定涉及的文档并写入一段 revision。
 * @param uid 当前认证用户标识。
 * @param deviceID 发起同步的设备标识。
 * @param changes 当前事务处理的 revision 子列表。
 * @param protocolVersion 客户端使用的同步协议版本。
 * @returns 当前分段已提交的序号、广播列表以及可能发生的错误。
 */
func (s *Service) pushChunk(uid string, deviceID string, changes []FullChangeEntry, protocolVersion int) (PushResult, error) {
	docIDs := uniqueSortedDocIDs(changes)
	releaseDocumentLocks := s.documentLocks.lock(uid, docIDs)
	defer releaseDocumentLocks()

	var committed PushResult
	var err error
	for attempt := 1; attempt <= maxPushTransactionAttempts; attempt++ {
		candidate := PushResult{}
		err = s.repo.Transaction(func(tx *gorm.DB) error {
			// 统一排序锁键，避免包含多个文档的并发批次形成反向加锁死锁。
			for _, docID := range docIDs {
				if err := repository.AcquireDocumentWriteLock(tx, uid, docID, time.Now().UnixMilli()); err != nil {
					return err
				}
			}
			for _, change := range changes {
				result, err := insertRevisionChange(tx, uid, deviceID, change, protocolVersion)
				if err != nil {
					return err
				}
				if result.Inserted && result.LastSeq > 0 && result.Revision != nil {
					candidate.LastSeq = result.LastSeq
					winnerRev := change.Rev
					if result.Winner != nil {
						winnerRev = result.Winner.Rev
					}
					candidate.Broadcasts = append(candidate.Broadcasts, Broadcast{
						UID:            uid,
						SenderDeviceID: deviceID,
						Change:         buildDocChangeFromRevision(*result.Revision, candidate.LastSeq, winnerRev, change.Resolution),
					})
				}
			}
			return nil
		})
		if err == nil {
			return candidate, nil
		}
		if !s.retryableTransactionError(err) || attempt == maxPushTransactionAttempts {
			return committed, err
		}

		// 只对死锁和锁等待超时短暂退避，其他错误立即返回给客户端。
		time.Sleep(time.Duration(attempt*25) * time.Millisecond)
	}
	return committed, err
}

/**
 * uniqueSortedDocIDs 提取并排序批次内的文档标识，提供稳定的数据库加锁顺序。
 * @param changes 待处理的 revision 列表。
 * @returns 去重并按字典序排序的文档标识。
 */
func uniqueSortedDocIDs(changes []FullChangeEntry) []string {
	docIDSet := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		docIDSet[change.DocID] = struct{}{}
	}
	docIDs := make([]string, 0, len(docIDSet))
	for docID := range docIDSet {
		docIDs = append(docIDs, docID)
	}
	sort.Strings(docIDs)
	return docIDs
}

/**
 * isRetryableTransactionError 判断事务是否因 MySQL 死锁或锁等待超时而适合重试。
 * @param err 数据库事务返回的错误。
 * @returns 可以安全重试时返回 true。
 */
func isRetryableMySQLTransactionError(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	return mysqlErr.Number == 1205 || mysqlErr.Number == 1213
}

func (s *Service) Changes(uid string, since int64, clientProtocolVersion int) ([]FullChangeEntry, int64, error) {
	logs, err := s.repo.ChangelogSince(uid, since)
	if err != nil {
		return nil, since, err
	}
	changes := make([]FullChangeEntry, 0, len(logs))
	for _, entry := range logs {
		if clientProtocolVersion >= store.ProtocolVersion || entry.ProtocolVersion >= store.ProtocolVersion {
			revision, err := s.repo.Revision(uid, entry.DocID, entry.Rev)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return nil, since, err
			}
			attachments, err := s.repo.RevisionAttachments(uid, entry.DocID, entry.Rev)
			if err != nil {
				return nil, since, err
			}
			revisions, err := s.repo.DocumentRevisions(uid, entry.DocID)
			if err != nil {
				return nil, since, err
			}
			history := revisionHistoryFromRevisions(entry.Rev, revisions)
			changes = append(changes, buildDocChangeFromRevisionWithAttachmentsAndHistory(revision, attachments, history, entry.Seq, entry.WinnerRev, parseResolution(entry.Resolution)))
		}
	}
	lastSeq := since
	if len(changes) > 0 {
		lastSeq = changes[len(changes)-1].Seq
	}
	return changes, lastSeq, nil
}

func (s *Service) SnapshotChanges(uid string) ([]FullChangeEntry, error) {
	rows, err := s.repo.LeafRevisions(uid)
	if err != nil {
		return nil, err
	}
	grouped := map[string][]models.DocumentRevision{}
	for _, row := range rows {
		grouped[row.DocID] = append(grouped[row.DocID], row)
	}
	changes := make([]FullChangeEntry, 0, len(rows))
	for _, leaves := range grouped {
		winner := chooseWinner(leaves)
		winnerRev := ""
		if winner != nil {
			winnerRev = winner.Rev
		}
		for _, revision := range leaves {
			attachments, err := s.repo.RevisionAttachments(uid, revision.DocID, revision.Rev)
			if err != nil {
				return nil, err
			}
			revisions, err := s.repo.DocumentRevisions(uid, revision.DocID)
			if err != nil {
				return nil, err
			}
			history := revisionHistoryFromRevisions(revision.Rev, revisions)
			changes = append(changes, buildDocChangeFromRevisionWithAttachmentsAndHistory(revision, attachments, history, 0, winnerRev, nil))
		}
	}
	return changes, nil
}

func (s *Service) ListAttachments(uid string) ([]models.RevisionAttachment, error) {
	return s.repo.Attachments(uid)
}

func (s *Service) PutAttachmentBlob(uid string, digest string, contentType string, data []byte) (models.AttachmentBlob, error) {
	now := time.Now().UnixMilli()
	normalized := normalizeDigest(digest)
	if normalized == "" {
		normalized = attachmentDigest(data)
	}
	actual := attachmentDigest(data)
	if normalized != actual {
		return models.AttachmentBlob{}, fmt.Errorf("%w: expected %s got %s", ErrAttachmentDigestMismatch, normalized, actual)
	}
	blob := models.AttachmentBlob{UID: uid, Digest: normalized, ContentType: contentType, Length: int64(len(data)), Data: data, CreatedAt: now}
	err := s.repo.Transaction(func(tx *gorm.DB) error {
		return repository.UpsertAttachmentBlob(tx, blob)
	})
	return blob, err
}

func (s *Service) GetAttachmentBlob(uid string, digest string) (models.AttachmentBlob, error) {
	return s.repo.AttachmentBlob(uid, normalizeDigest(digest))
}

func (s *Service) CompactAttachmentBlobs(uid string, graceMs int64) (int64, error) {
	if graceMs < 0 {
		graceMs = 0
	}
	cutoff := time.Now().UnixMilli() - graceMs
	var deleted int64
	err := s.repo.Transaction(func(tx *gorm.DB) error {
		count, err := repository.DeleteUnreferencedAttachmentBlobs(tx, uid, cutoff)
		if err != nil {
			return err
		}
		deleted = count
		return nil
	})
	return deleted, err
}

func (s *Service) DB() *gorm.DB {
	return s.repo.DB()
}

func (s *Service) missingAttachmentDigests(uid string, changes []FullChangeEntry) ([]string, error) {
	digestSet := map[string]struct{}{}
	for _, change := range changes {
		if change.Deleted {
			continue
		}
		attachments, err := parseDocAttachments(change.Doc, change.Rev)
		if err != nil {
			return nil, err
		}
		for _, attachment := range attachments {
			digestSet[attachment.Digest] = struct{}{}
		}
	}
	if len(digestSet) == 0 {
		return nil, nil
	}
	digests := make([]string, 0, len(digestSet))
	for digest := range digestSet {
		digests = append(digests, digest)
	}
	existing, err := s.repo.AttachmentBlobDigestSet(uid, digests)
	if err != nil {
		return nil, err
	}
	missing := make([]string, 0)
	for _, digest := range digests {
		if !existing[digest] {
			missing = append(missing, digest)
		}
	}
	sort.Strings(missing)
	return missing, nil
}
