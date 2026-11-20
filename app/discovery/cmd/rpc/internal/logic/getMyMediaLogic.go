package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetMyMediaLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetMyMediaLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetMyMediaLogic {
	return &GetMyMediaLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *GetMyMediaLogic) GetMyMedia(in *pb.IdRequest) (*pb.JsonReply, error) {
	userID, err := currentUser(l.ctx)
	if err != nil {
		return nil, err
	}
	avatar, images, err := l.svcCtx.ReviewSvc.GetMedia(l.ctx, userID)
	if err != nil {
		return nil, grpcError(err)
	}
	if images == nil {
		images = []string{}
	}
	return jsonReply(map[string]any{"avatar_url": avatar, "image_urls": images})
}
