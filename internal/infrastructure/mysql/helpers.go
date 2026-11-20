package mysqlstore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
)

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

func normalizePage(page repository.Page) repository.Page {
	if page.Limit <= 0 || page.Limit > 200 {
		page.Limit = 50
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	return page
}

// paginate applies the same bounds to every GORM list query.
func paginate(page repository.Page) func(*gorm.DB) *gorm.DB {
	page = normalizePage(page)
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
