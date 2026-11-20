package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RefundLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRefundLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefundLogic {
	return &RefundLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

type refundBody struct {
	Reason string `json:"reason"`
}

func (l *RefundLogic) Refund(in *pb.IdBody) (*pb.JsonReply, error) {
	claims, err := currentClaims(l.ctx)
	if err != nil {
		return nil, err
	}
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("order id is required"))
	}
	var body refundBody
	if err := decodeBody(in.GetBodyJson(), &body); err != nil {
		return nil, grpcError(err)
	}
	order, err := l.svcCtx.Payments.Refund(l.ctx, in.GetId(), claims.Subject, claims.Role, body.Reason)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"payment": order})
}
