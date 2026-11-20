package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/merchant"
)

type reviewAudit struct {
	ReviewID  string
	AdminID   string
	Action    string
	Reason    string
	CreatedAt time.Time
}

func (s *Store) UpdatePlaceReviewLikeCount(_ context.Context, id string, count int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviews[id] == nil {
		return nil
	}
	if count < 0 {
		count = 0
	}
	s.reviews[id].Likes = int(count)
	return nil
}

func (s *Store) CreatePlaceReview(_ context.Context, r merchant.Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reviews == nil {
		s.reviews = map[string]*merchant.Review{}
	}
	if s.reviews[r.ID] != nil {
		return domain.ErrConflict
	}
	if r.OrderID != "" {
		for _, existing := range s.reviews {
			if existing.OrderID == r.OrderID && existing.PlaceSource == r.PlaceSource && existing.PlaceID == r.PlaceID && existing.UserID == r.UserID {
				return domain.ErrConflict
			}
		}
	}
	r.CreatedAt = time.Now().UTC()
	s.reviews[r.ID] = &r
	return nil
}
func (s *Store) DeletePlaceReview(_ context.Context, id, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.reviews[id]
	if r == nil {
		return domain.ErrNotFound
	}
	if r.UserID != userID {
		return domain.ErrForbidden
	}
	delete(s.reviews, id)
	return nil
}
func (s *Store) PlaceReviews(_ context.Context, source, id, userID string, limit, offset int) ([]merchant.Review, int, float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]merchant.Review, 0)
	sum := 0
	for _, v := range s.reviews {
		if v.PlaceSource != source || v.PlaceID != id {
			continue
		}
		r := *v
		r.Mine = r.UserID == userID
		list = append(list, r)
		sum += r.Rating
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID > list[j].ID
		}
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	count := len(list)
	average := float64(0)
	if count > 0 {
		average = float64(sum) / float64(count)
	}
	if offset >= count {
		return []merchant.Review{}, count, average, nil
	}
	end := offset + limit
	if end > count {
		end = count
	}
	return list[offset:end], count, average, nil
}

func (s *Store) PublicPlaceReviews(ctx context.Context, source, id, userID string, limit, offset int) ([]merchant.Review, int, float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]merchant.Review, 0)
	sum := 0
	for _, v := range s.reviews {
		if v.PlaceSource != source || v.PlaceID != id {
			continue
		}
		r := *v
		r.OrderID = ""
		r.Mine = r.UserID == userID
		list = append(list, r)
		sum += r.Rating
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Likes == list[j].Likes {
			return list[i].ID > list[j].ID
		}
		return list[i].Likes > list[j].Likes
	})
	count := len(list)
	average := float64(0)
	if count > 0 {
		average = float64(sum) / float64(count)
	}
	if offset >= count {
		return []merchant.Review{}, count, average, nil
	}
	end := offset + limit
	if end > count {
		end = count
	}
	return list[offset:end], count, average, nil
}

func (s *Store) ListPlaceReviewsAdmin(_ context.Context, source, placeID string, limit, offset int) ([]merchant.Review, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]merchant.Review, 0)
	for _, v := range s.reviews {
		if source != "" && v.PlaceSource != source {
			continue
		}
		if placeID != "" && v.PlaceID != placeID {
			continue
		}
		list = append(list, *v)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return list[i].ID > list[j].ID
		}
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	count := len(list)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= count {
		return []merchant.Review{}, count, nil
	}
	end := offset + limit
	if end > count {
		end = count
	}
	return list[offset:end], count, nil
}

func (s *Store) RevealPlaceReviewIdentity(_ context.Context, reviewID, adminID, reason string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	review := s.reviews[reviewID]
	if review == nil {
		return "", domain.ErrNotFound
	}
	now := time.Now().UTC()
	recent := 0
	for _, audit := range s.reviewAudits {
		if audit.AdminID == adminID && audit.Action == "REVEAL" && audit.CreatedAt.After(now.Add(-time.Hour)) {
			recent++
		}
	}
	if recent >= 20 {
		return "", fmt.Errorf("%w: 每小时最多查看 20 条评价的真实身份", domain.ErrConflict)
	}
	s.reviewAudits = append(s.reviewAudits, reviewAudit{
		ReviewID: reviewID, AdminID: adminID, Action: "REVEAL", Reason: reason, CreatedAt: now,
	})
	return review.UserID, nil
}
