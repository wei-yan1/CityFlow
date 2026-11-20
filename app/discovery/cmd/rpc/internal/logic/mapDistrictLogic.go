package logic

import (
	"context"
	"fmt"
	"regexp"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var adcodePattern = regexp.MustCompile(`^[1-9][0-9]{5}$`)

type MapDistrictLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMapDistrictLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MapDistrictLogic {
	return &MapDistrictLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MapDistrictLogic) MapDistrict(in *pb.QueryRequest) (*pb.JsonReply, error) {
	if l.svcCtx.Map == nil {
		return nil, status.Error(codes.Unavailable, "map service is not configured")
	}
	var query struct {
		Adcode string `json:"adcode"`
	}
	if err := decodeBody(in.GetQueryJson(), &query); err != nil {
		return nil, grpcError(err)
	}
	if !adcodePattern.MatchString(query.Adcode) {
		return nil, grpcError(fmt.Errorf("%w: 需要六位行政区编码", domain.ErrInvalidRequest))
	}
	result, err := l.svcCtx.Map.District(l.ctx, query.Adcode)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return jsonReply(result)
}
