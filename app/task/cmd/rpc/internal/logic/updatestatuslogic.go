package logic

import (
	"context"
	"strings"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/domain/auth"
	taskdomain "cityflow/internal/domain/task"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateStatusLogic {
	return &UpdateStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateStatusLogic) UpdateStatus(in *taskpb.StatusRequest) (*taskpb.TaskReply, error) {
	claims, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireAnyRole(claims.Role, auth.RoleRider, auth.RoleAdmin); err != nil {
		return nil, err
	}
	var request taskdomain.UpdateStatusRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	next := strings.ToUpper(strings.TrimSpace(request.Status))
	if next == taskdomain.StatusCompleted || next == taskdomain.StatusCancelled {
		if _, err := l.svcCtx.Tasks.Get(l.ctx, in.GetId()); err != nil {
			return nil, grpcTaskError(err)
		}
	}
	item, err := l.svcCtx.Tasks.UpdateStatus(l.ctx, in.GetId(), request.Status, claims.Subject, claims.Role)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	body, err := encodeBody(item)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode task")
	}
	return &taskpb.TaskReply{BodyJson: body}, nil
}
