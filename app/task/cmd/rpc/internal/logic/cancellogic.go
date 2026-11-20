package logic

import (
	"context"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/domain/auth"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CancelLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCancelLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CancelLogic {
	return &CancelLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CancelLogic) Cancel(in *taskpb.CancelRequest) (*taskpb.TaskReply, error) {
	claims, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireAnyRole(claims.Role, auth.RoleUser, auth.RoleAdmin); err != nil {
		return nil, err
	}
	if _, err := l.svcCtx.Tasks.Get(l.ctx, in.GetId()); err != nil {
		return nil, grpcTaskError(err)
	}
	item, err := l.svcCtx.Tasks.Cancel(l.ctx, in.GetId(), claims.Subject, claims.Role)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	body, err := encodeBody(item)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode task")
	}
	return &taskpb.TaskReply{BodyJson: body}, nil
}
