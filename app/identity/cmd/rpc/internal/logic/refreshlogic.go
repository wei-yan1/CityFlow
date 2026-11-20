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

type RefreshLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewRefreshLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RefreshLogic {
	return &RefreshLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *RefreshLogic) Refresh(in *identity.RefreshRequest) (*identity.AuthReply, error) {
	previous := strings.TrimSpace(in.GetRefreshToken())
	if previous == "" {
		return nil, status.Error(codes.Unauthenticated, "请重新登录")
	}
	response, next, err := l.svcCtx.Auth.RefreshSession(l.ctx, l.svcCtx.Refresh, previous)
	if err != nil {
		return nil, mapError(err, "登录已过期，请重新登录")
	}
	return &identity.AuthReply{
		AccessToken:  response.AccessToken,
		TokenType:    response.TokenType,
		ExpiresAt:    formatTime(response.ExpiresAt),
		RefreshToken: next,
		User:         userMessage(response.User),
	}, nil
}
