package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ZToolsCenter/ztools-sync-server/store"
)

type StandaloneConfig struct {
	Port              string
	DataDir           string
	Database          store.Options
	JWTSecret         string
	BootstrapUsername string
	BootstrapPassword string
	AllowRegistration bool
}

func LoadStandalone() (StandaloneConfig, error) {
	dataDir := getenv("DATA_DIR", "data/standalone")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return StandaloneConfig{}, fmt.Errorf("create data directory: %w", err)
	}

	driver := store.Driver(strings.ToLower(getenv("DB_DRIVER", string(store.DriverSQLite))))
	dsn := ""
	switch driver {
	case store.DriverSQLite:
		dsn = getenv("SQLITE_PATH", filepath.Join(dataDir, "ztools.db"))
	case store.DriverMySQL:
		dsn = mysqlDSN()
	default:
		return StandaloneConfig{}, fmt.Errorf("unsupported DB_DRIVER: %s", driver)
	}

	secret, err := loadOrCreateJWTSecret(dataDir)
	if err != nil {
		return StandaloneConfig{}, err
	}
	allowRegistration, err := parseBoolEnv("ALLOW_REGISTRATION", false)
	if err != nil {
		return StandaloneConfig{}, err
	}
	username := strings.TrimSpace(os.Getenv("ZTOOLS_USERNAME"))
	password := os.Getenv("ZTOOLS_PASSWORD")
	if (username == "") != (password == "") {
		return StandaloneConfig{}, errors.New("ZTOOLS_USERNAME and ZTOOLS_PASSWORD must be configured together")
	}

	return StandaloneConfig{
		Port:              getenv("PORT", "23517"),
		DataDir:           dataDir,
		Database:          store.Options{Driver: driver, DSN: dsn},
		JWTSecret:         secret,
		BootstrapUsername: username,
		BootstrapPassword: password,
		AllowRegistration: allowRegistration,
	}, nil
}

func loadOrCreateJWTSecret(dataDir string) (string, error) {
	if value := strings.TrimSpace(os.Getenv("JWT_SECRET")); value != "" {
		return value, nil
	}
	path := filepath.Join(dataDir, "server-secret")
	if data, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(data))
		if value == "" {
			return "", errors.New("persisted JWT secret is empty")
		}
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read JWT secret: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate JWT secret: %w", err)
	}
	value := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist JWT secret: %w", err)
	}
	return value, nil
}

func parseBoolEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func mysqlDSN() string {
	if value := strings.TrimSpace(os.Getenv("MYSQL_DSN")); value != "" {
		return value
	}
	user := getenv("MYSQL_USER", "root")
	password := os.Getenv("MYSQL_PASSWORD")
	host := getenv("MYSQL_HOST", "127.0.0.1")
	port := getenv("MYSQL_PORT", "3306")
	database := getenv("MYSQL_DATABASE", "ztools_sync")
	return user + ":" + password + "@tcp(" + host + ":" + port + ")/" + database + "?charset=utf8mb4&parseTime=True&loc=Local"
}

func getenv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
