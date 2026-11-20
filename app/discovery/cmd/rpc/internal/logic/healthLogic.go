package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type HealthLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewHealthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HealthLogic {
	return &HealthLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *HealthLogic) Health(in *pb.Empty) (*pb.HealthReply, error) {
	if err := l.svcCtx.Ping(l.ctx); err != nil {
		return &pb.HealthReply{Ok: false, Message: err.Error()}, nil
	}
	return &pb.HealthReply{Ok: true, Message: "ok"}, nil
}
