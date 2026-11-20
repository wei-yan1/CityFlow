package logic

import (
	"context"
	"encoding/json"

	"cityflow/app/payment/cmd/rpc/internal/svc"
	"cityflow/app/payment/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type NotifyLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewNotifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *NotifyLogic {
	return &NotifyLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *NotifyLogic) Notify(in *pb.NotifyRequest) (*pb.JsonReply, error) {
	headers := map[string]string{}
	if len(in.GetHeadersJson()) > 0 {
		if err := json.Unmarshal(in.GetHeadersJson(), &headers); err != nil {
			return nil, grpcError(domainErrInvalid("invalid notification headers"))
		}
	}
	if err := l.svcCtx.Payments.HandleNotify(l.ctx, in.GetProvider(), headers, in.GetBody()); err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"status": "ok"})
}
