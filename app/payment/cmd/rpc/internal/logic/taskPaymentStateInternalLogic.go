package logic

import (
	"context"
	"errors"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"
	"cityflow/internal/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type TaskPaymentStateInternalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewTaskPaymentStateInternalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TaskPaymentStateInternalLogic {
	return &TaskPaymentStateInternalLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *TaskPaymentStateInternalLogic) TaskPaymentStateInternal(in *pb.IdRequest) (*pb.JsonReply, error) {
	if _, err := currentUser(l.ctx); err != nil {
		return nil, err
	}
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("task id is required"))
	}
	order, err := l.svcCtx.Orders.PaymentOrderByTask(l.ctx, in.GetId())
	if errors.Is(err, domain.ErrNotFound) {
		return jsonReply(map[string]any{"status": "", "amount_fen": 0, "found": false})
	}
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"status": order.Status, "amount_fen": order.AmountFen, "found": true})
}
