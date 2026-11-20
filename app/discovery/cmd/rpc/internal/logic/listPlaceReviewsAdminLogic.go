package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListPlaceReviewsAdminLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListPlaceReviewsAdminLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListPlaceReviewsAdminLogic {
	return &ListPlaceReviewsAdminLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListPlaceReviewsAdminLogic) ListPlaceReviewsAdmin(in *pb.AdminReviewQuery) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, err
	}
	reviews, total, err := l.svcCtx.ReviewSvc.ListAdminReviews(l.ctx, in.GetSource(), in.GetPlaceId(), page.Limit, page.Offset)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{
		"data": reviews, "total": total, "limit": page.Limit, "offset": page.Offset,
	})
}
