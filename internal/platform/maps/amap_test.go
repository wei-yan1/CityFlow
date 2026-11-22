package maps

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cityflow/internal/common/location"
)

func TestAmapSearchAndRiding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Error("missing provider credential")
		}
		switch r.URL.Path {
		case "/v3/place/text":
			if r.URL.Query().Get("keywords") != "人民广场" {
				t.Error("keywords lost")
			}
			fmt.Fprint(w, `{"status":"1","pois":[{"id":"a","name":"广场","address":[],"location":"121.47,31.23"},{"id":"bad","location":"NaN,31"}]}`)
		case "/v4/direction/bicycling":
			if r.URL.Query().Get("origin") != "121.470000,31.230000" {
				t.Error("wrong coordinate order")
			}
			fmt.Fprint(w, `{"errcode":0,"data":{"paths":[{"distance":1200,"duration":300,"steps":[{"instruction":"向北骑行","polyline":"121.47,31.23;121.47,31.24"},{"instruction":"右转","polyline":"121.47,31.24;121.48,31.24"}]}]}}`)
		default:
			t.Error("unexpected upstream path")
		}
	}))
	defer server.Close()
	a := NewAmap("test-key")
	a.baseURL = server.URL
	places, err := a.Search(context.Background(), "人民广场", "上海")
	if err != nil || len(places.Data) != 1 || places.Data[0].Address != "" || places.CoordinateSystem != "GCJ02" {
		t.Fatalf("search=%+v err=%v", places, err)
	}
	route, err := a.Riding(context.Background(), location.Point{Latitude: 31.23, Longitude: 121.47}, location.Point{Latitude: 31.24, Longitude: 121.48})
	if err != nil {
		t.Fatal(err)
	}
	if len(route.Coordinates) != 3 || len(route.Instructions) != 2 || route.DistanceMeters != 1200 || route.DurationSeconds != 300 || route.Mode != "bicycling" {
		t.Fatalf("bad route: %+v", route)
	}
}

func TestAmapFailureAndCancellation(t *testing.T) {
	for _, body := range []string{`{"errcode":10001,"errmsg":"INVALID_USER_KEY"}`, `{"errcode":0,"data":{"paths":[]}}`, `{"errcode":0,"data":{"paths":[{"steps":[{"polyline":"bad"}]}]}}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			a := NewAmap("private-test-key")
			a.baseURL = server.URL
			_, err := a.Riding(context.Background(), location.Point{}, location.Point{})
			if err == nil || strings.Contains(err.Error(), "private-test-key") {
				t.Fatalf("unexpected error: %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := a.Search(ctx, "广场", ""); err == nil || strings.Contains(err.Error(), "private-test-key") {
				t.Fatalf("cancellation: %v", err)
			}
		})
	}
}
