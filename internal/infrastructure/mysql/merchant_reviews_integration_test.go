package mysqlstore

import (
	"cityflow/internal/domain"
	"cityflow/internal/domain/merchant"
	"cityflow/internal/domain/user"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlaceReviewsPersistAndProjectionMySQL(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated local MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.MigrateAllServices(ctx); e != nil {
		t.Fatal(e)
	}
	key, e := newUUID()
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateUser(ctx, user.CreateRequest{Email: key + "@example.test", Password: "test-password", Name: "review user", Role: "USER"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.orm.Exec("DELETE FROM users WHERE id=?", u.ID)
	id := strings.ReplaceAll(key, "-", "")
	r := merchant.Review{ID: id, PlaceSource: "amap", PlaceID: "test-place-" + id, UserID: u.ID, Rating: 4, Comment: "测试评价"}
	if e = s.CreatePlaceReview(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.ExecContext(ctx, "UPDATE place_reviews SET like_count=1 WHERE id=?", id); e != nil {
		t.Fatal(e)
	}
	again, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	items, n, avg, e := again.PlaceReviews(ctx, "amap", r.PlaceID, u.ID, 20, 0)
	if e != nil || n != 1 || avg != 4 || len(items) != 1 || items[0].Likes != 1 || items[0].Liked || !items[0].Mine {
		t.Fatalf("reviews=%+v n=%d avg=%v err=%v", items, n, avg, e)
	}
	if e = again.DeletePlaceReview(ctx, id, "00000000-0000-4000-8000-000000000099"); !errors.Is(e, domain.ErrForbidden) {
		t.Fatalf("wrong owner: %v", e)
	}
	if e = again.DeletePlaceReview(ctx, id, u.ID); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = again.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM place_reviews WHERE id=?", id).Scan(&count); e != nil || count != 0 {
		t.Fatalf("review after delete=%d err=%v", count, e)
	}
}
