package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResolveTrustedPlacesInternalLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewResolveTrustedPlacesInternalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResolveTrustedPlacesInternalLogic {
	return &ResolveTrustedPlacesInternalLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ResolveTrustedPlacesInternalLogic) ResolveTrustedPlacesInternal(in *pb.JsonBody) (*pb.JsonReply, error) {
	var request struct {
		IDs []string `json:"ids"`
	}
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	places, err := l.svcCtx.Trusted.Resolve(l.ctx, request.IDs)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(places)
}
