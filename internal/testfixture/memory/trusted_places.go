package memory

import (
	discoveryrepo "cityflow/app/discovery/repository"
	"context"
	"sync"
	"time"
)

// TrustedPlaceStore is the disposable, process-local equivalent used without MySQL.
type TrustedPlaceStore struct {
	mu     sync.RWMutex
	values map[string]discoveryrepo.TrustedPlace
}

func NewTrustedPlaceStore() *TrustedPlaceStore {
	return &TrustedPlaceStore{values: make(map[string]discoveryrepo.TrustedPlace)}
}
func (s *TrustedPlaceStore) SaveTrustedPlaces(_ context.Context, items []discoveryrepo.TrustedPlace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.values {
		if !p.ExpiresAt.After(time.Now().UTC()) {
			delete(s.values, id)
		}
	}
	for _, p := range items {
		if _, exists := s.values[p.Place.ID]; !exists {
			s.values[p.Place.ID] = p
		}
	}
	return nil
}
func (s *TrustedPlaceStore) FindTrustedPlaces(_ context.Context, ids []string, now time.Time) (map[string]discoveryrepo.TrustedPlace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]discoveryrepo.TrustedPlace)
	for _, id := range ids {
		if p, ok := s.values[id]; ok && p.ExpiresAt.After(now) {
			result[id] = p
		}
	}
	return result, nil
}
