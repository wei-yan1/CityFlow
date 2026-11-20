package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MockPayLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMockPayLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MockPayLogic {
	return &MockPayLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MockPayLogic) MockPay(in *pb.IdRequest) (*pb.JsonReply, error) {
	payerID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("order id is required"))
	}
	order, err := l.svcCtx.Payments.SimulatePaid(l.ctx, in.GetId(), payerID)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"payment": order})
}
