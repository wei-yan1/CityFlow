package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"
	"cityflow/app/promotion/internal/domain/coupon"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListRedeemCodesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListRedeemCodesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListRedeemCodesLogic {
	return &ListRedeemCodesLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListRedeemCodesLogic) ListRedeemCodes(in *pb.ListRedeemCodesReq) (*pb.ListRedeemCodesResp, error) {
	if err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	items, err := l.svcCtx.Service.RedeemCodes(l.ctx, coupon.RedeemCodeFilter{
		CouponID: in.GetCouponId(), BatchNo: in.GetBatchNo(), Status: in.GetStatus(),
	}, couponPage(in.GetLimit(), in.GetOffset()))
	if err != nil {
		return nil, promotionError(err)
	}
	reply := &pb.ListRedeemCodesResp{
		Codes: make([]*pb.RedeemCode, 0, len(items)),
		Limit: in.GetLimit(), Offset: in.GetOffset(),
	}
	for _, item := range items {
		reply.Codes = append(reply.Codes, toProtoRedeemCode(item))
	}
	return reply, nil
}
