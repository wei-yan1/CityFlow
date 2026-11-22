package logic

import (
	"context"

	"cityflow/app/promotion/cmd/rpc/internal/svc"
	"cityflow/app/promotion/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type QuoteLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewQuoteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QuoteLogic {
	return &QuoteLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *QuoteLogic) Quote(in *pb.QuoteTaskReq) (*pb.QuoteTaskResp, error) {
	quote, err := l.svcCtx.Service.Quote(
		l.ctx, in.GetUserId(), in.GetUserCouponId(), in.GetTaskId(),
		in.GetAmountFen(), in.GetTaskType(), in.GetLock(),
	)
	if err != nil {
		return nil, promotionError(err)
	}
	return &pb.QuoteTaskResp{Quote: toProtoQuote(quote)}, nil
}
