// Package platformmysql provides the shared MySQL connection and migration
// runner for service-owned adapters. It owns no business tables and exposes no
// domain models.
package platformmysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"cityflow/internal/migrations"
)

type DB struct {
	ORM *gorm.DB
	sql *sql.DB
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	config, err := mysql.ParseDSN(strings.TrimSpace(dsn))
	if err != nil {
		return nil, fmt.Errorf("parse MySQL DSN: %w", err)
	}
	config.ParseTime = true
	config.Loc = time.UTC
	config.Params = mergeParams(config.Params, map[string]string{"charset": "utf8mb4"})
	sqlDB, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open MySQL connection pool: %w", err)
	}
	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		TranslateError: true, DisableAutomaticPing: true,
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("initialize GORM MySQL adapter: %w", err)
	}
	maxOpen := positiveEnv("DATABASE_MAX_OPEN_CONNS", 12)
	maxIdle := positiveEnv("DATABASE_MAX_IDLE_CONNS", 4)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxIdleTime(time.Duration(positiveEnv("DATABASE_CONN_MAX_IDLE_SECONDS", 300)) * time.Second)
	sqlDB.SetConnMaxLifetime(time.Duration(positiveEnv("DATABASE_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second)
	if err := pingWithBackoff(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return &DB{ORM: orm, sql: sqlDB}, nil
}

func pingWithBackoff(ctx context.Context, db *sql.DB) error {
	var pingErr error
	backoff := 250 * time.Millisecond
	for {
		if pingErr = db.PingContext(ctx); pingErr == nil {
			return nil
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("connect to MySQL: %w (last ping: %v)", ctx.Err(), pingErr)
		case <-timer.C:
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}

func (d *DB) Close() error {
	if d == nil || d.sql == nil {
		return nil
	}
	return d.sql.Close()
}

func (d *DB) Ping(ctx context.Context) error {
	if d == nil || d.sql == nil {
		return errors.New("mysql is not initialized")
	}
	return d.sql.PingContext(ctx)
}

func (d *DB) Stats() sql.DBStats {
	if d == nil || d.sql == nil {
		return sql.DBStats{}
	}
	return d.sql.Stats()
}

// SQL exposes the underlying pool for service adapters that need statement-
// level control (bulk upserts, advisory locks or migrations).
func (d *DB) SQL() *sql.DB {
	if d == nil {
		return nil
	}
	return d.sql
}

func MigrateService(ctx context.Context, db *DB, service string) error {
	switch service {
	case "identity", "task", "discovery", "payment", "promotion", "notification", "tracking":
	default:
		return fmt.Errorf("unsupported service migration %q", service)
	}
	return applyMigrationSet(ctx, db, "service/"+service+"*.sql", "service")
}

func applyMigrationSet(ctx context.Context, db *DB, pattern, prefix string) error {
	conn, release, err := withLock(ctx, db, "cityflow_schema_migrations")
	if err != nil {
		return err
	}
	defer release()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(128) NOT NULL PRIMARY KEY,
		applied_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	files, err := fs.Glob(migrations.Files, pattern)
	if err != nil {
		return err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no migrations matched %s", pattern)
	}
	for _, name := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(name, prefix+"/"), ".sql")
		version = prefix + "/" + version
		var applied bool
		if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=?)", version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}
		content, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		for _, statement := range splitSQLStatements(string(content)) {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply migration %s: %w", version, err)
			}
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", version); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}
	return nil
}

func withLock(ctx context.Context, db *DB, name string) (*sql.Conn, func(), error) {
	conn, err := db.sql.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reserve MySQL connection: %w", err)
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 20)", name).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("acquire MySQL lock %s: %w", name, err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("could not acquire MySQL lock %s", name)
	}
	release := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx, "SELECT RELEASE_LOCK(?)", name)
		_ = conn.Close()
	}
	return conn, release, nil
}

func splitSQLStatements(content string) []string {
	var statements []string
	var statement strings.Builder
	var lineComment, blockComment, singleQuote, doubleQuote, backtick bool
	for i := 0; i < len(content); i++ {
		c := content[i]
		switch {
		case lineComment:
			if c == '\n' {
				lineComment = false
				statement.WriteByte(c)
			}
			continue
		case blockComment:
			if c == '*' && i+1 < len(content) && content[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		case singleQuote:
			if c == '\\' && i+1 < len(content) {
				statement.WriteByte(c)
				statement.WriteByte(content[i+1])
				i++
				continue
			}
			if c == '\'' {
				if i+1 < len(content) && content[i+1] == '\'' {
					statement.WriteString("''")
					i++
					continue
				}
				singleQuote = false
			}
			statement.WriteByte(c)
			continue
		case doubleQuote:
			if c == '\\' && i+1 < len(content) {
				statement.WriteByte(c)
				statement.WriteByte(content[i+1])
				i++
				continue
			}
			if c == '"' {
				if i+1 < len(content) && content[i+1] == '"' {
					statement.WriteString("\"\"")
					i++
					continue
				}
				doubleQuote = false
			}
			statement.WriteByte(c)
			continue
		case backtick:
			if c == '`' {
				if i+1 < len(content) && content[i+1] == '`' {
					statement.WriteString("``")
					i++
					continue
				}
				backtick = false
			}
			statement.WriteByte(c)
			continue
		}
		if c == '-' && i+1 < len(content) && content[i+1] == '-' {
			lineComment = true
			i++
			continue
		}
		if c == '/' && i+1 < len(content) && content[i+1] == '*' {
			blockComment = true
			i++
			continue
		}
		switch c {
		case '\'':
			singleQuote = true
		case '"':
			doubleQuote = true
		case '`':
			backtick = true
		case ';':
			if value := strings.TrimSpace(statement.String()); value != "" {
				statements = append(statements, value)
			}
			statement.Reset()
			continue
		}
		statement.WriteByte(c)
	}
	if value := strings.TrimSpace(statement.String()); value != "" {
		statements = append(statements, value)
	}
	return statements
}

func positiveEnv(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func mergeParams(existing, additions map[string]string) map[string]string {
	if existing == nil {
		existing = make(map[string]string, len(additions))
	}
	for key, value := range additions {
		existing[key] = value
	}
	return existing
}
