package logic

import (
	"context"

	"cityflow/app/identity/cmd/rpc/internal/svc"
	"cityflow/app/identity/cmd/rpc/pb"

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

func (l *HealthLogic) Health(in *identity.Empty) (*identity.HealthReply, error) {
	if err := l.svcCtx.Ping(l.ctx); err != nil {
		return nil, status.Error(codes.Unavailable, "identity database unavailable")
	}
	return &identity.HealthReply{Ok: true}, nil
}
