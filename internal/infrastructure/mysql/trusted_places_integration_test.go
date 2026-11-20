package mysqlstore

import (
	"cityflow/internal/application/repository"
	"cityflow/internal/domain/location"
	"context"
	"os"
	"testing"
	"time"
)

func TestTrustedPlacesMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set CITYFLOW_TEST_MYSQL_DSN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM trusted_places WHERE poi_id=?", id)
	now := time.Now().UTC().Truncate(time.Millisecond)
	place := repository.TrustedPlace{Place: location.Place{ID: id, Name: "真实地点", Address: "桂林市", Location: location.Point{Latitude: 25.2, Longitude: 110.3}}, VerifiedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err = s.SaveTrustedPlaces(ctx, []repository.TrustedPlace{place}); err != nil {
		t.Fatal(err)
	}
	result, err := s.FindTrustedPlaces(ctx, []string{id, "untrusted"}, now)
	if err != nil || len(result) != 1 || result[id].Place.Name != place.Place.Name {
		t.Fatalf("lookup: %+v %v", result, err)
	}
	changed := place
	changed.Place.Name = "新名称"
	changed.ExpiresAt = now.Add(2 * time.Minute)
	if err = s.SaveTrustedPlaces(ctx, []repository.TrustedPlace{changed}); err != nil {
		t.Fatal(err)
	}
	result, err = s.FindTrustedPlaces(ctx, []string{id}, now)
	if err != nil || result[id].Place.Name != place.Place.Name || !result[id].ExpiresAt.Equal(place.ExpiresAt) {
		t.Fatalf("valid existing record was changed: %+v %v", result, err)
	}
	result, err = s.FindTrustedPlaces(ctx, []string{id}, now.Add(2*time.Minute))
	if err != nil || len(result) != 0 {
		t.Fatalf("expired: %+v %v", result, err)
	}
	if err = s.orm.Model(&trustedPlaceRecord{}).Where("poi_id = ?", id).Update("expires_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.SaveTrustedPlaces(ctx, []repository.TrustedPlace{changed}); err != nil {
		t.Fatal(err)
	}
	result, err = s.FindTrustedPlaces(ctx, []string{id}, now)
	if err != nil || result[id].Place.Name != changed.Place.Name {
		t.Fatalf("expired record not renewed: %+v %v", result, err)
	}
}
