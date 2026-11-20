package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SettlementsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSettlementsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SettlementsLogic {
	return &SettlementsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SettlementsLogic) Settlements(in *pb.ListQuery) (*pb.JsonReply, error) {
	riderID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, err
	}
	items, err := l.svcCtx.Payments.SettlementsForRider(l.ctx, riderID, page)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"data": items, "limit": page.Limit, "offset": page.Offset})
}
