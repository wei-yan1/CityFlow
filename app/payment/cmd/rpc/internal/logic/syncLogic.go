package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SyncLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSyncLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SyncLogic {
	return &SyncLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SyncLogic) Sync(in *pb.IdRequest) (*pb.JsonReply, error) {
	claims, err := currentClaims(l.ctx)
	if err != nil {
		return nil, err
	}
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("order id is required"))
	}
	order, err := l.svcCtx.Payments.SyncPayment(l.ctx, in.GetId(), claims.Subject, claims.Role)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"payment": order})
}
