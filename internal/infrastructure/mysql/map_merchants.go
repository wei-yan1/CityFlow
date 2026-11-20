package mysqlstore

import (
	"cityflow/internal/domain/location"
	"context"
)

func (s *Store) MapMerchants(ctx context.Context, b location.Bounds, limit int) ([]location.Place, error) {
	var records []merchantRecord
	err := activeMerchantQuery(s.orm.WithContext(ctx)).Where("merchant_profiles.business_status = ? AND longitude >= ? AND longitude < ? AND latitude >= ? AND latitude < ?", "OPEN", b.West, b.East, b.South, b.North).Order("merchant_profiles.id").Limit(limit).Find(&records).Error
	result := make([]location.Place, 0, len(records))
	for _, r := range records {
		result = append(result, location.Place{ID: r.ID, Name: r.Name, Address: r.Address, Category: "MERCHANT", Location: location.Point{Latitude: r.Latitude, Longitude: r.Longitude}})
	}
	return result, err
}
