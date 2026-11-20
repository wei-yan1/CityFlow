package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain/merchant"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateTaskMerchantReviewLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateTaskMerchantReviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateTaskMerchantReviewLogic {
	return &CreateTaskMerchantReviewLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CreateTaskMerchantReviewLogic) CreateTaskMerchantReview(in *pb.IdBody) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	var request merchant.ReviewRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	review, err := l.svcCtx.ReviewSvc.CreateForTask(l.ctx, in.GetId(), userID, request)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(review)
}
