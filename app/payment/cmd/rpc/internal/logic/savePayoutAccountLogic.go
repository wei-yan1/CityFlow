package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"
	"cityflow/internal/domain/payment"

	"github.com/zeromicro/go-zero/core/logx"
)

type SavePayoutAccountLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSavePayoutAccountLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SavePayoutAccountLogic {
	return &SavePayoutAccountLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SavePayoutAccountLogic) SavePayoutAccount(in *pb.JsonBody) (*pb.JsonReply, error) {
	riderID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	var request payment.PayoutAccountRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	account, err := l.svcCtx.Payments.SavePayoutAccount(l.ctx, riderID, request)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(account)
}
