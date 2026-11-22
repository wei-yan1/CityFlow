package memory

import (
	"context"
	"sort"
	"strings"

	"cityflow/app/discovery/domain/comment"
	discoveryrepo "cityflow/app/discovery/repository"
	"cityflow/internal/common/domain"
)

type PlaceComments struct{ Store *Store }

var _ discoveryrepo.ReviewComments = PlaceComments{}

func (p PlaceComments) ReviewExists(ctx context.Context, id string) (bool, error) {
	return p.Store.targetExists(ctx, "place:"+id)
}

func (p PlaceComments) CreateComment(ctx context.Context, item comment.Record) error {
	return p.Store.createComment(ctx, "place", item)
}
func (p PlaceComments) GetComment(ctx context.Context, reviewID, id string) (comment.Record, error) {
	return p.Store.getComment(ctx, "place:"+reviewID, id)
}
func (p PlaceComments) ListTopComments(ctx context.Context, reviewID, sortBy, cursor string, limit int) ([]comment.Record, string, error) {
	return p.Store.listTopComments(ctx, "place:"+reviewID, sortBy, cursor, limit)
}
func (p PlaceComments) ListCommentThread(ctx context.Context, reviewID, rootID string, limit int) ([]comment.Record, error) {
	return p.Store.listCommentThread(ctx, "place:"+reviewID, rootID, limit)
}
func (p PlaceComments) HideComment(ctx context.Context, reviewID, id string) error {
	return p.Store.hideComment(ctx, "place:"+reviewID, id)
}

func (s *Store) targetExists(_ context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.HasPrefix(key, "place:") {
		return s.reviews[strings.TrimPrefix(key, "place:")] != nil, nil
	}
	id := strings.TrimPrefix(key, "rider:")
	for _, item := range s.feedback {
		if item.ID == id && item.Kind == "REVIEW" {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) createComment(_ context.Context, target string, item comment.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.comments == nil {
		s.comments = map[string][]comment.Record{}
	}
	if target == "rider" {
		item.Anonymous = true
	}
	s.comments[target+":"+item.ReviewID] = append(s.comments[target+":"+item.ReviewID], item)
	return nil
}

func (s *Store) getComment(_ context.Context, key, id string) (comment.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.comments[key] {
		if item.ID == id {
			return item, nil
		}
	}
	return comment.Record{}, domain.ErrNotFound
}

func (s *Store) listTopComments(_ context.Context, key, sortBy, cursor string, limit int) ([]comment.Record, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > comment.MaxPage {
		limit = 20
	}
	if sortBy != comment.SortHot {
		sortBy = comment.SortLatest
	}
	items := make([]comment.Record, 0)
	for _, item := range s.comments[key] {
		if item.ParentID != "" || item.Hidden {
			continue
		}
		if cursor != "" {
			if sortBy == comment.SortHot {
				likeCount, at, cursorID, err := comment.DecodeHotCursor(cursor)
				if err != nil {
					return nil, "", err
				}
				if !(item.LikeCount < likeCount ||
					(item.LikeCount == likeCount && (item.CreatedAt.Before(at) ||
						(item.CreatedAt.Equal(at) && item.ID < cursorID)))) {
					continue
				}
			} else {
				at, cursorID, err := comment.DecodeCursor(cursor)
				if err != nil {
					return nil, "", err
				}
				if !item.CreatedAt.Before(at) && !(item.CreatedAt.Equal(at) && item.ID < cursorID) {
					continue
				}
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if sortBy == comment.SortHot && items[i].LikeCount != items[j].LikeCount {
			return items[i].LikeCount > items[j].LikeCount
		}
		return items[i].CreatedAt.After(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID > items[j].ID)
	})
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		if sortBy == comment.SortHot {
			next = comment.EncodeHotCursor(last.LikeCount, last.CreatedAt, last.ID)
		} else {
			next = comment.EncodeCursor(last.CreatedAt, last.ID)
		}
	}
	for i := range items {
		items[i].ReplyCount = 0
		for _, child := range s.comments[key] {
			if child.RootID == items[i].ID && !child.Hidden {
				items[i].ReplyCount++
			}
		}
		if items[i].ReplyCount > 0 {
			items[i].ReplyCount--
		}
	}
	return items, next, nil
}

func (s *Store) listCommentThread(_ context.Context, key, rootID string, limit int) ([]comment.Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > comment.ThreadPage {
		limit = comment.ThreadPage
	}
	items := make([]comment.Record, 0)
	for _, item := range s.comments[key] {
		if item.RootID != rootID || item.Hidden {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID < items[j].ID)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) SetCommentLikeCount(commentID string, count int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, items := range s.comments {
		for i := range items {
			if items[i].ID != commentID {
				continue
			}
			items[i].LikeCount = count
			s.comments[key] = items
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) hideComment(_ context.Context, key, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := s.comments[key]
	index := -1
	for i := range items {
		if items[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return domain.ErrNotFound
	}
	items[index].Hidden = true
	for i := range items {
		if items[i].ParentID == id {
			items[i].Hidden = true
			for j := range items {
				if items[j].ParentID == items[i].ID {
					items[j].Hidden = true
				}
			}
		}
	}
	s.comments[key] = items
	return nil
}
