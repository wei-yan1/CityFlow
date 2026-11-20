package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ArchiveMyMerchantLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewArchiveMyMerchantLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ArchiveMyMerchantLogic {
	return &ArchiveMyMerchantLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ArchiveMyMerchantLogic) ArchiveMyMerchant(in *pb.IdRequest) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := l.svcCtx.MerchantSvc.Archive(l.ctx, userID); err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]bool{"archived": true})
}
