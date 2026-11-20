package maps

import (
	"cityflow/internal/domain/location"
	"cityflow/internal/infrastructure/cache"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAmapCityMunicipalityAndBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/geocode/regeo":
			fmt.Fprint(w, `{"status":"1","regeocode":{"addressComponent":{"city":[],"province":"上海市","adcode":"310101"}}}`)
		case "/v3/config/district":
			fmt.Fprint(w, `{"status":"1","districts":[{"name":"桂林市","adcode":"450300","center":"110.29,25.27","polyline":"110.2,25.2;110.3,25.2;110.3,25.3|110.4,25.4;110.5,25.4;110.5,25.5"}]}`)
		default:
			t.Error("unexpected provider request")
		}
	}))
	defer server.Close()
	a := NewAmap("test")
	a.baseURL = server.URL
	c, err := a.City(context.Background(), location.Point{Latitude: 31.23, Longitude: 121.47})
	if err != nil || c.Name != "上海市" || c.Adcode != "310000" || c.CoordinateSystem != "GCJ02" {
		t.Fatalf("city %+v %v", c, err)
	}
	d, err := a.District(context.Background(), "450300")
	if err != nil || len(d.Boundaries) != 2 || d.Center == nil {
		t.Fatalf("district %+v %v", d, err)
	}
	if _, err = a.District(context.Background(), "310000"); err == nil {
		t.Fatal("mismatched district accepted")
	}
}

type citySpy struct {
	MapProvider
	point location.Point
}

func (s *citySpy) City(_ context.Context, p location.Point) (*City, error) {
	s.point = p
	return &City{Name: "桂林市", Adcode: "450300", CoordinateSystem: "GCJ02"}, nil
}
func TestCityReverseLookupUsesAnonymousGridCenter(t *testing.T) {
	m := cache.NewMapCache(nil, 0, 60)
	defer m.Close()
	spy := &citySpy{}
	g := GuardedAmap{Provider: spy, Cache: m}
	input := location.Point{Latitude: 25.274123, Longitude: 110.290123}
	if _, err := g.City(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if spy.point == input || spy.point.Latitude < 25.2749 || spy.point.Latitude > 25.2751 || spy.point.Longitude < 110.2949 || spy.point.Longitude > 110.2951 {
		t.Fatalf("exact coordinate leaked to lookup: %+v", spy.point)
	}
}
