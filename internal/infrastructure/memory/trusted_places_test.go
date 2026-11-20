package memory

import (
	"cityflow/internal/application/repository"
	"cityflow/internal/domain/location"
	"context"
	"testing"
	"time"
)

func TestTrustedPlacePreservesValidAndRenewsExpired(t *testing.T) {
	ctx := context.Background()
	s := NewTrustedPlaceStore()
	now := time.Now().UTC()
	original := repository.TrustedPlace{Place: location.Place{ID: "one", Name: "原地点"}, VerifiedAt: now, ExpiresAt: now.Add(time.Minute)}
	if err := s.SaveTrustedPlaces(ctx, []repository.TrustedPlace{original}); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Place.Name = "新地点"
	changed.ExpiresAt = now.Add(2 * time.Minute)
	if err := s.SaveTrustedPlaces(ctx, []repository.TrustedPlace{changed}); err != nil {
		t.Fatal(err)
	}
	got, err := s.FindTrustedPlaces(ctx, []string{"one"}, now)
	if err != nil || got["one"].Place.Name != "原地点" || !got["one"].ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatalf("existing: %+v %v", got, err)
	}
	s.values["one"] = repository.TrustedPlace{Place: original.Place, ExpiresAt: now.Add(-time.Second)}
	if err := s.SaveTrustedPlaces(ctx, []repository.TrustedPlace{changed}); err != nil {
		t.Fatal(err)
	}
	got, err = s.FindTrustedPlaces(ctx, []string{"one"}, now)
	if err != nil || got["one"].Place.Name != "新地点" {
		t.Fatalf("renewal: %+v %v", got, err)
	}
}
