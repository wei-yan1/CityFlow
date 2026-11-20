package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain/auth"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListMyMerchantReviewsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMyMerchantReviewsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMyMerchantReviewsLogic {
	return &ListMyMerchantReviewsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListMyMerchantReviewsLogic) ListMyMerchantReviews(in *pb.ListQuery) (*pb.JsonReply, error) {
	claims, err := currentClaims(l.ctx)
	if err != nil {
		return nil, err
	}
	if claims.Role != auth.RoleMerchant {
		return nil, permissionDenied("merchant role required")
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, err
	}
	reviews, err := l.svcCtx.ReviewSvc.MerchantReviews(l.ctx, claims.Subject, page.Limit, page.Offset)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"data": reviews, "limit": page.Limit, "offset": page.Offset})
}
