package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cityflow/internal/domain/feedback"
	"cityflow/internal/domain/merchant"
)

func TestReviewAuthorLabelIsStableForRealNameAndUniqueForAnonymous(t *testing.T) {
	first := reviewAuthorLabel(false, "user-a")
	second := reviewAuthorLabel(false, "user-a")
	other := reviewAuthorLabel(false, "user-b")
	if first != second || first == other {
		t.Fatalf("stable labels: %q %q %q", first, second, other)
	}
	if !strings.HasPrefix(first, "用户·") || len(first) != len("用户·")+6 {
		t.Fatalf("unexpected real-name label %q", first)
	}
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		label := reviewAuthorLabel(true, "user-a")
		if !strings.HasPrefix(label, "匿名用户·") || len(label) != len("匿名用户·")+6 {
			t.Fatalf("unexpected anonymous label %q", label)
		}
		seen[label] = true
	}
	if len(seen) < 9 {
		t.Fatalf("anonymous labels should be per-review random, got %d unique", len(seen))
	}
}

func TestPublicReviewJSONContainsNoIdentityOrOrderLink(t *testing.T) {
	review := merchant.Review{
		ID: "review-1", OrderID: "order-secret", UserID: "user-secret",
		Anonymous: true, AuthorLabel: "匿名用户·abc123", Rating: 4, Comment: "很好",
		CreatedAt: time.Date(2026, 11, 19, 12, 34, 56, 0, time.UTC), Likes: 3, Mine: true,
	}
	body, err := json.Marshal((MerchantReviewService{}).publicReview(review))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"user_id", "order_id", "created_at", "order-secret", "user-secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public review leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(string(body), "匿名用户·abc123") || !strings.Contains(string(body), `"date":"2026-11-19"`) {
		t.Fatalf("public review missing label/date: %s", body)
	}
}

func TestPublicRiderFeedbackForcesAnonymous(t *testing.T) {
	item := feedback.Record{
		ID: "fb-1", RiderID: "rider-secret", TaskID: "task-secret", UserID: "user-secret",
		Kind: feedback.KindReview, Rating: 5, Comment: "很快",
		CreatedAt: time.Date(2026, 11, 19, 12, 34, 56, 0, time.UTC),
	}
	body, err := json.Marshal(publicRiderFeedback(item))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"rider_id", "task_id", "user_id", "created_at", "rider-secret", "task-secret", "user-secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("public rider feedback leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(string(body), `"author_label":"匿名用户"`) || !strings.Contains(string(body), `"date":"2026-11-19"`) {
		t.Fatalf("public rider feedback missing anonymous label/date: %s", body)
	}
}

func TestMaskUserID(t *testing.T) {
	if got := maskUserID("00000000-0000-4000-8000-000000000001"); got != "0000****0001" {
		t.Fatalf("mask = %q", got)
	}
	if got := maskUserID("short"); got != "****" {
		t.Fatalf("short mask = %q", got)
	}
}
