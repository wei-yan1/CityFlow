package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RedeemCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRedeemCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RedeemCodeLogic {
	return &RedeemCodeLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *RedeemCodeLogic) RedeemCode(in *pb.RedeemCodeReq) (*pb.RedeemCodeResp, error) {
	item, err := l.svcCtx.Service.Redeem(l.ctx, in.GetUserId(), in.GetCode())
	if err != nil {
		return nil, promotionError(err)
	}
	return &pb.RedeemCodeResp{Coupon: toProtoUserCoupon(item)}, nil
}
