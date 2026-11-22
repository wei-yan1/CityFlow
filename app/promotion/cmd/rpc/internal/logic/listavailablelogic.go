package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListAvailableLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListAvailableLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListAvailableLogic {
	return &ListAvailableLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListAvailableLogic) ListAvailable(in *pb.ListCouponsReq) (*pb.ListCouponsResp, error) {
	items, err := l.svcCtx.Service.Available(l.ctx, in.GetUserId(), couponPage(in.GetLimit(), in.GetOffset()))
	if err != nil {
		return nil, promotionError(err)
	}
	out := make([]*pb.Coupon, 0, len(items))
	for _, item := range items {
		out = append(out, toProtoCoupon(item))
	}
	return &pb.ListCouponsResp{Coupons: out, Limit: in.GetLimit(), Offset: in.GetOffset()}, nil
}
