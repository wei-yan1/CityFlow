package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type RevealPlaceReviewIdentityLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRevealPlaceReviewIdentityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RevealPlaceReviewIdentityLogic {
	return &RevealPlaceReviewIdentityLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *RevealPlaceReviewIdentityLogic) RevealPlaceReviewIdentity(in *pb.IdBody) (*pb.JsonReply, error) {
	adminID, err := requireAdmin(l.ctx)
	if err != nil {
		return nil, err
	}
	var request struct {
		Reason string `json:"reason"`
	}
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	userID, err := l.svcCtx.ReviewSvc.RevealIdentity(l.ctx, in.GetId(), adminID, request.Reason)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]string{
		"review_id": in.GetId(), "user_id": userID, "action": "REVEAL", "reason": request.Reason,
	})
}
