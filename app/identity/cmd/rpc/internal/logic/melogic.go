package logic

import (
	"context"

	"cityflow/app/identity/cmd/rpc/internal/svc"
	"cityflow/app/identity/cmd/rpc/pb"
	"cityflow/internal/platform/authctx"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MeLogic {
	return &MeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MeLogic) Me(in *identity.MeRequest) (*identity.UserReply, error) {
	claims, ok := authctx.Claims(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	account, err := l.svcCtx.Auth.Me(l.ctx, claims.Subject)
	if err != nil {
		return nil, mapError(err, "could not load user")
	}
	return &identity.UserReply{User: userMessage(account)}, nil
}
