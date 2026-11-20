package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MapConfigLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMapConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MapConfigLogic {
	return &MapConfigLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MapConfigLogic) MapConfig(in *pb.Empty) (*pb.JsonReply, error) {
	return jsonReply(l.svcCtx.MapPublic)
}
