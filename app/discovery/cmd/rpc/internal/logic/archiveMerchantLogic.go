package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ArchiveMerchantLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewArchiveMerchantLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ArchiveMerchantLogic {
	return &ArchiveMerchantLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ArchiveMerchantLogic) ArchiveMerchant(in *pb.IdRequest) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	if err := l.svcCtx.MerchantSvc.Archive(l.ctx, in.GetId()); err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]bool{"archived": true})
}
