package service

import (
	"context"
	"log"
	"sync"
	"time"

	"cityflow/internal/domain/payment"
)

// NotifyLogBuffer decouples provider callback audit persistence from the
// callback request. The callback still performs its order/refund state
// transition synchronously; only the append-only audit row is batched.
//
// The buffer is bounded. When it is full, Enqueue returns false and the caller
// writes the row synchronously, so a burst can add latency but cannot silently
// drop audit evidence.
type NotifyLogBuffer struct {
	queue    chan payment.NotifyLog
	batch    int
	interval time.Duration
	write    func(context.Context, []payment.NotifyLog) error
	fallback func(context.Context, payment.NotifyLog) error

	mu     sync.RWMutex
	closed bool
	once   sync.Once
	wg     sync.WaitGroup
}

func NewNotifyLogBuffer(
	write func(context.Context, []payment.NotifyLog) error,
	fallback func(context.Context, payment.NotifyLog) error,
	capacity, batch int,
	interval time.Duration,
) *NotifyLogBuffer {
	if capacity <= 0 {
		capacity = 1024
	}
	if batch <= 0 {
		batch = 100
	}
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	return &NotifyLogBuffer{
		queue:    make(chan payment.NotifyLog, capacity),
		batch:    batch,
		interval: interval,
		write:    write,
		fallback: fallback,
	}
}

func (b *NotifyLogBuffer) Start() {
	if b == nil {
		return
	}
	b.wg.Add(1)
	go b.run()
}

func (b *NotifyLogBuffer) Enqueue(entry payment.NotifyLog) bool {
	if b == nil {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return false
	}
	select {
	case b.queue <- entry:
		return true
	default:
		return false
	}
}

func (b *NotifyLogBuffer) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.mu.Lock()
		b.closed = true
		close(b.queue)
		b.mu.Unlock()
		b.wg.Wait()
	})
}

func (b *NotifyLogBuffer) run() {
	defer b.wg.Done()
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	pending := make([]payment.NotifyLog, 0, b.batch)
	for {
		select {
		case entry, ok := <-b.queue:
			if !ok {
				b.flush(pending)
				return
			}
			pending = append(pending, entry)
			if len(pending) >= b.batch {
				b.flush(pending)
				pending = pending[:0]
			}
		case <-ticker.C:
			if len(pending) > 0 {
				b.flush(pending)
				pending = pending[:0]
			}
		}
	}
}

func (b *NotifyLogBuffer) flush(entries []payment.NotifyLog) {
	if len(entries) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if b.write != nil {
		if err := b.write(ctx, entries); err == nil {
			return
		} else {
			log.Printf("warning: batch payment notify audit failed, falling back to per-row writes: %v", err)
		}
	}
	if b.fallback == nil {
		return
	}
	for _, entry := range entries {
		if err := b.fallback(ctx, entry); err != nil {
			log.Printf("warning: record payment notify fallback: %v", err)
		}
	}
}
