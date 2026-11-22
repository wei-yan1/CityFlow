package logic

import (
	"context"

	"cityflow/app/notification/cmd/rpc/internal/svc"
	"cityflow/app/notification/cmd/rpc/pb"

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
	if l.svcCtx.DB == nil {
		return &pb.HealthReply{Ok: false, Message: "database not configured"}, nil
	}
	if err := l.svcCtx.DB.Ping(l.ctx); err != nil {
		return &pb.HealthReply{Ok: false, Message: err.Error()}, nil
	}
	return &pb.HealthReply{Ok: true, Message: "ok"}, nil
}
