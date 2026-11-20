package event

import (
	"sync"
	"testing"

	"cityflow/internal/domain/location"
)

func TestPositionSubscribersDoNotBlockOrCrossRiders(t *testing.T) {
	bus := NewPositions()
	updates, cancel := bus.Subscribe("rider-a")
	defer cancel()
	other, stopOther := bus.Subscribe("rider-b")
	defer stopOther()
	for version := int64(1); version <= 100; version++ {
		bus.Publish(location.Position{RiderID: "rider-a", Version: version})
	}
	if p := <-updates; p.Version != 100 {
		t.Fatalf("slow subscriber should retain latest position: %+v", p)
	}
	select {
	case <-other:
		t.Fatal("position leaked to another rider subscription")
	default:
	}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for i := 0; i < 100; i++ {
			bus.Publish(location.Position{RiderID: "rider-a"})
		}
	}()
	go func() { defer workers.Done(); cancel(); cancel() }()
	workers.Wait()
}
