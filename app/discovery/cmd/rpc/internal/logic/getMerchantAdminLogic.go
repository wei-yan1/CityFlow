package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMerchantAdminLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetMerchantAdminLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMerchantAdminLogic {
	return &GetMerchantAdminLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetMerchantAdminLogic) GetMerchantAdmin(in *pb.IdRequest) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	profile, err := l.svcCtx.MerchantSvc.Profile(l.ctx, in.GetId())
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(profile)
}
