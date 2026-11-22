// Package discoveryrpc adapts Discovery's RPC projections to Task's ports.
package discoveryrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/internal/common/contract"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// HotRankClient is the narrow Discovery capability the scheduler needs. It is
// declared here so tests do not have to fake the whole generated client.
type HotRankClient interface {
	ListHotMerchants(ctx context.Context, in *discoveryclient.JsonBody, opts ...grpc.CallOption) (*discoveryclient.JsonReply, error)
}

// RankBuilder asks Discovery to rebuild the authoritative monthly hot
// snapshots. Discovery owns the ranking tables; Task only orchestrates the job.
type RankBuilder struct {
	Client HotRankClient
	Token  string
	Limit  int
}

type rankRebuildReply struct {
	Month      string         `json:"month"`
	Categories map[string]int `json:"categories"`
	Items      int            `json:"items"`
}

func (b RankBuilder) Build(ctx context.Context) (string, error) {
	if b.Client == nil {
		return "", errors.New("discovery rank client is unavailable")
	}
	if strings.TrimSpace(b.Token) == "" {
		return "", errors.New("internal job token is required for rank.build")
	}
	limit := b.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	body, err := json.Marshal(map[string]any{
		"rebuild": true,
		"limit":   limit,
	})
	if err != nil {
		return "", fmt.Errorf("encode rank.build request: %w", err)
	}
	ctx = metadata.AppendToOutgoingContext(ctx, contract.InternalJobTokenHeader, strings.TrimSpace(b.Token))
	reply, err := b.Client.ListHotMerchants(ctx, &discoveryclient.JsonBody{BodyJson: body})
	if err != nil {
		return "", fmt.Errorf("rebuild merchant hot ranking: %w", err)
	}
	var result rankRebuildReply
	if err := json.Unmarshal([]byte(reply.GetBodyJson()), &result); err != nil {
		return "", fmt.Errorf("decode rank.build reply: %w", err)
	}
	if len(result.Categories) == 0 {
		return "", errors.New("rank.build returned no categories")
	}
	return fmt.Sprintf("rank.build ok month=%s items=%d categories=%v", result.Month, result.Items, result.Categories), nil
}
