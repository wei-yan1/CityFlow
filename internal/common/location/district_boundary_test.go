package location

import (
	"math"
	"testing"
)

func TestDistrictContains(t *testing.T) {
	// Two disconnected regions, one concave; repeat the last vertex as Amap may.
	rings := [][]Point{
		{{Latitude: 0, Longitude: 0}, {Latitude: 0, Longitude: 4}, {Latitude: 1, Longitude: 4}, {Latitude: 1, Longitude: 1}, {Latitude: 4, Longitude: 1}, {Latitude: 4, Longitude: 0}, {Latitude: 0, Longitude: 0}},
		{{Latitude: 10, Longitude: 10}, {Latitude: 10, Longitude: 11}, {Latitude: 11, Longitude: 11}, {Latitude: 11, Longitude: 10}},
	}
	tests := []struct {
		name  string
		point Point
		want  bool
	}{
		{"inside", Point{Latitude: 2, Longitude: .5}, true},
		{"concavity is outside", Point{Latitude: 2, Longitude: 2}, false},
		{"second region", Point{Latitude: 10.5, Longitude: 10.5}, true},
		{"edge", Point{Latitude: 1, Longitude: 2}, true},
		{"vertex", Point{Latitude: 4, Longitude: 0}, true},
		{"outside", Point{Latitude: 9, Longitude: 9}, false},
		{"not a number", Point{Latitude: math.NaN(), Longitude: 0}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DistrictContains(rings, tt.point); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	if DistrictContains([][]Point{{{Latitude: 0, Longitude: 0}, {Latitude: 1, Longitude: 1}, {Latitude: math.NaN(), Longitude: 0}}}, Point{}) {
		t.Fatal("invalid polygon accepted")
	}
	if DistrictContains([][]Point{{{Latitude: 0, Longitude: 0}, {Latitude: 1, Longitude: 1}, {Latitude: 2, Longitude: 2}}}, Point{Latitude: 1, Longitude: 1}) {
		t.Fatal("degenerate polygon accepted")
	}
}
