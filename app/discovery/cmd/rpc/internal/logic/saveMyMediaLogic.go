package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type SaveMyMediaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSaveMyMediaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SaveMyMediaLogic {
	return &SaveMyMediaLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *SaveMyMediaLogic) SaveMyMedia(in *pb.IdBody) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	var request struct {
		Avatar string   `json:"avatar_url"`
		Images []string `json:"image_urls"`
	}
	if err := decodeBody(in.GetBodyJson(), &request); err != nil {
		return nil, grpcError(err)
	}
	if err := l.svcCtx.ReviewSvc.SaveMedia(l.ctx, userID, request.Avatar, request.Images); err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]bool{"saved": true})
}
