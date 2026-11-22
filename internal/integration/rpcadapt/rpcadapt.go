// Package rpcadapt implements the narrow read ports of the application
// services with zRPC calls to the owning service. A domain service reads a
// neighbour's facts through these adapters instead of touching its tables.
package rpcadapt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	identitypb "cityflow/app/identity/cmd/rpc/pb"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	trackingpb "cityflow/app/tracking/cmd/rpc/pb"
	trackingclient "cityflow/app/tracking/cmd/rpc/trackingclient"
	"cityflow/internal/common/contract"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/common/pagination"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func forward(ctx context.Context) context.Context {
	if token, ok := authn.BearerFromMetadata(ctx); ok {
		return rpcctx.WithBearer(ctx, token)
	}
	return ctx
}

func toDomain(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Unauthenticated:
		return domain.ErrUnauthorized
	case codes.PermissionDenied:
		return domain.ErrForbidden
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.FailedPrecondition, codes.AlreadyExists, codes.Aborted:
		return fmt.Errorf("%w: %s", domain.ErrConflict, status.Convert(err).Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", domain.ErrInvalidRequest, status.Convert(err).Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		return domain.ErrUnavailable
	default:
		return fmt.Errorf("upstream service failed: %w", err)
	}
}

func decode(data []byte, target any) error {
	if len(data) == 0 {
		return domain.ErrNotFound
	}
	return json.Unmarshal(data, target)
}

// Account is the identity facts a consuming service may project. It is an
// adapter-level DTO: callers convert it into their own local account view.
type Account struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Role      string `json:"role"`
	Status    string `json:"status"`
}

// Accounts reads identity facts.
type Accounts struct{ Client identityclient.Identity }

func (a Accounts) GetAccount(ctx context.Context, id string) (*Account, error) {
	reply, err := a.Client.GetUserInternal(forward(ctx), &identitypb.IdRequest{Id: id})
	if err != nil {
		return nil, toDomain(err)
	}
	var out Account
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a Accounts) IsActive(ctx context.Context, id string) (bool, error) {
	account, err := a.GetAccount(ctx, id)
	if err != nil {
		return false, err
	}
	return account.Status == "ACTIVE", nil
}

// Tasks reads Task-owned facts through an adapter DTO. Consumer services
// convert this snapshot into their own local read model.
type Tasks struct{ Client taskclient.Task }

// TaskSnapshot is the read-only task projection consumer services may keep
// locally. Task owns the aggregate; neighbours use this adapter DTO instead of
// importing Task's domain package or database.
type TaskSnapshot struct {
	ID                  string          `json:"id"`
	Title               string          `json:"title"`
	Type                string          `json:"type"`
	Status              string          `json:"status"`
	CreatedBy           string          `json:"created_by"`
	WorkerID            string          `json:"worker_id,omitempty"`
	Destination         string          `json:"destination"`
	DestinationPOIID    string          `json:"destination_poi_id,omitempty"`
	DestinationLocation *location.Point `json:"destination_location,omitempty"`
	QuotedAmountFen     *int64          `json:"quoted_amount_fen,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	Events              []TaskEvent     `json:"events"`
}

type TaskEvent struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	ActorID   string    `json:"actor_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (a Tasks) Snapshot(ctx context.Context, id string) (*TaskSnapshot, error) {
	reply, err := a.Client.GetTaskInternal(forward(ctx), &taskpb.IdRequest{Id: id})
	if err != nil {
		return nil, toDomain(err)
	}
	var out TaskSnapshot
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a Tasks) ListSnapshots(ctx context.Context, statusFilter string, page pagination.Page) ([]*TaskSnapshot, error) {
	reply, err := a.Client.ListTasksInternal(forward(ctx), &taskpb.ListQuery{Status: statusFilter, Limit: int32(page.Limit), Offset: int32(page.Offset)})
	if err != nil {
		return nil, toDomain(err)
	}
	var out []*TaskSnapshot
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RiderSnapshot is the Task-owned rider projection consumed by Tracking.
type RiderSnapshot struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	DisplayName string  `json:"display_name"`
	Status      string  `json:"status"`
	Rating      float64 `json:"rating"`
}

// Riders reads rider performance profiles owned by Task.
type Riders struct{ Client taskclient.Task }

func (a Riders) RiderForUser(ctx context.Context, userID string) (*RiderSnapshot, error) {
	reply, err := a.Client.GetRiderByUser(forward(ctx), &taskpb.IdRequest{Id: userID})
	if err != nil {
		return nil, toDomain(err)
	}
	var out RiderSnapshot
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Positions reads the latest rider position owned by Tracking.
type Positions struct{ Client trackingclient.Tracking }

func (a Positions) GetPosition(ctx context.Context, riderID string) (*location.Position, error) {
	reply, err := a.Client.GetPositionInternal(forward(ctx), &trackingpb.IdRequest{Id: riderID})
	if err != nil {
		return nil, toDomain(err)
	}
	var out location.Position
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReactionStateView is the legacy-shaped state used by Discovery/Task's internal
// reaction RPC compatibility layer. New read models use ReactionStates.
type ReactionStateView struct {
	TargetID string
	Count    int64
	Liked    bool
}

type ReactionMineView struct {
	TargetID  string
	CreatedAt time.Time
}

// Reactions adapts the standalone Reaction service. The caller's bearer token
// is forwarded so the service can fill the per-user liked flag without trusting
// a client-supplied user id.
type Reactions struct{ Client reactionclient.Reaction }

func (a Reactions) Set(ctx context.Context, targetType, targetID, _ string, kind string, active bool) (int64, error) {
	if a.Client == nil {
		return 0, domain.ErrUnavailable
	}
	reply, err := a.Client.SetReaction(forward(ctx), &reactionclient.SetReactionReq{
		TargetType: targetType, TargetId: targetID, Kind: kind, Active: active,
	})
	if err != nil {
		return 0, toDomain(err)
	}
	return reply.GetCount(), nil
}

func (a Reactions) States(ctx context.Context, targetType string, ids []string, userID, kind string) ([]ReactionStateView, error) {
	if a.Client == nil {
		return nil, domain.ErrUnavailable
	}
	reply, err := a.Client.GetReactionState(forward(ctx), &reactionclient.ReactionStateReq{
		TargetType: targetType, Kind: kind, TargetIds: ids,
	})
	if err != nil {
		return nil, toDomain(err)
	}
	out := make([]ReactionStateView, 0, len(reply.GetItems()))
	for _, item := range reply.GetItems() {
		out = append(out, ReactionStateView{TargetID: item.GetTargetId(), Count: item.GetCount(), Liked: item.GetLiked()})
	}
	return out, nil
}

func (a Reactions) LikedStates(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]bool, error) {
	states, err := a.States(ctx, targetType, ids, userID, kind)
	if err != nil {
		return nil, err
	}
	liked := make(map[string]bool, len(states))
	for _, state := range states {
		liked[state.TargetID] = state.Liked
	}
	return liked, nil
}

func (a Reactions) ListMine(ctx context.Context, userID, kind, targetType, cursor string, limit int) ([]ReactionMineView, string, error) {
	if a.Client == nil {
		return nil, "", domain.ErrUnavailable
	}
	reply, err := a.Client.ListMine(forward(ctx), &reactionclient.ListMineReq{
		Kind: kind, TargetType: targetType, Cursor: cursor, Limit: int32(limit),
	})
	if err != nil {
		return nil, "", toDomain(err)
	}
	out := make([]ReactionMineView, 0, len(reply.GetItems()))
	for _, item := range reply.GetItems() {
		out = append(out, ReactionMineView{
			TargetID:  item.GetTargetId(),
			CreatedAt: time.UnixMilli(item.GetCreatedAtMs()).UTC(),
		})
	}
	return out, reply.GetNextCursor(), nil
}

func (a Reactions) ReactionStates(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]contract.ReactionState, error) {
	states, err := a.States(ctx, targetType, ids, userID, kind)
	if err != nil {
		return nil, err
	}
	out := make(map[string]contract.ReactionState, len(states))
	for _, state := range states {
		out[state.TargetID] = contract.ReactionState{Count: state.Count, Active: state.Liked}
	}
	return out, nil
}

func (a Reactions) ReactionLiked(ctx context.Context, targetType string, ids []string, kind, userID string) (map[string]bool, error) {
	liked, err := a.LikedStates(ctx, targetType, ids, kind, userID)
	if err != nil {
		return nil, err
	}
	return liked, nil
}
