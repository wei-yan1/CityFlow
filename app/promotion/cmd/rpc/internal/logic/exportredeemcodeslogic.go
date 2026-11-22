package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"
	"cityflow/app/promotion/internal/domain/coupon"

	"github.com/zeromicro/go-zero/core/logx"
)

type ExportRedeemCodesLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewExportRedeemCodesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ExportRedeemCodesLogic {
	return &ExportRedeemCodesLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ExportRedeemCodesLogic) ExportRedeemCodes(in *pb.ExportRedeemCodesReq) (*pb.ExportRedeemCodesResp, error) {
	if err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	items, err := l.svcCtx.Service.ExportRedeemCodes(l.ctx, coupon.RedeemCodeFilter{
		CouponID: in.GetCouponId(), BatchNo: in.GetBatchNo(), Status: in.GetStatus(),
	})
	if err != nil {
		return nil, promotionError(err)
	}
	reply := &pb.ExportRedeemCodesResp{Codes: make([]*pb.RedeemCode, 0, len(items))}
	for _, item := range items {
		reply.Codes = append(reply.Codes, toProtoRedeemCode(item))
	}
	return reply, nil
}
