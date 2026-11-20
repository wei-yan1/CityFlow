package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain/merchant"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateMerchantLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateMerchantLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateMerchantLogic {
	return &UpdateMerchantLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *UpdateMerchantLogic) UpdateMerchant(in *pb.IdBody) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	var request merchant.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	profile, err := l.svcCtx.MerchantSvc.Update(l.ctx, in.GetId(), request)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(profile)
}
