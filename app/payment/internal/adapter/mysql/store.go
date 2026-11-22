// Package paymentmysql is the Payment service's MySQL adapter. It owns
// payment orders, notify audit, settlements, payout accounts and the payment
// inbox; it does not query tasks, coupons or rider profiles.
package paymentmysql

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"cityflow/app/payment/repository"
	"cityflow/internal/common/domain"
	platformmysql "cityflow/internal/platform/mysql"
)

type Store struct {
	db *platformmysql.DB
}

func New(db *platformmysql.DB) *Store { return &Store{db: db} }

// DBStats exposes pool counters for the payment worker loop.
func (s *Store) DBStats() sql.DBStats {
	if s == nil || s.db == nil {
		return sql.DBStats{}
	}
	return s.db.Stats()
}

var (
	_ repository.PaymentRepository        = (*Store)(nil)
	_ repository.PaymentInboxRepository   = (*Store)(nil)
	_ repository.PaymentNotifyBatchWriter = (*Store)(nil)
)

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := fmt.Sprintf("%x", raw)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func paginate(page repository.Page) func(*gorm.DB) *gorm.DB {
	if page.Limit <= 0 || page.Limit > 200 {
		page.Limit = 50
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return func(db *gorm.DB) *gorm.DB { return db.Limit(page.Limit).Offset(page.Offset) }
}

func writeError(err error) error {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return fmt.Errorf("%w: duplicate unique value", domain.ErrConflict)
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%w: duplicate unique value", domain.ErrConflict)
	}
	return err
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func truncateReason(reason string) string {
	const limit = 255
	if len(reason) <= limit {
		return reason
	}
	return reason[:limit]
}
