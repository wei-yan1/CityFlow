package service

import (
	"context"
	"log"
	"time"

	"cityflow/internal/application/repository"
)

// RunPositionJanitor periodically removes expired GEO/seen members. It is a
// separate loop so report latency never includes cleanup work. The returned
// function stops the loop and waits for the current iteration to finish.
func RunPositionJanitor(ctx context.Context, janitor repository.PositionJanitor, interval time.Duration, limit int) func() {
	if janitor == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if limit <= 0 {
		limit = 500
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				n, err := janitor.CleanupStalePositions(runCtx, limit)
				if err != nil {
					log.Printf("warning: cleanup stale positions: %v", err)
					continue
				}
				if n > 0 {
					log.Printf("cleanup stale positions: removed=%d", n)
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
