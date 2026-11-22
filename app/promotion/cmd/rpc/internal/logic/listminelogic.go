package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListMineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMineLogic {
	return &ListMineLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListMineLogic) ListMine(in *pb.ListMineReq) (*pb.ListMineResp, error) {
	items, err := l.svcCtx.Service.Mine(l.ctx, in.GetUserId(), in.GetStatus(), couponPage(in.GetLimit(), in.GetOffset()))
	if err != nil {
		return nil, promotionError(err)
	}
	out := make([]*pb.UserCoupon, 0, len(items))
	for _, item := range items {
		out = append(out, toProtoUserCoupon(item))
	}
	return &pb.ListMineResp{Coupons: out, Limit: in.GetLimit(), Offset: in.GetOffset()}, nil
}
