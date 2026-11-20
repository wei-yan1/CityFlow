package logic

import (
	"context"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type HealthLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHealthLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HealthLogic {
	return &HealthLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HealthLogic) Health(in *taskpb.Empty) (*taskpb.HealthReply, error) {
	if err := l.svcCtx.Ping(l.ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "task database unavailable")
	}
	return &taskpb.HealthReply{Ok: true}, nil
}
