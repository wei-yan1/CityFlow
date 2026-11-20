package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"cityflow/internal/migrations"
)

func (s *Store) MigrateAllServices(ctx context.Context) error {
	for _, service := range []string{"identity", "task", "discovery", "payment", "reaction"} {
		if err := s.MigrateService(ctx, service); err != nil {
			return fmt.Errorf("migrate %s service: %w", service, err)
		}
	}
	return nil
}

// MigrateService applies only the schema owned by one go-zero service. The
// service must use its own database/account; this function never creates the
// legacy cityflow tables.
func (s *Store) MigrateService(ctx context.Context, service string) error {
	switch service {
	case "identity", "task", "discovery", "payment", "reaction":
	default:
		return fmt.Errorf("unsupported service migration %q", service)
	}
	return s.applyMigrationSet(ctx, "service/"+service+"*.sql", "service")
}

func (s *Store) applyMigrationSet(ctx context.Context, pattern, prefix string) error {
	conn, release, err := s.withLock(ctx, "cityflow_schema_migrations")
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

// splitSQLStatements splits a migration file without treating semicolons in
// comments or quoted values as statement terminators. Migration comments
// commonly contain prose such as "drop old index; create replacement", and
// naive strings.Split corrupted those files before this scanner existed.
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

func (s *Store) withLock(ctx context.Context, name string) (*sql.Conn, func(), error) {
	conn, err := s.db.Conn(ctx)
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
