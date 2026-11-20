package mysqlstore

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cityflow/internal/domain/comment"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/reaction"
	"cityflow/internal/domain/user"
)

func openTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated local MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func TestPlaceCommentsMySQL(t *testing.T) {
	s, ctx := openTestStore(t)
	key, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(ctx, user.CreateRequest{Email: key + "@example.test", Password: "test-password", Name: "comment user", Role: "USER"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM users WHERE id=?", u.ID)
	reviewID := strings.ReplaceAll(key, "-", "")
	r := merchant.Review{ID: reviewID, PlaceSource: "amap", PlaceID: "comment-place-" + reviewID, UserID: u.ID, Rating: 5, Comment: "评价"}
	if err = s.CreatePlaceReview(ctx, r); err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM place_review_comments WHERE review_id=?", reviewID)
	defer s.orm.Exec("DELETE FROM place_reviews WHERE id=?", reviewID)

	repo := PlaceComments{Store: s}
	rootID := strings.ReplaceAll(key, "-", "")
	created := time.Now().UTC().Add(-time.Minute)
	root := comment.Record{
		ID: rootID, ReviewID: reviewID, UserID: u.ID, Anonymous: true,
		AuthorLabel: "匿名用户·abcdef", RootID: rootID, Depth: 1,
		Content: "根评论", CreatedAt: created,
	}
	if err = repo.CreateComment(ctx, root); err != nil {
		t.Fatal(err)
	}
	replyKey, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	replyID := strings.ReplaceAll(replyKey, "-", "")
	reply := comment.Record{
		ID: replyID, ReviewID: reviewID, UserID: u.ID, Anonymous: true,
		AuthorLabel: "匿名用户·123456", RootID: rootID, ParentID: rootID, Depth: 2,
		Content: "回复", CreatedAt: created.Add(time.Second),
		ReplyToUserID: u.ID, ReplyToLabel: "匿名用户·abcdef",
	}
	if err = repo.CreateComment(ctx, reply); err != nil {
		t.Fatal(err)
	}

	top, _, err := repo.ListTopComments(ctx, reviewID, "", 20)
	if err != nil || len(top) != 1 || top[0].ReplyCount != 1 {
		t.Fatalf("top = %+v err=%v", top, err)
	}
	thread, err := repo.ListCommentThread(ctx, reviewID, rootID, 100)
	if err != nil || len(thread) != 2 || thread[1].ReplyToLabel != "匿名用户·abcdef" {
		t.Fatalf("thread = %+v err=%v", thread, err)
	}
	if err = repo.HideComment(ctx, reviewID, rootID); err != nil {
		t.Fatal(err)
	}
	if err = repo.HideComment(ctx, reviewID, rootID); err != nil {
		t.Fatalf("second hide must be idempotent: %v", err)
	}
	thread, err = repo.ListCommentThread(ctx, reviewID, rootID, 100)
	if err != nil || len(thread) != 0 {
		t.Fatalf("hidden thread = %+v err=%v", thread, err)
	}
	if err = repo.HideComment(ctx, reviewID, "missing"); err == nil {
		t.Fatal("missing comment should be NotFound")
	}
}

func TestReactionStoreApplyAndListMySQL(t *testing.T) {
	s, ctx := openTestStore(t)
	store := ReactionStore{Store: s}
	key, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	target := "reaction-" + strings.ReplaceAll(key, "-", "")
	userA := key
	userB, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM reaction_records WHERE target_id=?", target)

	if err = store.Apply(ctx, []reaction.Op{
		{TargetType: reaction.TargetPlaceReview, TargetID: target, UserID: userA, Kind: reaction.KindLike, Active: true},
		{TargetType: reaction.TargetPlaceReview, TargetID: target, UserID: userB, Kind: reaction.KindLike, Active: true},
	}); err != nil {
		t.Fatal(err)
	}
	count, err := store.Count(ctx, reaction.TargetPlaceReview, target, reaction.KindLike)
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	records, next, err := store.ListMine(ctx, userA, reaction.KindLike, reaction.TargetPlaceReview, "", 20)
	if err != nil || len(records) != 1 || next != "" {
		t.Fatalf("mine=%+v next=%q err=%v", records, next, err)
	}
	// This exercises the tuple DELETE path; a malformed placeholder count
	// would fail here rather than silently leaving a row behind.
	if err = store.Apply(ctx, []reaction.Op{
		{TargetType: reaction.TargetPlaceReview, TargetID: target, UserID: userA, Kind: reaction.KindLike, Active: false},
	}); err != nil {
		t.Fatal(err)
	}
	count, err = store.Count(ctx, reaction.TargetPlaceReview, target, reaction.KindLike)
	if err != nil || count != 1 {
		t.Fatalf("after unlike count=%d err=%v", count, err)
	}
	records, err = store.Rebuild(ctx, reaction.TargetPlaceReview)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		if record.TargetID == target && record.UserID == userB {
			found = true
		}
	}
	if !found {
		t.Fatalf("rebuild did not return remaining user: %+v", records)
	}
}
