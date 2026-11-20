package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CouponsForTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCouponsForTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CouponsForTaskLogic {
	return &CouponsForTaskLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CouponsForTaskLogic) CouponsForTask(in *pb.IdRequest) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("task id is required"))
	}
	quotes, err := l.svcCtx.CouponSvc.QuotesForTask(l.ctx, userID, in.GetId())
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"data": quotes})
}
