package logic

import (
	"context"
	"errors"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain"

	"github.com/zeromicro/go-zero/core/logx"
)

type PlaceDetailLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewPlaceDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PlaceDetailLogic {
	return &PlaceDetailLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *PlaceDetailLogic) PlaceDetail(in *pb.PlaceRequest) (*pb.JsonReply, error) {
	detail, err := l.svcCtx.ReviewSvc.Detail(l.ctx, in.GetSource(), in.GetId(), optionalUser(l.ctx), int(in.GetOffset()))
	if errors.Is(err, domain.ErrNotFound) && in.GetSource() == "amap" && l.svcCtx.Map != nil {
		if verifyErr := l.svcCtx.Map.VerifyPlace(l.ctx, in.GetId()); verifyErr == nil {
			detail, err = l.svcCtx.ReviewSvc.Detail(l.ctx, in.GetSource(), in.GetId(), optionalUser(l.ctx), int(in.GetOffset()))
		} else {
			err = verifyErr
		}
	}
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(detail)
}
