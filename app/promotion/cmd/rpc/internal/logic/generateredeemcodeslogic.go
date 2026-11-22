package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"
	"cityflow/app/promotion/internal/domain/coupon"

	"github.com/zeromicro/go-zero/core/logx"
)

type GenerateRedeemCodesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGenerateRedeemCodesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GenerateRedeemCodesLogic {
	return &GenerateRedeemCodesLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GenerateRedeemCodesLogic) GenerateRedeemCodes(in *pb.GenerateRedeemCodesReq) (*pb.GenerateRedeemCodesResp, error) {
	if err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	items, err := l.svcCtx.Service.GenerateRedeemCodes(l.ctx, coupon.GenerateRedeemCodesInput{
		CouponID: in.GetCouponId(), Count: int(in.GetCount()), MaxRedeem: int(in.GetMaxRedeem()),
		BatchNo: in.GetBatchNo(), CreatedBy: in.GetCreatedBy(), Note: in.GetNote(),
		StartsAt: millisTime(in.GetStartsAtMs()), EndsAt: millisTime(in.GetEndsAtMs()),
	})
	if err != nil {
		return nil, promotionError(err)
	}
	reply := &pb.GenerateRedeemCodesResp{Codes: make([]*pb.RedeemCode, 0, len(items))}
	for _, item := range items {
		reply.Codes = append(reply.Codes, toProtoRedeemCode(item))
	}
	if len(items) > 0 {
		reply.BatchNo = items[0].BatchNo
	}
	return reply, nil
}
