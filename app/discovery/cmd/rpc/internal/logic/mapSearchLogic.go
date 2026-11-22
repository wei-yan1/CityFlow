package logic

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"cityflow/app/discovery/cmd/rpc/internal/svc"
	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/app/discovery/domain/merchant"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/maps"

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
	local := l.searchMerchants(keywords, city)
	var result *maps.SearchResult
	if l.svcCtx.Map != nil {
		var err error
		result, err = l.svcCtx.Map.Search(l.ctx, keywords, city)
		if err != nil {
			l.Errorf("map search failed: %v", err)
			if len(local) == 0 {
				return nil, status.Error(codes.Unavailable, err.Error())
			}
		}
	} else if len(local) == 0 {
		return nil, status.Error(codes.Unavailable, "map service is not configured")
	}
	if result == nil {
		result = &maps.SearchResult{Provider: "cityflow", CoordinateSystem: "WGS84"}
	}
	result.Data = mergeSearchPlaces(local, result.Data)
	return jsonReply(result)
}

// searchMerchants is intentionally best-effort: a local database hiccup must
// not make AMap search unavailable, and a missing merchant must not hide AMap.
func (l *MapSearchLogic) searchMerchants(keywords, city string) []location.Place {
	if l.svcCtx.Merchants == nil || (city != "" && !strings.Contains(city, "桂林")) {
		return nil
	}
	searcher, ok := l.svcCtx.Merchants.(interface {
		SearchMerchants(context.Context, string, int) ([]*merchant.Profile, error)
	})
	if !ok {
		return nil
	}
	profiles, err := searcher.SearchMerchants(l.ctx, keywords, 10)
	if err != nil {
		l.Errorf("search merchants: %v", err)
		return nil
	}
	result := make([]location.Place, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, location.Place{
			ID: profile.ID, Source: "merchant", Name: profile.Name, Category: "商家",
			Address: profile.Address, Location: location.Point{Latitude: profile.Latitude, Longitude: profile.Longitude},
			CoordinateSystem: "WGS84", AMAPPOIID: profile.AMAPPOIID,
		})
	}
	return result
}

func mergeSearchPlaces(local, remote []location.Place) []location.Place {
	seenID := make(map[string]bool, len(local)+len(remote))
	seenName := make(map[string]bool, len(local))
	for _, place := range local {
		if place.ID != "" {
			seenID[place.ID] = true
		}
		seenName[strings.TrimSpace(place.Name)] = true
	}
	result := append([]location.Place(nil), local...)
	for _, place := range remote {
		if place.ID != "" && seenID[place.ID] {
			continue
		}
		// AMap often returns the same chain store as a local merchant. Keep the
		// local record because it carries the CityFlow merchant id.
		if seenName[strings.TrimSpace(place.Name)] {
			continue
		}
		result = append(result, place)
		if len(result) >= 20 {
			break
		}
	}
	return result
}
