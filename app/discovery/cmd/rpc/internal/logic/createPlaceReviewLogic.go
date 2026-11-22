package logic

import (
	"context"
	"errors"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/app/discovery/domain/merchant"
	"cityflow/internal/common/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreatePlaceReviewLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreatePlaceReviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePlaceReviewLogic {
	return &CreatePlaceReviewLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *CreatePlaceReviewLogic) CreatePlaceReview(in *pb.PlaceRequest) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	var request merchant.ReviewRequest
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	err = l.svcCtx.ReviewSvc.Create(l.ctx, in.GetSource(), in.GetId(), userID, request)
	if errors.Is(err, domain.ErrNotFound) && in.GetSource() == "amap" && l.svcCtx.Map != nil {
		if verifyErr := l.svcCtx.Map.VerifyPlace(l.ctx, in.GetId()); verifyErr == nil {
			err = l.svcCtx.ReviewSvc.Create(l.ctx, in.GetSource(), in.GetId(), userID, request)
		} else {
			err = verifyErr
		}
	}
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]bool{"created": true})
}
