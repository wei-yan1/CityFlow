package trackingmysql

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"cityflow/internal/common/location"
	platformmysql "cityflow/internal/platform/mysql"
)

// TestStoreArchivesAndReads runs against a real MySQL database. Set
// CITYFLOW_TEST_TRACKING_DSN to a disposable tracking schema to enable it.
func TestStoreArchivesAndReads(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_TRACKING_DSN")
	if dsn == "" {
		t.Skip("CITYFLOW_TEST_TRACKING_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := platformmysql.MigrateService(ctx, db, "tracking"); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	riderID := fmt.Sprintf("tracking-test-%d", time.Now().UTC().UnixNano())
	t.Cleanup(func() {
		_, _ = db.SQL().ExecContext(context.Background(), "DELETE FROM rider_track_points WHERE rider_id = ?", riderID)
	})

	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	points := []location.TrackPoint{
		newTrackPoint(riderID, "task-1", 1, base, 25.273, 110.290),
		newTrackPoint(riderID, "task-1", 2, base.Add(10*time.Second), 25.274, 110.291),
		newTrackPoint(riderID, "task-2", 3, base.Add(20*time.Second), 25.275, 110.292),
	}
	if err := store.ArchiveTrackPoints(ctx, points); err != nil {
		t.Fatal(err)
	}
	// Same primary key: the batch upsert must be idempotent and must not
	// rewrite the archived coordinates.
	duplicate := newTrackPoint(riderID, "task-1", 1, base, 99.0, 99.0)
	if err := store.ArchiveTrackPoints(ctx, []location.TrackPoint{duplicate}); err != nil {
		t.Fatal(err)
	}

	read, err := store.RiderTrack(ctx, riderID, base.Add(-time.Second), base.Add(30*time.Second), 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != 3 {
		t.Fatalf("read %d points, want 3", len(read))
	}
	if read[0].Latitude != 25.273 || read[1].Latitude != 25.274 || read[2].Latitude != 25.275 {
		t.Fatalf("unexpected point order: %+v", read)
	}
	if read[0].ID != 1 || read[1].ID != 2 || read[2].ID != 3 {
		t.Fatalf("unexpected point ids: %+v", read)
	}
	after, err := store.RiderTrack(ctx, riderID, base.Add(-time.Second), base.Add(30*time.Second), 2, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].ID != 3 {
		t.Fatalf("after cursor=%+v, want only id 3", after)
	}
}

func newTrackPoint(riderID, taskID string, version int64, observed time.Time, latitude, longitude float64) location.TrackPoint {
	return location.TrackPoint{
		ID:     uint64(version),
		TaskID: taskID,
		Position: location.Position{
			Source: "device", RiderID: riderID, TaskID: taskID,
			Point:      location.Point{Latitude: latitude, Longitude: longitude},
			Accuracy:   10,
			ObservedAt: observed,
			UpdatedAt:  observed.Add(100 * time.Millisecond),
			Version:    version,
		},
	}
}
