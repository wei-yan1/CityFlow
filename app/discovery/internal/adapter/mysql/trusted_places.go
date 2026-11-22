package discoverymysql

import (
	"context"
	"time"

	"gorm.io/gorm/clause"

	"cityflow/app/discovery/repository"
	"cityflow/internal/common/location"
)

func (s *Store) SaveTrustedPlaces(ctx context.Context, places []repository.TrustedPlace) error {
	if len(places) == 0 {
		return nil
	}
	records := make([]trustedPlaceRecord, 0, len(places))
	ids := make([]string, 0, len(places))
	for _, p := range places {
		ids = append(ids, p.Place.ID)
		records = append(records, trustedPlaceRecord{POIID: p.Place.ID, Name: p.Place.Name, Address: p.Place.Address, Latitude: p.Place.Location.Latitude, Longitude: p.Place.Location.Longitude, VerifiedAt: p.VerifiedAt, ExpiresAt: p.ExpiresAt})
	}
	if err := s.db.ORM.WithContext(ctx).Where("poi_id IN ? AND expires_at <= ?", ids, time.Now().UTC()).Delete(&trustedPlaceRecord{}).Error; err != nil {
		return err
	}
	// A concurrent request may insert the same POI after the service's lookup.
	// Keep its record instead of overwriting it or extending its validity.
	return s.db.ORM.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "poi_id"}}, DoNothing: true}).CreateInBatches(records, 100).Error
}

func (s *Store) FindTrustedPlaces(ctx context.Context, ids []string, now time.Time) (map[string]repository.TrustedPlace, error) {
	result := make(map[string]repository.TrustedPlace, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var rows []trustedPlaceRecord
	if err := s.db.ORM.WithContext(ctx).Where("poi_id IN ? AND expires_at > ?", ids, now).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, p := range rows {
		result[p.POIID] = repository.TrustedPlace{Place: location.Place{ID: p.POIID, Name: p.Name, Address: p.Address, Category: "POI", Location: location.Point{Latitude: p.Latitude, Longitude: p.Longitude}}, VerifiedAt: p.VerifiedAt, ExpiresAt: p.ExpiresAt}
	}
	return result, nil
}

// PurgeTrustedPlaces deletes expired verification rows independently of user
// search traffic. SaveTrustedPlaces keeps its targeted delete so a POI that is
// searched again can be re-verified without waiting for this job.
func (s *Store) PurgeTrustedPlaces(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	result := s.db.ORM.WithContext(ctx).Exec(
		"DELETE FROM trusted_places WHERE expires_at <= ? LIMIT ?",
		before, limit,
	)
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
