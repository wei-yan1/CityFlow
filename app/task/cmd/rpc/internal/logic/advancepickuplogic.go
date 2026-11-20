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

type AdvancePickupLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAdvancePickupLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AdvancePickupLogic {
	return &AdvancePickupLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AdvancePickupLogic) AdvancePickup(in *taskpb.PickupRequest) (*taskpb.TaskReply, error) {
	claims, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	if err := requireRole(auth.RoleRider, claims.Role); err != nil {
		return nil, err
	}
	var request struct {
		Status string `json:"status"`
	}
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	item, err := l.svcCtx.Tasks.AdvancePickup(l.ctx, in.GetId(), claims.Subject, int(in.GetIndex()), request.Status)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	body, err := encodeBody(item)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode task")
	}
	return &taskpb.TaskReply{BodyJson: body}, nil
}
