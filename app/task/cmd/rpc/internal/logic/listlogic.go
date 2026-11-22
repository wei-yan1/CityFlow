package logic

import (
	"context"
	"strings"

	"cityflow/app/task/cmd/rpc/internal/svc"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	taskdomain "cityflow/app/task/domain/task"
	"cityflow/internal/common/auth"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListLogic {
	return &ListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListLogic) List(in *taskpb.ListRequest) (*taskpb.ListReply, error) {
	claims, err := claims(l.ctx)
	if err != nil {
		return nil, err
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	statusFilter := strings.ToUpper(strings.TrimSpace(in.GetStatus()))
	var items []*taskdomain.Task
	switch claims.Role {
	case auth.RoleUser:
		items, err = l.svcCtx.Tasks.ListForUser(l.ctx, claims.Subject, statusFilter, page)
	case auth.RoleRider:
		items, err = l.svcCtx.Tasks.ListForRider(l.ctx, claims.Subject, statusFilter, page)
	case auth.RoleAdmin:
		items, err = l.svcCtx.Tasks.ListAll(l.ctx, statusFilter, page)
	default:
		return nil, status.Error(codes.PermissionDenied, "insufficient role")
	}
	if err != nil {
		return nil, grpcTaskError(err)
	}
	if items == nil {
		items = []*taskdomain.Task{}
	}
	body, err := encodeBody(map[string]any{
		"data": items, "limit": page.Limit, "offset": page.Offset,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode task list")
	}
	return &taskpb.ListReply{BodyJson: body}, nil
}
