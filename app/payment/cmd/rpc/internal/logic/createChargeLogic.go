package logic

import (
	"context"

	paymentservice "cityflow/app/payment/application/payment"
	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateChargeLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateChargeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateChargeLogic {
	return &CreateChargeLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

type createChargeBody struct {
	Provider       string `json:"provider"`
	UserCouponID   string `json:"user_coupon_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (l *CreateChargeLogic) CreateCharge(in *pb.IdBody) (*pb.JsonReply, error) {
	payerID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("task id is required"))
	}
	var body createChargeBody
	if err := decodeBody(in.GetBodyJson(), &body); err != nil {
		return nil, grpcError(err)
	}
	result, err := l.svcCtx.Payments.CreateCharge(l.ctx, paymentservice.CreateChargeInput{
		TaskID: in.GetId(), PayerID: payerID, Provider: body.Provider,
		UserCouponID: body.UserCouponID, IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(result)
}
