package logic

import (
	"context"
	"encoding/json"
	"errors"

	paymentservice "cityflow/app/payment/application/payment"
	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"
	"cityflow/app/payment/domain/payment"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type RefundForTaskInternalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRefundForTaskInternalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefundForTaskInternalLogic {
	return &RefundForTaskInternalLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *RefundForTaskInternalLogic) RefundForTaskInternal(in *pb.JsonBody) (*pb.JsonReply, error) {
	if _, err := currentUser(l.ctx); err != nil {
		return nil, err
	}
	var item paymentservice.TaskView
	if err := json.Unmarshal(in.GetBodyJson(), &item); err != nil {
		return nil, grpcError(domainErrInvalid("invalid task payload"))
	}
	order, err := l.svcCtx.Orders.PaymentOrderByTask(l.ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return jsonReply(map[string]any{"refunded": false, "reason": "no payment"})
	}
	if err != nil {
		return nil, grpcError(err)
	}
	if order.Status != payment.PaymentPaid && order.Status != payment.PaymentRefundPending && order.Status != payment.PaymentRefundFailed {
		return jsonReply(map[string]any{"refunded": order.Status == payment.PaymentRefunded, "reason": "not refundable"})
	}
	refunded, err := l.svcCtx.Payments.Refund(l.ctx, order.ID, order.PayerID, auth.RoleUser, "任务已取消")
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"refunded": true, "payment": refunded})
}
