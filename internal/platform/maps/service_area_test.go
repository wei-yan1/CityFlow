package maps

import (
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"context"
	"errors"
	"math"
	"testing"
)

type fakeDistrict struct {
	district *District
	err      error
}

func (f fakeDistrict) District(context.Context, string) (*District, error) { return f.district, f.err }
func TestServiceAreaGCJAndFailure(t *testing.T) {
	p := location.Point{Latitude: 25.274, Longitude: 110.29}
	got := wgs84ToGCJ(p)
	if math.Abs(got.Latitude-25.271164496375235) > 1e-10 || math.Abs(got.Longitude-110.29458489225246) > 1e-10 {
		t.Fatalf("converted %+v", got)
	}
	ring := []location.Point{{Latitude: 25.2, Longitude: 110.2}, {Latitude: 25.4, Longitude: 110.2}, {Latitude: 25.4, Longitude: 110.4}, {Latitude: 25.2, Longitude: 110.4}}
	a := ServiceArea{Districts: fakeDistrict{district: &District{Boundaries: [][]location.Point{ring}}}, Adcode: "450300"}
	if err := a.Validate(context.Background(), []location.Point{p}); err != nil {
		t.Fatal(err)
	}
	if err := a.Validate(context.Background(), []location.Point{p, {Latitude: 31.23, Longitude: 121.47}}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("outside: %v", err)
	}
	a.Districts = fakeDistrict{err: errors.New("upstream unavailable")}
	if err := a.Validate(context.Background(), []location.Point{p}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("provider failure: %v", err)
	}
}
