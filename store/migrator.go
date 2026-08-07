package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/sqlite/*.sql migrations/mysql/*.sql
var migrationFiles embed.FS

type Migration struct {
	Version string
	Name    string
	Path    string
	SQL     string
}

func RunSQLiteMigrations(sqlDB *sql.DB) error {
	return runMigrations(sqlDB, "migrations/sqlite", DriverSQLite)
}

func RunMySQLMigrations(sqlDB *sql.DB) error {
	return runMigrations(sqlDB, "migrations/mysql", DriverMySQL)
}

func runMigrations(sqlDB *sql.DB, directory string, driver Driver) error {
	if err := ensureMigrationTable(sqlDB, driver); err != nil {
		return err
	}
	migrations, err := loadMigrations(directory)
	if err != nil {
		return err
	}
	applied, err := appliedVersions(sqlDB)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}
		if err := applyMigration(sqlDB, migration, driver); err != nil {
			return err
		}
	}
	return nil
}

func ensureMigrationTable(db *sql.DB, driver Driver) error {
	statement := `
CREATE TABLE IF NOT EXISTS ztools_core_migrations (
  version VARCHAR(64) NOT NULL PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  checksum VARCHAR(64) NOT NULL,
  applied_at BIGINT NOT NULL
)`
	if driver == DriverMySQL {
		statement += " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	}
	_, err := db.Exec(statement)
	return err
}

func loadMigrations(directory string) ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFiles, directory)
	if err != nil {
		return nil, err
	}
	migrations := make([]Migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, name, ok := parseMigrationFilename(entry.Name())
		if !ok {
			return nil, fmt.Errorf("invalid migration filename: %s", entry.Name())
		}
		path := directory + "/" + entry.Name()
		data, err := migrationFiles.ReadFile(path)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, Migration{
			Version: version,
			Name:    name,
			Path:    path,
			SQL:     string(data),
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

func parseMigrationFilename(filename string) (string, string, bool) {
	base := strings.TrimSuffix(filename, ".sql")
	parts := strings.SplitN(base, "__", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "V") {
		return "", "", false
	}
	version := strings.TrimPrefix(parts[0], "V")
	if version == "" || parts[1] == "" {
		return "", "", false
	}
	return version, parts[1], true
}

func appliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM ztools_core_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		result[version] = true
	}
	return result, rows.Err()
}

func applyMigration(db *sql.DB, migration Migration, driver Driver) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := splitSQLStatements(migration.SQL)
	for _, statement := range statements {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		executable, err := shouldExecuteStatement(tx, statement, driver)
		if err != nil {
			return err
		}
		if !executable {
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply migration %s failed: %w", migration.Path, err)
		}
	}
	checksum := fmt.Sprintf("%x", fnv64(migration.SQL))
	if _, err := tx.Exec(
		"INSERT INTO ztools_core_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)",
		migration.Version,
		migration.Name,
		checksum,
		time.Now().UnixMilli(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func splitSQLStatements(sqlText string) []string {
	var statements []string
	var current strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "-- IF ") {
			current.WriteString(line)
			current.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
		if strings.HasSuffix(trimmed, ";") {
			statements = append(statements, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, strings.TrimSpace(current.String()))
	}
	return statements
}

func shouldExecuteStatement(tx *sql.Tx, statement string, driver Driver) (bool, error) {
	lines := strings.Split(statement, "\n")
	if len(lines) == 0 {
		return true, nil
	}
	trimmed := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(trimmed, "-- IF TABLE EXISTS ") {
		return true, nil
	}
	if driver != DriverMySQL {
		return false, fmt.Errorf("conditional table migration is not supported for %s", driver)
	}
	table := strings.TrimSpace(strings.TrimPrefix(trimmed, "-- IF TABLE EXISTS "))
	var count int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
		table,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func fnv64(value string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	hash := uint64(offset64)
	for i := 0; i < len(value); i++ {
		hash ^= uint64(value[i])
		hash *= prime64
	}
	return hash
}
