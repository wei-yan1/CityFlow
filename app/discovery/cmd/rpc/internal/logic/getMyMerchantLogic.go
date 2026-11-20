package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMyMerchantLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetMyMerchantLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMyMerchantLogic {
	return &GetMyMerchantLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetMyMerchantLogic) GetMyMerchant(in *pb.IdRequest) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	profile, err := l.svcCtx.MerchantSvc.Profile(l.ctx, userID)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(profile)
}
