package logic

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/domain"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MapSearchLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewMapSearchLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MapSearchLogic {
	return &MapSearchLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (l *MapSearchLogic) MapSearch(in *pb.QueryRequest) (*pb.JsonReply, error) {
	if l.svcCtx.Map == nil {
		return nil, status.Error(codes.Unavailable, "map service is not configured")
	}
	var query struct {
		Keywords string `json:"keywords"`
		City     string `json:"city"`
	}
	if err := decodeBody(in.GetQueryJson(), &query); err != nil {
		return nil, grpcError(err)
	}
	keywords := strings.TrimSpace(query.Keywords)
	city := strings.TrimSpace(query.City)
	if count := utf8.RuneCountInString(keywords); count < 2 || count > 100 || utf8.RuneCountInString(city) > 50 {
		return nil, grpcError(fmt.Errorf("%w: 请输入 2 到 100 个字符的地点名称", domain.ErrInvalidRequest))
	}
	result, err := l.svcCtx.Map.Search(l.ctx, keywords, city)
	if err != nil {
		l.Errorf("map search failed: %v", err)
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return jsonReply(result)
}
