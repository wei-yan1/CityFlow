package logic

import (
	"errors"

	"cityflow/app/notification/cmd/rpc/pb"
	"cityflow/app/notification/internal/application/notificationapp"
	"cityflow/app/notification/internal/domain/notification"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func notificationPage(limit, offset int32) notificationapp.Page {
	return notificationapp.Page{Limit: int(limit), Offset: int(offset)}
}

func toProtoNotification(item *notification.Notification) *pb.NotificationView {
	if item == nil {
		return nil
	}
	out := &pb.NotificationView{
		Id: item.ID, UserId: item.UserID, Kind: item.Kind, Title: item.Title, Body: item.Body,
		TaskId: item.TaskID, Read: item.Read(), CreatedAtMs: item.CreatedAt.UnixMilli(),
	}
	if item.ReadAt != nil {
		out.ReadAtMs = item.ReadAt.UnixMilli()
	}
	return out
}

func notificationError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, notification.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, notification.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, notification.ErrUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
