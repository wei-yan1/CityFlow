package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MarkUsedLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMarkUsedLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkUsedLogic {
	return &MarkUsedLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MarkUsedLogic) MarkUsed(in *pb.MarkUsedReq) (*pb.EmptyReply, error) {
	err := l.svcCtx.Service.MarkUsed(l.ctx, in.GetUserCouponId(), in.GetTaskId(), in.GetPaymentOrderId(), in.GetDiscountFen())
	if err != nil {
		return nil, promotionError(err)
	}
	return &pb.EmptyReply{}, nil
}
