package logic

import (
	"context"
	"errors"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"
	"cityflow/internal/common/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetPayoutAccountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetPayoutAccountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPayoutAccountLogic {
	return &GetPayoutAccountLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetPayoutAccountLogic) GetPayoutAccount(_ *pb.Empty) (*pb.JsonReply, error) {
	riderID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	account, err := l.svcCtx.Payments.PayoutAccount(l.ctx, riderID)
	if errors.Is(err, domain.ErrNotFound) {
		return jsonReply(map[string]any{"provider": "", "account": "", "account_name": ""})
	}
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(account)
}
