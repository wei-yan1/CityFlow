package feedbackservice

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"cityflow/app/task/domain/feedback"
)

func TestFeedbackForcesAnonymous(t *testing.T) {
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
