package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InvalidateMapCacheLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewInvalidateMapCacheLogic(ctx context.Context, svcCtx *svc.ServiceContext) *InvalidateMapCacheLogic {
	return &InvalidateMapCacheLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *InvalidateMapCacheLogic) InvalidateMapCache(in *pb.Empty) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	if l.svcCtx.MapCache == nil {
		return nil, status.Error(codes.Unavailable, "map cache is not configured")
	}
	if err := l.svcCtx.MapCache.Invalidate(l.ctx); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return jsonReply(map[string]bool{"invalidated": true})
}
