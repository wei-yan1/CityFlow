package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ReleaseLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewReleaseLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ReleaseLogic {
	return &ReleaseLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ReleaseLogic) Release(in *pb.ReleaseCouponReq) (*pb.EmptyReply, error) {
	if err := l.svcCtx.Service.Release(l.ctx, in.GetUserCouponId(), in.GetTaskId()); err != nil {
		return nil, promotionError(err)
	}
	return &pb.EmptyReply{}, nil
}
