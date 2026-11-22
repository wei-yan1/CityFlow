package memory

import (
	"context"
	"errors"
	"sync"
	"testing"

	"cityflow/app/task/domain/rider"
	"cityflow/app/task/domain/task"
	"cityflow/internal/common/domain"
)

func TestClaimConcurrentHonorsConcurrentLimit(t *testing.T) {
	const riderID = "rider-1"
	s := &Store{
		tasks: map[string]*task.Task{
			"task-1": {ID: "task-1", Status: task.StatusOpen},
			"task-2": {ID: "task-2", Status: task.StatusOpen},
		},
		riders: map[string]*rider.Profile{
			riderID: {UserID: riderID, Status: "AVAILABLE", MaxConcurrentTasks: 1},
		},
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []string{"task-1", "task-2"} {
		wg.Add(1)
		go func(taskID string) {
			defer wg.Done()
			_, err := s.Claim(context.Background(), taskID, riderID)
			results <- err
		}(id)
	}
	wg.Wait()
	close(results)

	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("got successes=%d conflicts=%d, want exactly one of each", successes, conflicts)
	}
	if s.tasks["task-1"].Status == task.StatusOpen && s.tasks["task-2"].Status == task.StatusOpen {
		t.Fatal("one task should have been claimed")
	}
}
