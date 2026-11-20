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

type VerifyLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewVerifyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *VerifyLogic {
	return &VerifyLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *VerifyLogic) Verify(in *identity.VerifyRequest) (*identity.VerifyReply, error) {
	claims, ok := authctx.Claims(l.ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	return &identity.VerifyReply{
		Subject:   claims.Subject,
		Email:     claims.Email,
		Role:      claims.Role,
		Active:    true,
		IssuedAt:  claims.IssuedAt,
		ExpiresAt: claims.Expires,
	}, nil
}
