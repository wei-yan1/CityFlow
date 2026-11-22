package reviewservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"cityflow/app/discovery/domain/comment"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/domain"
)

// CommentService applies the same comment rules for every review type:
// three levels, no physical delete, label snapshots and cursor pagination.
type CommentService struct {
	Repo           repository.ReviewComments
	ForceAnonymous bool
	// AnonymousLabel is the fixed public label for a domain whose policy is
	// "always this exact anonymous label" (the rider page uses 匿名用户).
	// When empty, a per-comment random label is generated.
	AnonymousLabel string
	// ReactionTarget is PLACE_COMMENT or RIDER_COMMENT. Reactions is optional:
	// when Reaction is down, comments remain readable with zero like counts.
	ReactionTarget string
	Reactions      ReactionReader
	Now            func() time.Time
}

func NewCommentService(repo repository.ReviewComments, forceAnonymous bool) *CommentService {
	return &CommentService{Repo: repo, ForceAnonymous: forceAnonymous, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *CommentService) Create(ctx context.Context, reviewID, userID string, req comment.CreateRequest) (*comment.Public, error) {
	if s.Repo == nil {
		return nil, domain.ErrUnavailable
	}
	if strings.TrimSpace(reviewID) == "" {
		return nil, fmt.Errorf("%w: 评价不存在", domain.ErrInvalidRequest)
	}
	content := strings.TrimSpace(req.Content)
	if content == "" || !utf8.ValidString(content) || utf8.RuneCountInString(content) > comment.MaxLength {
		return nil, fmt.Errorf("%w: 评论需为 1 至 500 字", domain.ErrInvalidRequest)
	}
	exists, err := s.Repo.ReviewExists(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	anonymous := req.Anonymous || s.ForceAnonymous
	label := CommentAuthorLabel(anonymous, userID)
	if anonymous && s.AnonymousLabel != "" {
		label = s.AnonymousLabel
	}
	record := comment.Record{
		ID: randomCommentID(), ReviewID: reviewID, UserID: userID,
		Anonymous: anonymous, AuthorLabel: label,
		Content: content, Depth: 1, CreatedAt: s.now(),
	}
	if parentID := strings.TrimSpace(req.ParentID); parentID != "" {
		parent, err := s.Repo.GetComment(ctx, reviewID, parentID)
		if err != nil {
			return nil, err
		}
		if parent.Hidden {
			return nil, fmt.Errorf("%w: 父评论已隐藏", domain.ErrConflict)
		}
		if parent.Depth >= comment.MaxDepth {
			return nil, fmt.Errorf("%w: 回复最多三层", domain.ErrInvalidRequest)
		}
		record.ParentID = parent.ID
		record.RootID = parent.RootID
		if record.RootID == "" {
			record.RootID = parent.ID
		}
		record.Depth = parent.Depth + 1
		record.ReplyToUserID = parent.UserID
		record.ReplyToLabel = parent.AuthorLabel
	} else {
		record.RootID = record.ID
	}
	if err := s.Repo.CreateComment(ctx, record); err != nil {
		return nil, err
	}
	public := PublicComment(record)
	return &public, nil
}

func (s *CommentService) ListTop(ctx context.Context, reviewID, userID, sortBy, cursor string, limit int) (*comment.List, error) {
	if s.Repo == nil {
		return nil, domain.ErrUnavailable
	}
	exists, err := s.Repo.ReviewExists(ctx, reviewID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, domain.ErrNotFound
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > comment.MaxPage {
		limit = comment.MaxPage
	}
	sortBy = strings.ToLower(strings.TrimSpace(sortBy))
	if sortBy != comment.SortHot {
		sortBy = comment.SortLatest
	}
	items, next, err := s.Repo.ListTopComments(ctx, reviewID, sortBy, strings.TrimSpace(cursor), limit)
	if err != nil {
		return nil, err
	}
	out := make([]comment.Public, 0, len(items))
	for _, item := range items {
		out = append(out, PublicComment(item))
	}
	s.overlayReactions(ctx, out, userID)
	return &comment.List{Items: out, NextCursor: next}, nil
}

func (s *CommentService) Thread(ctx context.Context, reviewID, userID, rootID string, limit int) ([]comment.Public, error) {
	if s.Repo == nil {
		return nil, domain.ErrUnavailable
	}
	if limit <= 0 || limit > comment.ThreadPage {
		limit = comment.ThreadPage
	}
	items, err := s.Repo.ListCommentThread(ctx, reviewID, strings.TrimSpace(rootID), limit)
	if err != nil {
		return nil, err
	}
	out := make([]comment.Public, 0, len(items))
	for _, item := range items {
		out = append(out, PublicComment(item))
	}
	s.overlayReactions(ctx, out, userID)
	return out, nil
}

func (s *CommentService) Hide(ctx context.Context, reviewID, id string) error {
	if s.Repo == nil {
		return domain.ErrUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return domain.ErrNotFound
	}
	return s.Repo.HideComment(ctx, reviewID, id)
}

func (s *CommentService) Get(ctx context.Context, reviewID, id string) (*comment.Public, error) {
	record, err := s.Repo.GetComment(ctx, reviewID, id)
	if err != nil {
		return nil, err
	}
	public := PublicComment(record)
	return &public, nil
}

func PublicComment(record comment.Record) comment.Public {
	return comment.Public{
		ID: record.ID, AuthorLabel: record.AuthorLabel, Anonymous: record.Anonymous,
		Content: record.Content, Date: commentDate(record.CreatedAt),
		RootID: record.RootID, ParentID: record.ParentID, Depth: record.Depth,
		ReplyToLabel: record.ReplyToLabel, ReplyCount: record.ReplyCount,
		Likes: record.LikeCount,
	}
}

// overlayReactions fills only the per-user liked flag. Like counts come from
// the comment row's like_count projection, which is maintained together with
// the like-count projection; a Reaction outage therefore degrades the flag, not the
// count.
func (s *CommentService) overlayReactions(ctx context.Context, items []comment.Public, userID string) {
	if s.Reactions == nil || s.ReactionTarget == "" || len(items) == 0 || strings.TrimSpace(userID) == "" {
		return
	}
	ids := make([]string, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	liked, err := s.Reactions.ReactionLiked(ctx, s.ReactionTarget, ids, "LIKE", userID)
	if err != nil {
		LogDegradation("read comment reaction flags", err)
		return
	}
	for i := range items {
		items[i].Liked = liked[items[i].ID]
	}
}

// CommentAuthorLabel is generated once at creation and stored as a snapshot.
// Anonymous labels must never be derived from user_id.
func CommentAuthorLabel(anonymous bool, userID string) string {
	if anonymous {
		return "匿名用户·" + randomCommentID()[:6]
	}
	sum := sha256.Sum256([]byte(userID))
	return "用户·" + hex.EncodeToString(sum[:])[:6]
}

func RandomCommentID() string { return randomCommentID() }

func randomCommentID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("%032x", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buffer[:])
}

func commentDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}

// EncodeCommentCursor makes the created_at/id cursor opaque to clients.
func EncodeCommentCursor(createdAt time.Time, id string) string {
	return comment.EncodeCursor(createdAt, id)
}

func DecodeCommentCursor(cursor string) (time.Time, string, error) {
	return comment.DecodeCursor(cursor)
}

func (s *CommentService) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now()
}

// LogDegradation is intentionally small: callers can record a projection
// failure without turning a comment read into a 5xx.
func LogDegradation(scope string, err error) {
	if err != nil {
		log.Printf("warning: %s: %v", scope, err)
	}
}
