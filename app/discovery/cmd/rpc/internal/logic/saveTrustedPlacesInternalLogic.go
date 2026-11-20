package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/application/repository"

	"github.com/zeromicro/go-zero/core/logx"
)

type SaveTrustedPlacesInternalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveTrustedPlacesInternalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveTrustedPlacesInternalLogic {
	return &SaveTrustedPlacesInternalLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SaveTrustedPlacesInternalLogic) SaveTrustedPlacesInternal(in *pb.JsonBody) (*pb.JsonReply, error) {
	var places []repository.TrustedPlace
	if err := decodeBody(in.GetBodyJson(), &places); err != nil {
		return nil, grpcError(err)
	}
	if err := l.svcCtx.Places.SaveTrustedPlaces(l.ctx, places); err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]bool{"saved": true})
}
