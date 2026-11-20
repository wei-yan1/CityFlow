package logic

import (
	"context"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetLogic {
	return &GetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetLogic) Get(in *taskpb.GetRequest) (*taskpb.TaskReply, error) {
	claims, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	item, err := l.svcCtx.Tasks.GetVisible(l.ctx, in.GetId(), *claims)
	if err != nil {
		return nil, grpcTaskError(err)
	}
	body, err := encodeBody(item)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode task")
	}
	return &taskpb.TaskReply{BodyJson: body}, nil
}
