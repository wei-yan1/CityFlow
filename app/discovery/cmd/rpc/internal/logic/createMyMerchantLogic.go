package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/app/discovery/domain/merchant"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateMyMerchantLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateMyMerchantLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateMyMerchantLogic {
	return &CreateMyMerchantLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CreateMyMerchantLogic) CreateMyMerchant(in *pb.IdBody) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	var request merchant.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	profile, err := l.svcCtx.MerchantSvc.Create(l.ctx, userID, request)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(profile)
}
