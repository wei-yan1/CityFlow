package service

import (
	"context"
	"errors"
	"testing"

	"cityflow/internal/domain/merchant"
)

type reactionReaderFake struct {
	states map[string]ReactionState
	err    error
}

func (f reactionReaderFake) ReactionStates(context.Context, string, []string, string, string) (map[string]ReactionState, error) {
	return f.states, f.err
}

func TestOverlayLikesReadsReactionProjection(t *testing.T) {
	svc := MerchantReviewService{Reactions: reactionReaderFake{states: map[string]ReactionState{
		"review-1": {Count: 7, Active: true},
	}}}
	reviews := []merchant.Review{{ID: "review-1", Likes: 3}}
	svc.overlayLikes(context.Background(), reviews, "user-1")
	if reviews[0].Likes != 7 || !reviews[0].Liked {
		t.Fatalf("review = %+v", reviews[0])
	}
}

func TestOverlayLikesFallsBackToProjectionOnReactionError(t *testing.T) {
	svc := MerchantReviewService{Reactions: reactionReaderFake{err: errors.New("down")}}
	reviews := []merchant.Review{{ID: "review-1", Likes: 4}}
	svc.overlayLikes(context.Background(), reviews, "user-1")
	if reviews[0].Likes != 4 || reviews[0].Liked {
		t.Fatalf("review = %+v", reviews[0])
	}
}
