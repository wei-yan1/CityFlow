package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CouponsAvailableLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCouponsAvailableLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CouponsAvailableLogic {
	return &CouponsAvailableLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CouponsAvailableLogic) CouponsAvailable(in *pb.ListQuery) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, err
	}
	items, err := l.svcCtx.CouponSvc.Available(l.ctx, userID, page)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"data": items, "limit": page.Limit, "offset": page.Offset})
}
