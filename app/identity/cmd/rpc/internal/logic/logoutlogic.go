package logic

import (
	"context"
	"strings"

	"cityflow/app/identity/cmd/rpc/internal/svc"
	"cityflow/app/identity/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LogoutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LogoutLogic) Logout(in *identity.LogoutRequest) (*identity.Empty, error) {
	token := strings.TrimSpace(in.GetRefreshToken())
	if l.svcCtx.Refresh != nil && token != "" {
		if err := l.svcCtx.Refresh.RevokeRefreshSession(l.ctx, token); err != nil {
			return nil, status.Error(codes.Unavailable, "退出失败，请重试")
		}
	}
	return &identity.Empty{}, nil
}
