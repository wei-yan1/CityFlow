package service

import (
	"context"
	"testing"
)

type fixedRiderReactions struct {
	states map[string]ReactionState
}

func (f fixedRiderReactions) ReactionStates(context.Context, string, []string, string, string) (map[string]ReactionState, error) {
	return f.states, nil
}

func TestRiderReviewReactionOverlay(t *testing.T) {
	svc := &RiderFeedbackService{
		ReactionTarget: "RIDER_REVIEW",
		Reactions: fixedRiderReactions{states: map[string]ReactionState{
			"r1": {Count: 4, Active: true},
		}},
	}
	reviews := []PublicRiderFeedback{{ID: "r1"}, {ID: "r2"}}
	svc.overlayReviewReactions(context.Background(), reviews, "user-1")
	if reviews[0].Likes != 4 || !reviews[0].Liked {
		t.Fatalf("r1 = %+v", reviews[0])
	}
	if reviews[1].Likes != 0 || reviews[1].Liked {
		t.Fatalf("r2 = %+v", reviews[1])
	}
}
