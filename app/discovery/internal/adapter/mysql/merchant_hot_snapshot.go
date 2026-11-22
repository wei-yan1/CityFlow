package discoverymysql

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"cityflow/app/discovery/repository"
)

type merchantHotSnapshotRecord struct {
	Month      string `gorm:"column:month;type:char(6);primaryKey"`
	Category   string `gorm:"column:category;type:varchar(16);primaryKey"`
	RankNo     uint8  `gorm:"column:rank_no;primaryKey"`
	MerchantID string `gorm:"column:merchant_id;type:char(36);not null"`
	LikeCount  int64  `gorm:"column:like_count;not null"`
}

func (merchantHotSnapshotRecord) TableName() string { return "merchant_hot_snapshot" }

// ListMerchantHotSnapshot returns a frozen monthly ranking ordered by the
// persisted rank. Unlike the live hot query it does not inspect Redis or the
// current like counts, so historical pages stay stable after the month closes.
func (s *Store) ListMerchantHotSnapshot(ctx context.Context, month, category string, limit int) ([]repository.MerchantHotSnapshotItem, error) {
	if limit <= 0 {
		return []repository.MerchantHotSnapshotItem{}, nil
	}
	var records []merchantHotSnapshotRecord
	if err := s.db.ORM.WithContext(ctx).
		Where("month = ? AND category = ?", month, category).
		Order("rank_no ASC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list merchant hot snapshot %s/%s: %w", month, category, err)
	}
	items := make([]repository.MerchantHotSnapshotItem, 0, len(records))
	for _, record := range records {
		items = append(items, repository.MerchantHotSnapshotItem{
			Rank: int(record.RankNo), MerchantID: record.MerchantID, LikeCount: record.LikeCount,
		})
	}
	return items, nil
}

// ReplaceMerchantHotSnapshot swaps one month/category snapshot in a single
// transaction. A failed insert rolls the delete back, so readers never see a
// half-written Top N. Empty items still clears the old snapshot.
func (s *Store) ReplaceMerchantHotSnapshot(ctx context.Context, month, category string, items []repository.MerchantHotSnapshotItem) error {
	return s.db.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"DELETE FROM merchant_hot_snapshot WHERE month = ? AND category = ?",
			month, category,
		).Error; err != nil {
			return fmt.Errorf("clear merchant hot snapshot %s/%s: %w", month, category, err)
		}
		if len(items) == 0 {
			return nil
		}
		records := make([]merchantHotSnapshotRecord, 0, len(items))
		for _, item := range items {
			if item.Rank <= 0 || item.Rank > 255 {
				return fmt.Errorf("merchant hot snapshot rank %d is out of range", item.Rank)
			}
			records = append(records, merchantHotSnapshotRecord{
				Month: month, Category: category,
				RankNo: uint8(item.Rank), MerchantID: item.MerchantID, LikeCount: item.LikeCount,
			})
		}
		if err := tx.Create(&records).Error; err != nil {
			return fmt.Errorf("write merchant hot snapshot %s/%s: %w", month, category, err)
		}
		return nil
	})
}
