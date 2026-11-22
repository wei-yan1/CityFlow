package logic

import (
	"context"
	"fmt"
	"strings"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MapRidingLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMapRidingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MapRidingLogic {
	return &MapRidingLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MapRidingLogic) MapRiding(in *pb.QueryRequest) (*pb.JsonReply, error) {
	if l.svcCtx.Map == nil {
		return nil, status.Error(codes.Unavailable, "map service is not configured")
	}
	var query struct {
		Origin           *location.Point `json:"origin"`
		Destination      *location.Point `json:"destination"`
		CoordinateSystem string          `json:"coordinate_system"`
		Mode             string          `json:"mode"`
	}
	if err := decodeBody(in.GetQueryJson(), &query); err != nil {
		return nil, grpcError(err)
	}
	if query.Origin == nil || query.Destination == nil || !query.Origin.Valid() || !query.Destination.Valid() || query.CoordinateSystem != "GCJ02" {
		return nil, grpcError(fmt.Errorf("%w: 起终点必须是有效的 GCJ02 坐标", domain.ErrInvalidRequest))
	}
	mode := strings.ToLower(strings.TrimSpace(query.Mode))
	if mode == "" {
		mode = "bicycling"
	}
	if mode != "bicycling" && mode != "electrobike" && mode != "driving" {
		return nil, grpcError(fmt.Errorf("%w: 路线方式必须是 bicycling、electrobike 或 driving", domain.ErrInvalidRequest))
	}
	result, err := l.svcCtx.Map.RidingMode(l.ctx, mode, *query.Origin, *query.Destination)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return jsonReply(result)
}
