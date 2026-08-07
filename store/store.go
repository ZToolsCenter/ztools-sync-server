package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/ZToolsCenter/ztools-sync-server/models"
)

const ProtocolVersion = 2

type Driver string

const (
	DriverMySQL  Driver = "mysql"
	DriverSQLite Driver = "sqlite"
)

type Options struct {
	Driver Driver
	DSN    string
}

/**
 * Open 建立数据库连接、执行迁移并初始化同步元数据。
 * @param dsn MySQL 数据源连接字符串。
 * @returns 初始化后的 GORM 连接和可能发生的错误。
 */
func Open(dsn string) (*gorm.DB, error) {
	return OpenWithOptions(Options{Driver: DriverMySQL, DSN: dsn})
}

// OpenWithOptions opens a supported database and initializes the public sync schema.
func OpenWithOptions(options Options) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch options.Driver {
	case DriverMySQL:
		dialector = mysql.Open(options.DSN)
	case DriverSQLite:
		if strings.TrimSpace(options.DSN) == "" {
			return nil, errors.New("sqlite dsn is required")
		}
		dialector = sqlite.Open(options.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", options.Driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{Logger: productionLogger()})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if err := configureDatabase(db, sqlDB, options.Driver); err != nil {
		return nil, err
	}
	if err := runDriverMigrations(sqlDB, options.Driver); err != nil {
		return nil, err
	}
	if err := validateSchema(db); err != nil {
		return nil, err
	}
	if err := InitializeSyncMetadata(db); err != nil {
		return nil, err
	}
	return db, nil
}

func configureDatabase(db *gorm.DB, sqlDB *sql.DB, driver Driver) error {
	switch driver {
	case DriverMySQL:
		// 限制连接池规模，避免异常同步流量耗尽小内存实例的数据库资源。
		sqlDB.SetMaxOpenConns(32)
		sqlDB.SetMaxIdleConns(8)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
		return nil
	case DriverSQLite:
		// 独立版优先保证单进程写入顺序；同步事务很短，单连接足以覆盖个人部署场景。
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
		for _, statement := range []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA busy_timeout=5000",
			"PRAGMA foreign_keys=ON",
			"PRAGMA synchronous=NORMAL",
		} {
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("configure sqlite with %s: %w", statement, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported database driver: %s", driver)
	}
}

func runDriverMigrations(sqlDB *sql.DB, driver Driver) error {
	switch driver {
	case DriverMySQL:
		return RunMySQLMigrations(sqlDB)
	case DriverSQLite:
		return RunSQLiteMigrations(sqlDB)
	default:
		return fmt.Errorf("unsupported database driver: %s", driver)
	}
}

/**
 * productionLogger 创建不会展开 SQL 参数的生产日志器，避免大文档和敏感值进入日志。
 * @returns 配置完成的 GORM 日志接口。
 */
func productionLogger() logger.Interface {
	return logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	})
}

func validateSchema(db *gorm.DB) error {
	requiredTables := []string{
		"users",
		"documents",
		"document_revisions",
		"document_write_locks",
		"changelog",
		"device_sync_state",
		"attachment_blobs",
		"revision_attachments",
		"sync_meta",
		"sync_checkpoints",
		"ztools_core_migrations",
	}
	for _, table := range requiredTables {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("required table %s is missing", table)
		}
	}
	return nil
}

// InitializeSyncMetadata ensures the protocol metadata shared by SaaS and standalone deployments.
func InitializeSyncMetadata(db *gorm.DB) error {
	if err := setMetaIfMissing(db, "protocol_version", strconv.Itoa(ProtocolVersion)); err != nil {
		return err
	}
	var maxSeq int64
	if err := db.Model(&models.Changelog{}).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
		return err
	}
	if err := setMetaIfMissing(db, "sync_epoch", strconv.FormatInt(maxSeq, 10)); err != nil {
		return err
	}
	serverID, err := newServerInstanceID()
	if err != nil {
		return err
	}
	return setMetaIfMissing(db, "server_instance_id", serverID)
}

func newServerInstanceID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate server instance id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func setMetaIfMissing(db *gorm.DB, key string, value string) error {
	var meta models.SyncMeta
	err := db.First(&meta, "`key` = ?", key).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&models.SyncMeta{Key: key, Value: value}).Error
}

func ParseGeneration(rev string) int {
	if rev == "" {
		return 0
	}
	for i, r := range rev {
		if r == '-' {
			n, _ := strconv.Atoi(rev[:i])
			return n
		}
	}
	n, _ := strconv.Atoi(rev)
	return n
}
