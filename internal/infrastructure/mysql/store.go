package mysqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"cityflow/internal/application/repository"
)

type Store struct {
	orm *gorm.DB
	db  *sql.DB // Used for versioned SQL migrations and MySQL advisory locks.
}

var _ repository.AuthRepository = (*Store)(nil)
var _ repository.UserRepository = (*Store)(nil)
var _ repository.MerchantRepository = (*Store)(nil)
var _ repository.RiderRepository = (*Store)(nil)
var _ repository.TaskRepository = (*Store)(nil)
var _ repository.PaymentRepository = (*Store)(nil)
var _ repository.RiderFeedback = (*Store)(nil)

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := mysql.ParseDSN(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse MySQL DSN: %w", err)
	}
	config.ParseTime = true
	config.Loc = time.UTC
	config.Params = mergeParams(config.Params, map[string]string{"charset": "utf8mb4"})
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open MySQL connection pool: %w", err)
	}
	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{
		TranslateError: true, DisableAutomaticPing: true,
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize GORM MySQL adapter: %w", err)
	}
	maxOpen := positiveEnv("DATABASE_MAX_OPEN_CONNS", 12)
	maxIdle := positiveEnv("DATABASE_MAX_IDLE_CONNS", 4)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxIdleTime(time.Duration(positiveEnv("DATABASE_CONN_MAX_IDLE_SECONDS", 300)) * time.Second)
	db.SetConnMaxLifetime(time.Duration(positiveEnv("DATABASE_CONN_MAX_LIFETIME_SECONDS", 1800)) * time.Second)
	var pingErr error
	backoff := 250 * time.Millisecond
	for {
		if pingErr = db.PingContext(ctx); pingErr == nil {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = db.Close()
			return nil, fmt.Errorf("connect to MySQL: %w (last ping: %v)", ctx.Err(), pingErr)
		case <-timer.C:
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
	return &Store{orm: orm, db: db}, nil
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

func (s *Store) Close() { _ = s.db.Close() }

// DBStats exposes the database/sql pool counters for operational logging.
// WaitCount/WaitDuration are the direct signal that handlers are queuing for a
// connection rather than waiting on MySQL itself.
func (s *Store) DBStats() sql.DBStats {
	if s == nil || s.db == nil {
		return sql.DBStats{}
	}
	return s.db.Stats()
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
