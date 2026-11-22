package discoverymysql

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm/clause"

	"cityflow/app/discovery/repository"
)

type merchantLikeRecord struct {
	ID         uint64 `gorm:"column:id;primaryKey;autoIncrement"`
	MerchantID string `gorm:"column:merchant_id;type:char(36);not null"`
	UserID     string `gorm:"column:user_id;type:char(36);not null"`
	LikeMonth  string `gorm:"column:like_month;type:char(6);not null"`
}

func (merchantLikeRecord) TableName() string { return "merchant_like" }

type merchantLikeCountRow struct {
	MerchantID string `gorm:"column:merchant_id"`
	LikeCount  int64  `gorm:"column:like_count"`
}

// AddMerchantLike inserts the month-scoped fact and reports whether this call
// actually created a new row. The unique key makes concurrent duplicate likes
// harmless.
func (s *Store) AddMerchantLike(ctx context.Context, merchantID, userID, month string) (bool, error) {
	record := merchantLikeRecord{MerchantID: merchantID, UserID: userID, LikeMonth: month}
	result := s.db.ORM.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&record)
	if result.Error != nil {
		return false, fmt.Errorf("insert merchant like: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (s *Store) RemoveMerchantLike(ctx context.Context, merchantID, userID, month string) (bool, error) {
	result := s.db.ORM.WithContext(ctx).
		Where("merchant_id = ? AND user_id = ? AND like_month = ?", merchantID, userID, month).
		Delete(&merchantLikeRecord{})
	if result.Error != nil {
		return false, fmt.Errorf("delete merchant like: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (s *Store) MerchantLikeCount(ctx context.Context, merchantID, month string) (int64, error) {
	var count int64
	if err := s.db.ORM.WithContext(ctx).
		Model(&merchantLikeRecord{}).
		Where("merchant_id = ? AND like_month = ?", merchantID, month).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count merchant likes: %w", err)
	}
	return count, nil
}

func (s *Store) MerchantLikeCounts(ctx context.Context, month string, ids []string) (map[string]int64, error) {
	out := make(map[string]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []merchantLikeCountRow
	if err := s.db.ORM.WithContext(ctx).
		Model(&merchantLikeRecord{}).
		Select("merchant_id, COUNT(*) AS like_count").
		Where("like_month = ? AND merchant_id IN ?", month, ids).
		Group("merchant_id").
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("count merchant likes in batch: %w", err)
	}
	for _, row := range rows {
		out[row.MerchantID] = row.LikeCount
	}
	return out, nil
}

func (s *Store) MerchantLikeUsers(ctx context.Context, userID, month string, ids []string) (map[string]bool, error) {
	out := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var records []merchantLikeRecord
	if err := s.db.ORM.WithContext(ctx).
		Model(&merchantLikeRecord{}).
		Select("merchant_id").
		Where("user_id = ? AND like_month = ? AND merchant_id IN ?", userID, month, ids).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list merchant likes for user: %w", err)
	}
	for _, record := range records {
		out[record.MerchantID] = true
	}
	return out, nil
}

// MerchantLikeStats returns the current user's monthly and lifetime like
// counts in one indexed range. The caller supplies the month bucket so the
// application clock remains the only source of "now".
func (s *Store) MerchantLikeStats(ctx context.Context, userID, month string) (repository.MerchantLikeTotals, error) {
	var totals repository.MerchantLikeTotals
	err := s.db.SQL().QueryRowContext(ctx,
		`SELECT COUNT(CASE WHEN like_month = ? THEN 1 END) AS monthly,
		        COUNT(*) AS lifetime
		 FROM merchant_like
		 WHERE user_id = ?`,
		month, userID).Scan(&totals.Monthly, &totals.Lifetime)
	if err != nil {
		return repository.MerchantLikeTotals{}, fmt.Errorf("count merchant likes for user: %w", err)
	}
	return totals, nil
}

// ListMerchantLikeCounts is the MySQL rebuild path. It filters by the first
// service_types value and treats unknown/legacy values as OTHER.
func (s *Store) ListMerchantLikeCounts(ctx context.Context, month, category string, limit int) ([]repository.MerchantLikeCount, error) {
	if limit <= 0 {
		limit = 50
	}
	category = strings.ToUpper(strings.TrimSpace(category))
	var rows []merchantLikeCountRow
	err := s.db.ORM.WithContext(ctx).
		Table("merchant_like AS l").
		Select("l.merchant_id, COUNT(*) AS like_count").
		Joins("JOIN merchant_profiles AS p ON p.id = l.merchant_id").
		Where("l.like_month = ? AND p.is_archived = ? AND p.business_status <> ?", month, false, "CLOSED").
		Where("(CASE WHEN UPPER(JSON_UNQUOTE(JSON_EXTRACT(p.service_types, '$[0]'))) IN ('FOOD','SUPPLIES','OTHER') THEN UPPER(JSON_UNQUOTE(JSON_EXTRACT(p.service_types, '$[0]'))) ELSE 'OTHER' END) = ?", category).
		Group("l.merchant_id").
		Order("like_count DESC").
		Order("l.merchant_id ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list merchant like counts: %w", err)
	}
	result := make([]repository.MerchantLikeCount, 0, len(rows))
	for _, row := range rows {
		result = append(result, repository.MerchantLikeCount{MerchantID: row.MerchantID, LikeCount: row.LikeCount})
	}
	return result, nil
}
