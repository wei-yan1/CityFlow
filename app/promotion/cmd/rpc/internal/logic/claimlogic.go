package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/core/logx"
)

type ClaimLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewClaimLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ClaimLogic {
	return &ClaimLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ClaimLogic) Claim(in *pb.ClaimCouponReq) (*pb.ClaimCouponResp, error) {
	item, err := l.svcCtx.Service.ClaimWithIdempotency(
		l.ctx, in.GetUserId(), in.GetCouponId(), rpcctx.IdempotencyKey(l.ctx),
	)
	if err != nil {
		return nil, promotionError(err)
	}
	return &pb.ClaimCouponResp{Coupon: toProtoUserCoupon(item)}, nil
}
