package logic

import (
	"context"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/domain/auth"
	taskdomain "cityflow/internal/domain/task"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type UpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateLogic {
	return &UpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateLogic) Update(in *taskpb.UpdateRequest) (*taskpb.TaskReply, error) {
	claims, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireAnyRole(claims.Role, auth.RoleUser, auth.RoleAdmin); err != nil {
		return nil, err
	}
	var request taskdomain.UpdateRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	item, err := l.svcCtx.Tasks.Update(l.ctx, in.GetId(), request, claims.Subject, claims.Role)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	body, err := encodeBody(item)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode task")
	}
	return &taskpb.TaskReply{BodyJson: body}, nil
}
