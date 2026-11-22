package logic

import (
	"context"

	"cityflow/app/notification/cmd/rpc/internal/svc"
	"cityflow/app/notification/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListLogic {
	return &ListLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListLogic) List(in *pb.ListReq) (*pb.ListResp, error) {
	items, unread, err := l.svcCtx.Service.List(l.ctx, in.GetUserId(), in.GetUnreadOnly(), notificationPage(in.GetLimit(), in.GetOffset()))
	if err != nil {
		return nil, notificationError(err)
	}
	out := make([]*pb.NotificationView, 0, len(items))
	for _, item := range items {
		out = append(out, toProtoNotification(item))
	}
	return &pb.ListResp{Notifications: out, Unread: int32(unread), Limit: in.GetLimit(), Offset: in.GetOffset()}, nil
}
