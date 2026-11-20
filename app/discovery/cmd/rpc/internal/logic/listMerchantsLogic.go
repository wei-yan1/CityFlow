package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListMerchantsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListMerchantsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListMerchantsLogic {
	return &ListMerchantsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *ListMerchantsLogic) ListMerchants(in *pb.ListQuery) (*pb.JsonReply, error) {
	if _, err := requireAdmin(l.ctx); err != nil {
		return nil, err
	}
	page, err := pageFromProto(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, err
	}
	profiles, err := l.svcCtx.MerchantSvc.List(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, grpcError(err)
	}
	return jsonReply(map[string]any{"data": profiles, "limit": page.Limit, "offset": page.Offset})
}
