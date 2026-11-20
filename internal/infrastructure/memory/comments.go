package memory

import (
	"context"
	"sort"
	"strings"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/comment"
)

type PlaceComments struct{ Store *Store }
type RiderComments struct{ Store *Store }

var _ repository.ReviewComments = PlaceComments{}
var _ repository.ReviewComments = RiderComments{}

func (p PlaceComments) ReviewExists(ctx context.Context, id string) (bool, error) {
	return p.Store.targetExists(ctx, "place:"+id)
}

func (p PlaceComments) CreateComment(ctx context.Context, item comment.Record) error {
	return p.Store.createComment(ctx, "place", item)
}
func (p PlaceComments) GetComment(ctx context.Context, reviewID, id string) (comment.Record, error) {
	return p.Store.getComment(ctx, "place:"+reviewID, id)
}
func (p PlaceComments) ListTopComments(ctx context.Context, reviewID, cursor string, limit int) ([]comment.Record, string, error) {
	return p.Store.listTopComments(ctx, "place:"+reviewID, cursor, limit)
}
func (p PlaceComments) ListCommentThread(ctx context.Context, reviewID, rootID string, limit int) ([]comment.Record, error) {
	return p.Store.listCommentThread(ctx, "place:"+reviewID, rootID, limit)
}
func (p PlaceComments) HideComment(ctx context.Context, reviewID, id string) error {
	return p.Store.hideComment(ctx, "place:"+reviewID, id)
}

func (r RiderComments) ReviewExists(ctx context.Context, id string) (bool, error) {
	return r.Store.targetExists(ctx, "rider:"+id)
}
func (r RiderComments) CreateComment(ctx context.Context, item comment.Record) error {
	return r.Store.createComment(ctx, "rider", item)
}
func (r RiderComments) GetComment(ctx context.Context, feedbackID, id string) (comment.Record, error) {
	return r.Store.getComment(ctx, "rider:"+feedbackID, id)
}
func (r RiderComments) ListTopComments(ctx context.Context, feedbackID, cursor string, limit int) ([]comment.Record, string, error) {
	return r.Store.listTopComments(ctx, "rider:"+feedbackID, cursor, limit)
}
func (r RiderComments) ListCommentThread(ctx context.Context, feedbackID, rootID string, limit int) ([]comment.Record, error) {
	return r.Store.listCommentThread(ctx, "rider:"+feedbackID, rootID, limit)
}
func (r RiderComments) HideComment(ctx context.Context, feedbackID, id string) error {
	return r.Store.hideComment(ctx, "rider:"+feedbackID, id)
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

func (s *Store) listTopComments(_ context.Context, key, cursor string, limit int) ([]comment.Record, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > comment.MaxPage {
		limit = 20
	}
	items := make([]comment.Record, 0)
	for _, item := range s.comments[key] {
		if item.ParentID != "" || item.Hidden {
			continue
		}
		if cursor != "" {
			at, cursorID, err := comment.DecodeCursor(cursor)
			if err != nil {
				return nil, "", err
			}
			if !item.CreatedAt.Before(at) && !(item.CreatedAt.Equal(at) && item.ID < cursorID) {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt) || (items[i].CreatedAt.Equal(items[j].CreatedAt) && items[i].ID > items[j].ID)
	})
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = comment.EncodeCursor(last.CreatedAt, last.ID)
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
