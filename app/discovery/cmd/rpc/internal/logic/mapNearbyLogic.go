package logic

import (
	"context"
	"fmt"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MapNearbyLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMapNearbyLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MapNearbyLogic {
	return &MapNearbyLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MapNearbyLogic) MapNearby(in *pb.QueryRequest) (*pb.JsonReply, error) {
	if l.svcCtx.Map == nil {
		return nil, status.Error(codes.Unavailable, "map service is not configured")
	}
	var query struct {
		Latitude         float64 `json:"latitude"`
		Longitude        float64 `json:"longitude"`
		CoordinateSystem string  `json:"coordinate_system"`
	}
	if err := decodeBody(in.GetQueryJson(), &query); err != nil {
		return nil, grpcError(err)
	}
	point := location.Point{Latitude: query.Latitude, Longitude: query.Longitude}
	if !point.Valid() || query.CoordinateSystem != "GCJ02" {
		return nil, grpcError(fmt.Errorf("%w: 需要有效 GCJ02 坐标", domain.ErrInvalidRequest))
	}
	result, err := l.svcCtx.Map.Nearby(l.ctx, point)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return jsonReply(result)
}
