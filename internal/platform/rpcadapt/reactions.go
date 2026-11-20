package rpcadapt

import (
	"context"

	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	"cityflow/internal/application/service"
)

// ReactionReader reads the reaction projection without touching Redis or the
// Reaction database directly.
type ReactionReader struct{ Client reactionclient.Reaction }

func (r ReactionReader) ReactionStates(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]service.ReactionState, error) {
	if r.Client == nil {
		return nil, nil
	}
	reply, err := r.Client.GetReactionState(forward(ctx), &reactionclient.ReactionStateReq{
		TargetType: targetType, Kind: kind, TargetIds: ids,
	})
	if err != nil {
		return nil, toDomain(err)
	}
	out := make(map[string]service.ReactionState, len(reply.GetItems()))
	for _, item := range reply.GetItems() {
		out[item.GetTargetId()] = service.ReactionState{Count: item.GetCount(), Active: item.GetLiked()}
	}
	return out, nil
}
