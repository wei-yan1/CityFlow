package taskmysql

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// UpdateLikeCounts applies one absolute-count batch from Reaction. The message
// carries the latest total, not a delta, so duplicate delivery is harmless.
func (s *Store) UpdateLikeCounts(ctx context.Context, targetType string, counts map[string]int64) error {
	if len(counts) == 0 {
		return nil
	}
	table := ""
	switch targetType {
	case "RIDER_REVIEW":
		table = "rider_feedback"
	case "RIDER_COMMENT":
		table = riderCommentTable
	default:
		return nil
	}
	return s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for id, count := range counts {
			if id == "" {
				continue
			}
			if count < 0 {
				count = 0
			}
			if err := tx.Exec(`UPDATE `+table+` SET like_count=? WHERE id=?`, count, id).Error; err != nil {
				return fmt.Errorf("update %s like_count: %w", table, err)
			}
		}
		return nil
	})
}
