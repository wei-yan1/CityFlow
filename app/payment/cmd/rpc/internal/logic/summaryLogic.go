package logic

import (
	"context"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SummaryLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSummaryLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SummaryLogic {
	return &SummaryLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SummaryLogic) Summary(in *pb.IdRequest) (*pb.JsonReply, error) {
	if in.GetId() == "" {
		return nil, grpcError(domainErrInvalid("task id is required"))
	}
	summary, err := l.svcCtx.Payments.Summary(l.ctx, in.GetId())
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(summary)
}
