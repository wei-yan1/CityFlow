package mysqlstore

import (
	"context"
	"os"
	"testing"
	"time"

	"cityflow/internal/domain/location"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
)

func TestTaskCoordinatesMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set CITYFLOW_TEST_MYSQL_DSN for a dedicated local MySQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.CreateUser(ctx, user.CreateRequest{Email: id + "@example.test", Password: "integration-test-password", Name: "map test", Role: "USER"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM users WHERE id=?", u.ID)
	pickupTime := time.Date(2027, 1, 1, 2, 0, 0, 0, time.UTC)
	deliveryTime := pickupTime.Add(time.Hour)
	item, err := s.Create(ctx, task.CreateRequest{Title: "coordinates", Type: "DELIVERY", Pickup: "A", Destination: "B", PickupLocation: &location.Point{Latitude: 31.2, Longitude: 121.4}, PickupStops: []task.PickupStop{{Address: "A", Location: &location.Point{Latitude: 31.2, Longitude: 121.4}, PickupAt: &pickupTime}, {Address: "B", Location: &location.Point{Latitude: 31.21, Longitude: 121.41}}}, DeliveryAt: &deliveryTime}, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM tasks WHERE id=?", item.ID)
	if item.PickupLocation == nil || item.PickupLocation.Latitude != 31.2 || item.DestinationLocation != nil {
		t.Fatalf("coordinate roundtrip: %+v", item)
	}
	if len(item.PickupStops) != 2 || item.PickupStops[0].PickupAt == nil || !item.PickupStops[0].PickupAt.Equal(pickupTime) || item.DeliveryAt == nil || !item.DeliveryAt.Equal(deliveryTime) {
		t.Fatalf("stops/schedule roundtrip: %+v", item)
	}
	updated, err := s.Update(ctx, item.ID, task.UpdateRequest{Title: "updated", Type: "DELIVERY", Pickup: "C", Destination: "D", DestinationLocation: &location.Point{Latitude: 31.3, Longitude: 121.5}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PickupLocation != nil || updated.DestinationLocation == nil || updated.DestinationLocation.Longitude != 121.5 {
		t.Fatalf("coordinate update: %+v", updated)
	}
	request := task.CreateRequest{Title: "idempotency", Type: "DELIVERY", Pickup: "A", Destination: "B", IdempotencyKey: "mysql-test", IdempotencyHash: "hash-one"}
	first, err := s.Create(ctx, request, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM tasks WHERE id=?", first.ID)
	again, err := s.Create(ctx, request, u.ID)
	if err != nil || again.ID != first.ID {
		t.Fatalf("idempotency: %+v %v", again, err)
	}
	request.IdempotencyHash = "hash-two"
	if _, err = s.Create(ctx, request, u.ID); err == nil {
		t.Fatal("different payload accepted with same key")
	}
	// A real database must serialize stop progress with task status and events.
	progressTask, err := s.Create(ctx, task.CreateRequest{Title: "two stops", Type: "DELIVERY", Pickup: "A", Destination: "B", PickupStops: []task.PickupStop{{Address: "A"}, {Address: "B"}}}, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.orm.Exec("DELETE FROM tasks WHERE id=?", progressTask.ID)
	if _, err = s.Claim(ctx, progressTask.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AdvancePickup(ctx, progressTask.ID, u.ID, 1, "ARRIVED"); err == nil {
		t.Fatal("out of order pickup accepted")
	}
	for _, step := range []struct {
		index  int
		status string
	}{{0, "ARRIVED"}, {0, "PICKED_UP"}, {1, "ARRIVED"}, {1, "PICKED_UP"}} {
		if _, err = s.AdvancePickup(ctx, progressTask.ID, u.ID, step.index, step.status); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := s.Get(ctx, progressTask.ID)
	if err != nil || progress.PickupStops[1].Status != "PICKED_UP" {
		t.Fatalf("progress: %+v %v", progress, err)
	}
}
