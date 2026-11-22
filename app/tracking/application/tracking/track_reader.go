package trackingservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"cityflow/app/tracking/repository"
	"cityflow/internal/common/location"
)

// MergedTrackReader reads the durable MySQL archive and the Redis pending
// queue. A point can briefly exist in both stores between the MySQL commit and
// the Redis ZREM, so duplicate IDs are collapsed with the archived copy taking
// precedence.
type MergedTrackReader struct {
	Archive repository.TrackReader
	Pending repository.TrackReader
}

func NewMergedTrackReader(archive, pending repository.TrackReader) *MergedTrackReader {
	return &MergedTrackReader{Archive: archive, Pending: pending}
}

func (m *MergedTrackReader) RiderTrack(ctx context.Context, riderID string, from, to time.Time, after uint64, limit int) ([]location.TrackPoint, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 501 {
		limit = 501
	}
	var archived, pending []location.TrackPoint
	var archiveErr, pendingErr error
	if m != nil && m.Archive != nil {
		archived, archiveErr = m.Archive.RiderTrack(ctx, riderID, from, to, after, limit)
	} else {
		archiveErr = errors.New("archive reader is not configured")
	}
	if m != nil && m.Pending != nil {
		// Redis can still contain members whose version is <= after: the
		// archiver cursor advances on publish, while MySQL commit + ZREM may
		// lag. Query the whole pending window and apply the cursor after
		// merging so those points are not silently skipped.
		pending, pendingErr = m.Pending.RiderTrack(ctx, riderID, from, to, 0, 501)
	} else {
		pendingErr = errors.New("pending reader is not configured")
	}
	if archiveErr != nil && pendingErr != nil {
		return nil, fmt.Errorf("trajectory unavailable: archive: %v; pending: %v", archiveErr, pendingErr)
	}
	if archiveErr != nil {
		log.Printf("warning: trajectory archive read %s: %v; serving Redis pending points only", riderID, archiveErr)
	}
	if pendingErr != nil {
		log.Printf("warning: trajectory pending read %s: %v; serving MySQL archive only", riderID, pendingErr)
	}

	values := make([]location.TrackPoint, 0, len(archived)+len(pending))
	seen := make(map[uint64]struct{}, len(archived)+len(pending))
	for _, point := range archived {
		if _, ok := seen[point.ID]; ok {
			continue
		}
		seen[point.ID] = struct{}{}
		values = append(values, point)
	}
	for _, point := range pending {
		if _, ok := seen[point.ID]; ok {
			// The archived copy wins when a point is visible in both stores
			// between the MySQL commit and the Redis ZREM.
			continue
		}
		seen[point.ID] = struct{}{}
		values = append(values, point)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ObservedAt.Equal(values[j].ObservedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].ObservedAt.Before(values[j].ObservedAt)
	})
	result := make([]location.TrackPoint, 0, limit)
	for _, point := range values {
		if after > 0 && point.ID <= after {
			continue
		}
		result = append(result, point)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}
