package logic

import (
	"context"
	"strings"

	"cityflow/app/reaction/cmd/rpc/internal/svc"
	"cityflow/app/reaction/cmd/rpc/pb"
	"cityflow/internal/domain/reaction"
	"cityflow/internal/platform/rpckit"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type SetReactionLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSetReactionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SetReactionLogic {
	return &SetReactionLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SetReactionLogic) SetReaction(in *pb.SetReactionReq) (*pb.SetReactionResp, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	targetType := strings.TrimSpace(in.GetTargetType())
	kind := strings.TrimSpace(in.GetKind())
	if !reaction.ValidTargetType(targetType) || !reaction.ValidKind(kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	targetID := strings.TrimSpace(in.GetTargetId())
	if targetID == "" || len(targetID) > 128 {
		return nil, status.Error(codes.InvalidArgument, "invalid target_id")
	}
	active, count, err := l.svcCtx.Cache.Set(l.ctx, targetType, targetID, claims.Subject, kind, in.GetActive())
	if err != nil {
		return nil, status.Error(codes.Unavailable, "reaction store unavailable")
	}
	return &pb.SetReactionResp{Active: active, Count: count}, nil
}

type GetReactionStateLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetReactionStateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetReactionStateLogic {
	return &GetReactionStateLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetReactionStateLogic) GetReactionState(in *pb.ReactionStateReq) (*pb.ReactionStateResp, error) {
	// Public place/rider reads may arrive without a bearer token. Counts are
	// public; only liked is user-specific, so anonymous reads are allowed.
	userID := ""
	if claims, err := rpckit.Claims(l.ctx); err == nil {
		userID = claims.Subject
	} else if status.Code(err) != codes.Unauthenticated {
		return nil, err
	}
	targetType := strings.TrimSpace(in.GetTargetType())
	kind := strings.TrimSpace(in.GetKind())
	if !reaction.ValidTargetType(targetType) || !reaction.ValidKind(kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	ids := uniqueIDs(in.GetTargetIds(), l.svcCtx.Config.MaxSummaryIDs)
	if len(ids) == 0 {
		return &pb.ReactionStateResp{Items: []*pb.ReactionStateItem{}}, nil
	}
	states, err := l.svcCtx.Cache.GetStates(l.ctx, targetType, ids, userID, kind)
	if err == nil && len(states) == len(ids) {
		items := make([]*pb.ReactionStateItem, 0, len(states))
		for i, state := range states {
			// A missing score means the target has not been cached yet; a real
			// zero is authoritative. Only the former falls back to MySQL.
			if state.Count == 0 && !state.CountKnown && kind == reaction.KindLike {
				if count, countErr := l.svcCtx.Store.Count(l.ctx, targetType, state.TargetID, kind); countErr == nil {
					state.Count = count
				}
			}
			items = append(items, &pb.ReactionStateItem{
				TargetId: ids[i], Count: state.Count, Liked: state.Liked,
			})
		}
		return &pb.ReactionStateResp{Items: items}, nil
	}
	return l.fallbackState(ids, targetType, kind)
}

func (l *GetReactionStateLogic) fallbackState(ids []string, targetType, kind string) (*pb.ReactionStateResp, error) {
	items := make([]*pb.ReactionStateItem, 0, len(ids))
	for _, id := range ids {
		item := &pb.ReactionStateItem{TargetId: id}
		if kind == reaction.KindLike {
			if count, err := l.svcCtx.Store.Count(l.ctx, targetType, id, kind); err == nil {
				item.Count = count
			}
		}
		items = append(items, item)
	}
	return &pb.ReactionStateResp{Items: items}, nil
}

type ListMineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMineLogic {
	return &ListMineLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListMineLogic) ListMine(in *pb.ListMineReq) (*pb.ListMineResp, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	targetType := strings.TrimSpace(in.GetTargetType())
	kind := strings.TrimSpace(in.GetKind())
	if !reaction.ValidTargetType(targetType) || !reaction.ValidKind(kind) {
		return nil, status.Error(codes.InvalidArgument, "unsupported target_type or kind")
	}
	items, next, err := l.svcCtx.Store.ListMine(l.ctx, claims.Subject, kind, targetType, in.GetCursor(), int(in.GetLimit()))
	if err != nil {
		return nil, rpckit.Error(err, "reaction")
	}
	out := make([]*pb.ListMineItem, 0, len(items))
	for _, item := range items {
		out = append(out, &pb.ListMineItem{TargetId: item.TargetID, CreatedAtMs: item.CreatedAt.UnixMilli()})
	}
	return &pb.ListMineResp{Items: out, NextCursor: next}, nil
}

type HealthLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHealthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HealthLogic {
	return &HealthLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *HealthLogic) Health(in *pb.HealthReq) (*pb.HealthResp, error) {
	ok, err := l.svcCtx.Health(l.ctx)
	if err != nil {
		return &pb.HealthResp{Ok: false, Message: err.Error()}, nil
	}
	return &pb.HealthResp{Ok: ok, Message: "ok"}, nil
}

func uniqueIDs(raw []string, max int) []string {
	if max <= 0 || max > 50 {
		max = 50
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 128 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if len(out) == max {
			break
		}
	}
	return out
}
