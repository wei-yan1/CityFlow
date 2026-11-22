package taskmysql

import (
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"cityflow/app/task/repository"
	"cityflow/internal/common/domain"
)

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
