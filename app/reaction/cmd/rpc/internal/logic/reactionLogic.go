package logic

import (
	"context"
	"strings"

	"cityflow/app/reaction/cmd/rpc/internal/svc"
	"cityflow/app/reaction/cmd/rpc/pb"
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
	result, err := l.svcCtx.Reaction.Set(l.ctx,
		strings.TrimSpace(in.GetTargetType()),
		strings.TrimSpace(in.GetTargetId()),
		claims.Subject,
		strings.TrimSpace(in.GetKind()),
		in.GetActive(),
	)
	if err != nil {
		return nil, rpckit.Error(err, "reaction")
	}
	return &pb.SetReactionResp{Active: result.Active, Count: result.Count}, nil
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
	userID := ""
	if claims, err := rpckit.Claims(l.ctx); err == nil {
		userID = claims.Subject
	} else if status.Code(err) != codes.Unauthenticated {
		return nil, err
	}
	states, err := l.svcCtx.Reaction.States(l.ctx,
		strings.TrimSpace(in.GetTargetType()),
		in.GetTargetIds(),
		userID,
		strings.TrimSpace(in.GetKind()),
	)
	if err != nil {
		return nil, rpckit.Error(err, "reaction")
	}
	items := make([]*pb.ReactionStateItem, 0, len(states))
	for _, state := range states {
		items = append(items, &pb.ReactionStateItem{
			TargetId: state.TargetID,
			Count:    state.Count,
			Liked:    state.Active,
		})
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
	items, next, err := l.svcCtx.Reaction.ListMine(l.ctx, claims.Subject,
		strings.TrimSpace(in.GetKind()),
		strings.TrimSpace(in.GetTargetType()),
		in.GetCursor(),
		int(in.GetLimit()),
	)
	if err != nil {
		return nil, rpckit.Error(err, "reaction")
	}
	out := make([]*pb.ListMineItem, 0, len(items))
	for _, item := range items {
		out = append(out, &pb.ListMineItem{
			TargetId:    item.TargetID,
			CreatedAtMs: item.CreatedAt.UnixMilli(),
		})
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
	if err := l.svcCtx.Ping(l.ctx); err != nil {
		return &pb.HealthResp{Ok: false, Message: err.Error()}, nil
	}
	return &pb.HealthResp{Ok: true, Message: "ok"}, nil
}
