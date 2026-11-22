package logic

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/common/contract"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/rpckit"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const discoveryTargetMerchant = "MERCHANT"

var discoveryReactionTargets = map[string]bool{
	contract.TargetPlace:        true,
	contract.TargetPlaceReview:  true,
	contract.TargetPlaceComment: true,
	discoveryTargetMerchant:     true,
}

type SetReactionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSetReactionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SetReactionLogic {
	return &SetReactionLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

type setReactionBody struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Kind       string `json:"kind"`
	Active     bool   `json:"active"`
}

func (l *SetReactionLogic) SetReaction(in *pb.JsonBody) (*pb.JsonReply, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var body setReactionBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reaction body")
	}
	if !discoveryReactionTargets[body.TargetType] {
		return nil, status.Error(codes.InvalidArgument, "target_type is not owned by discovery")
	}
	if !validReactionKind(body.Kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported kind")
	}
	body.TargetID = strings.TrimSpace(body.TargetID)
	if body.TargetID == "" || len(body.TargetID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "invalid target_id")
	}
	if body.TargetType == discoveryTargetMerchant {
		if body.Kind != contract.KindLike {
			return nil, status.Error(codes.InvalidArgument, "merchant only supports LIKE")
		}
		state, err := l.svcCtx.LikeSvc.Set(l.ctx, body.TargetID, claims.Subject, body.Active)
		if err != nil {
			return nil, reactionError(err)
		}
		return &pb.JsonReply{BodyJson: reactionJSON("active", state.Active, "count", state.Count, "month", state.Month)}, nil
	}
	count, err := l.svcCtx.Reactions.Set(l.ctx, body.TargetType, body.TargetID, claims.Subject, body.Kind, body.Active)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	return &pb.JsonReply{BodyJson: reactionJSON("active", body.Active, "count", count)}, nil
}

type reactionStateBody struct {
	TargetType string   `json:"target_type"`
	Kind       string   `json:"kind"`
	TargetIDs  []string `json:"target_ids"`
}

type GetReactionStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetReactionStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetReactionStateLogic {
	return &GetReactionStateLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetReactionStateLogic) GetReactionState(in *pb.JsonBody) (*pb.JsonReply, error) {
	userID := ""
	if claims, err := rpckit.Claims(l.ctx); err == nil {
		userID = claims.Subject
	} else if status.Code(err) != codes.Unauthenticated {
		return nil, err
	}
	var body reactionStateBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reaction state body")
	}
	if !discoveryReactionTargets[body.TargetType] || !validReactionKind(body.Kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	ids := uniqueReactionIDs(body.TargetIDs)
	if body.TargetType == discoveryTargetMerchant {
		if body.Kind != contract.KindLike {
			return nil, status.Error(codes.InvalidArgument, "merchant only supports LIKE")
		}
		states, err := l.svcCtx.LikeSvc.States(l.ctx, ids, userID)
		if err != nil {
			return nil, reactionError(err)
		}
		items := make([]map[string]any, 0, len(states))
		for i, state := range states {
			items = append(items, map[string]any{
				"target_id": ids[i], "count": state.Count, "liked": state.Active,
			})
		}
		return &pb.JsonReply{BodyJson: mustReactionJSON(map[string]any{"items": items})}, nil
	}
	states, err := l.svcCtx.Reactions.States(l.ctx, body.TargetType, ids, userID, body.Kind)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	items := make([]map[string]any, 0, len(states))
	for _, state := range states {
		items = append(items, map[string]any{
			"target_id": state.TargetID, "count": state.Count, "liked": state.Liked,
		})
	}
	return &pb.JsonReply{BodyJson: mustReactionJSON(map[string]any{"items": items})}, nil
}

type listMineBody struct {
	Kind       string `json:"kind"`
	TargetType string `json:"target_type"`
	Cursor     string `json:"cursor"`
	Limit      int    `json:"limit"`
}

type ListMineReactionsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMineReactionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMineReactionsLogic {
	return &ListMineReactionsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListMineReactionsLogic) ListMineReactions(in *pb.JsonBody) (*pb.JsonReply, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var body listMineBody
	if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid reaction list body")
	}
	if !discoveryReactionTargets[body.TargetType] || !validReactionKind(body.Kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	if body.TargetType == discoveryTargetMerchant {
		return nil, status.Error(codes.InvalidArgument, "merchant likes are month-scoped; use the merchant hot list")
	}
	records, next, err := l.svcCtx.Reactions.ListMine(l.ctx, claims.Subject, body.Kind, body.TargetType, body.Cursor, body.Limit)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, map[string]any{
			"target_id": record.TargetID, "created_at_ms": record.CreatedAt.UnixMilli(),
		})
	}
	return &pb.JsonReply{BodyJson: mustReactionJSON(map[string]any{"items": items, "next_cursor": next})}, nil
}

func validReactionKind(kind string) bool {
	return kind == contract.KindLike || kind == contract.KindFavorite
}

func reactionError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, "merchant like operation failed")
	}
}

func uniqueReactionIDs(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == 50 {
			break
		}
	}
	return out
}

func reactionJSON(pairs ...any) []byte {
	body := map[string]any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		body[pairs[i].(string)] = pairs[i+1]
	}
	return mustReactionJSON(body)
}

func mustReactionJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}
