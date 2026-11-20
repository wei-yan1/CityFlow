package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CouponsMineLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCouponsMineLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CouponsMineLogic {
	return &CouponsMineLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CouponsMineLogic) CouponsMine(in *pb.ListQuery) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, err
	}
	items, err := l.svcCtx.CouponSvc.Mine(l.ctx, userID, in.GetStatus(), page)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"data": items, "limit": page.Limit, "offset": page.Offset})
}
