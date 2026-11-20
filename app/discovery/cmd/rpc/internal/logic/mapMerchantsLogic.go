package logic

import (
	"context"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/application/repository"
	"cityflow/internal/domain/location"

	"github.com/zeromicro/go-zero/core/logx"
)

type MapMerchantsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMapMerchantsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MapMerchantsLogic {
	return &MapMerchantsLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MapMerchantsLogic) MapMerchants(in *pb.Empty) (*pb.JsonReply, error) {
	// The viewport projection is a public read. Bounds filtering is supplied by
	// the map cache endpoint; without a viewport request this returns the
	// newest open merchants so the browser still has valid public data.
	profiles, err := l.svcCtx.MerchantSvc.List(l.ctx, "OPEN", repository.Page{Limit: 200})
	if err != nil {
		return nil, grpcError(err)
	}
	data := make([]location.MapMerchant, 0, len(profiles))
	for _, profile := range profiles {
		data = append(data, location.MapMerchant{Place: location.Place{
			ID:       profile.ID,
			Name:     profile.Name,
			Category: "MERCHANT",
			Address:  profile.Address,
			Location: location.Point{Latitude: profile.Latitude, Longitude: profile.Longitude},
		}})
	}
	return jsonReply(location.MerchantMap{Data: data, CoordinateSystem: "WGS84"})
}
