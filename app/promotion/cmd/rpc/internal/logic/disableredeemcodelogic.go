package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type DisableRedeemCodeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDisableRedeemCodeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DisableRedeemCodeLogic {
	return &DisableRedeemCodeLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *DisableRedeemCodeLogic) DisableRedeemCode(in *pb.DisableRedeemCodeReq) (*pb.EmptyReply, error) {
	if err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	if err := l.svcCtx.Service.DisableRedeemCode(l.ctx, in.GetId()); err != nil {
		return nil, promotionError(err)
	}
	return &pb.EmptyReply{}, nil
}
