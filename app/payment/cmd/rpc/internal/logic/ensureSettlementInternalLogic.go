package logic

import (
	"context"
	"encoding/json"

	paymentservice "cityflow/app/payment/application/payment"
	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type EnsureSettlementInternalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewEnsureSettlementInternalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *EnsureSettlementInternalLogic {
	return &EnsureSettlementInternalLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *EnsureSettlementInternalLogic) EnsureSettlementInternal(in *pb.JsonBody) (*pb.JsonReply, error) {
	if _, err := currentUser(l.ctx); err != nil {
		return nil, err
	}
	var item paymentservice.TaskView
	if err := json.Unmarshal(in.GetBodyJson(), &item); err != nil {
		return nil, grpcError(domainErrInvalid("invalid task payload"))
	}
	settlement, err := l.svcCtx.Payments.EnsureSettlement(l.ctx, &item)
	if err != nil {
		return nil, grpcError(err)
	}
	if settlement == nil {
		return &pb.JsonReply{}, nil
	}
	return jsonReply(settlement)
}
