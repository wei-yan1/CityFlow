package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cityflow/internal/domain"
	"cityflow/internal/domain/comment"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/infrastructure/memory"
)

func newCommentFixture(t *testing.T, force bool, label string) (*CommentService, *memory.Store, string) {
	t.Helper()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	reviewID := "0123456789abcdef0123456789abcdef"
	if err := store.CreatePlaceReview(context.Background(), merchant.Review{
		ID: reviewID, PlaceSource: "merchant", PlaceID: "place-1",
		UserID: "author-1", Rating: 5, Comment: "很好",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewCommentService(memory.PlaceComments{Store: store}, force)
	svc.AnonymousLabel = label
	return svc, store, reviewID
}

func TestCommentAnonymousLabelsUseExistingRules(t *testing.T) {
	svc, _, reviewID := newCommentFixture(t, false, "")
	first, err := svc.Create(context.Background(), reviewID, "user-1", comment.CreateRequest{Content: "匿名一", Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first.AuthorLabel, "匿名用户·") || len(first.AuthorLabel) != len("匿名用户·")+6 {
		t.Fatalf("anonymous label = %q", first.AuthorLabel)
	}
	if strings.Contains(first.AuthorLabel, "user-1") {
		t.Fatal("anonymous label leaked user_id")
	}
	realA, _ := svc.Create(context.Background(), reviewID, "user-2", comment.CreateRequest{Content: "实名一"})
	realB, _ := svc.Create(context.Background(), reviewID, "user-2", comment.CreateRequest{Content: "实名二"})
	if realA.AuthorLabel != realB.AuthorLabel || !strings.HasPrefix(realA.AuthorLabel, "用户·") {
		t.Fatalf("stable labels = %q %q", realA.AuthorLabel, realB.AuthorLabel)
	}
}

func TestRiderCommentKeepsExistingAnonymousLabel(t *testing.T) {
	svc, _, reviewID := newCommentFixture(t, true, "匿名用户")
	out, err := svc.Create(context.Background(), reviewID, "user-1", comment.CreateRequest{Content: "骑手评价", Anonymous: false})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Anonymous || out.AuthorLabel != "匿名用户" {
		t.Fatalf("rider comment = %+v", out)
	}
}

func TestCommentReplyDepthAndHideCascade(t *testing.T) {
	svc, _, reviewID := newCommentFixture(t, false, "")
	root, err := svc.Create(context.Background(), reviewID, "user-1", comment.CreateRequest{Content: "根评论"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Create(context.Background(), reviewID, "user-2", comment.CreateRequest{Content: "第二层", ParentID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if second.Depth != 2 || second.RootID != root.ID || second.ReplyToLabel != root.AuthorLabel {
		t.Fatalf("second = %+v", second)
	}
	third, err := svc.Create(context.Background(), reviewID, "user-3", comment.CreateRequest{Content: "第三层", ParentID: second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if third.Depth != 3 {
		t.Fatalf("third depth = %d", third.Depth)
	}
	if _, err := svc.Create(context.Background(), reviewID, "user-4", comment.CreateRequest{Content: "第四层", ParentID: third.ID}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("fourth level err = %v", err)
	}
	if err := svc.Hide(context.Background(), reviewID, root.ID); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Thread(context.Background(), reviewID, "", root.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("thread after hide = %+v", items)
	}
}

func TestCommentCursorPagination(t *testing.T) {
	svc, _, reviewID := newCommentFixture(t, false, "")
	base := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	counter := 0
	svc.Now = func() time.Time {
		counter++
		return base.Add(time.Duration(counter) * time.Second)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.Create(context.Background(), reviewID, "user-1", comment.CreateRequest{Content: "评论"}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := svc.ListTop(context.Background(), reviewID, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("page = %+v", first)
	}
	second, err := svc.ListTop(context.Background(), reviewID, "", first.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second page = %+v first = %+v", second, first)
	}
}

type fixedCommentReactions struct {
	states map[string]ReactionState
}

func (f fixedCommentReactions) ReactionStates(context.Context, string, []string, string, string) (map[string]ReactionState, error) {
	return f.states, nil
}

func TestCommentLikesAreOverlaidWithoutIdentity(t *testing.T) {
	svc, _, reviewID := newCommentFixture(t, false, "")
	root, err := svc.Create(context.Background(), reviewID, "user-1", comment.CreateRequest{Content: "根评论"})
	if err != nil {
		t.Fatal(err)
	}
	svc.ReactionTarget = "PLACE_COMMENT"
	svc.Reactions = fixedCommentReactions{states: map[string]ReactionState{
		root.ID: {Count: 7, Active: true},
	}}
	list, err := svc.ListTop(context.Background(), reviewID, "user-2", "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Likes != 7 || !list.Items[0].Liked {
		t.Fatalf("comment reaction overlay = %+v", list.Items)
	}
	thread, err := svc.Thread(context.Background(), reviewID, "user-2", root.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread) != 1 || thread[0].Likes != 7 || !thread[0].Liked {
		t.Fatalf("thread reaction overlay = %+v", thread)
	}
}
