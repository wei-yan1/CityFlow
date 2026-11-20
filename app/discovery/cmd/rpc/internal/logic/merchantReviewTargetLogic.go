package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type MerchantReviewTargetLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMerchantReviewTargetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MerchantReviewTargetLogic {
	return &MerchantReviewTargetLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MerchantReviewTargetLogic) MerchantReviewTarget(in *pb.IdBody) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	target, err := l.svcCtx.ReviewSvc.ReviewTargetForTask(l.ctx, in.GetId(), userID)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(target)
}
