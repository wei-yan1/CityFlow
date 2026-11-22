package logic

import (
	"context"
	"strings"

	"cityflow/app/identity/cmd/rpc/internal/svc"
	"cityflow/app/identity/cmd/rpc/pb"
	"cityflow/app/identity/domain/auth"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogic) Login(in *identity.LoginRequest) (*identity.AuthReply, error) {
	email := strings.TrimSpace(in.GetEmail())
	if email == "" || in.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}
	response, err := l.svcCtx.Auth.Login(l.ctx, auth.LoginRequest{
		Email:    email,
		Password: in.GetPassword(),
	})
	if err != nil {
		return nil, mapError(err, "invalid email or password")
	}
	refreshToken := ""
	if l.svcCtx.Refresh != nil {
		refreshToken, err = l.svcCtx.Auth.StartRefreshSession(l.ctx, l.svcCtx.Refresh, response.User.ID)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "登录服务暂不可用")
		}
	}
	return &identity.AuthReply{
		AccessToken:  response.AccessToken,
		TokenType:    response.TokenType,
		ExpiresAt:    formatTime(response.ExpiresAt),
		RefreshToken: refreshToken,
		User:         userMessage(response.User),
	}, nil
}
