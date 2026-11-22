package logic

import (
	"context"

	"cityflow/app/notification/cmd/rpc/internal/svc"
	"cityflow/app/notification/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MarkReadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMarkReadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MarkReadLogic {
	return &MarkReadLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MarkReadLogic) MarkRead(in *pb.MarkReadReq) (*pb.MarkReadResp, error) {
	count, err := l.svcCtx.Service.MarkRead(l.ctx, in.GetUserId(), in.GetIds(), in.GetAll())
	if err != nil {
		return nil, notificationError(err)
	}
	return &pb.MarkReadResp{Unread: int32(count)}, nil
}
