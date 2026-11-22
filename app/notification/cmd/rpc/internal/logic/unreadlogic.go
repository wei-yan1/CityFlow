package logic

import (
	"context"

	"cityflow/app/notification/cmd/rpc/internal/svc"
	"cityflow/app/notification/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnreadLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUnreadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnreadLogic {
	return &UnreadLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *UnreadLogic) Unread(in *pb.UnreadReq) (*pb.UnreadResp, error) {
	count, err := l.svcCtx.Service.Unread(l.ctx, in.GetUserId())
	if err != nil {
		return nil, notificationError(err)
	}
	return &pb.UnreadResp{Unread: int32(count)}, nil
}
