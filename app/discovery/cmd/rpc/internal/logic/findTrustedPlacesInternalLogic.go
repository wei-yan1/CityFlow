package logic

import (
	"context"
	"time"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type FindTrustedPlacesInternalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewFindTrustedPlacesInternalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *FindTrustedPlacesInternalLogic {
	return &FindTrustedPlacesInternalLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *FindTrustedPlacesInternalLogic) FindTrustedPlacesInternal(in *pb.JsonBody) (*pb.JsonReply, error) {
	var request struct {
		IDs []string  `json:"ids"`
		Now time.Time `json:"now"`
	}
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	if request.Now.IsZero() {
		request.Now = time.Now().UTC()
	}
	places, err := l.svcCtx.Places.FindTrustedPlaces(l.ctx, request.IDs, request.Now)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(places)
}
