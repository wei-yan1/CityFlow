package memory

import (
	"context"
	"sort"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/feedback"
)

func (s *Store) CreateRiderFeedback(_ context.Context, record feedback.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.RiderID == "" || record.TaskID == "" || record.UserID == "" || record.ID == "" {
		return domain.ErrInvalidRequest
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	s.feedback = append(s.feedback, cloneFeedback(record))
	return nil
}

func (s *Store) ListRiderFeedback(_ context.Context, riderID string, kinds []string, limit, offset int) ([]feedback.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	kindSet := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	items := make([]feedback.Record, 0)
	for _, item := range s.feedback {
		if item.RiderID != riderID || (len(kindSet) > 0 && !kindSet[item.Kind]) {
			continue
		}
		items = append(items, cloneFeedback(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID > items[j].ID)
	})
	return pageFeedback(items, limit, offset), nil
}

func (s *Store) ListRiderReports(_ context.Context, status string, page repository.Page) ([]feedback.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]feedback.Record, 0)
	for _, item := range s.feedback {
		if item.Kind != feedback.KindReport || (status != "" && item.Status != status) {
			continue
		}
		items = append(items, cloneFeedback(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID > items[j].ID)
	})
	return pageFeedback(items, page.Limit, page.Offset), nil
}

func (s *Store) ResolveRiderReport(_ context.Context, id, adminID, status, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.feedback {
		if s.feedback[index].ID != id || s.feedback[index].Kind != feedback.KindReport {
			continue
		}
		now := time.Now().UTC()
		s.feedback[index].Status = status
		s.feedback[index].ResolutionNote = note
		s.feedback[index].ResolvedBy = adminID
		s.feedback[index].ResolvedAt = &now
		s.feedback[index].UpdatedAt = now
		return nil
	}
	return domain.ErrNotFound
}

func cloneFeedback(item feedback.Record) feedback.Record {
	copy := item
	copy.Images = append([]string(nil), item.Images...)
	return copy
}

func pageFeedback(items []feedback.Record, limit, offset int) []feedback.Record {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []feedback.Record{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
