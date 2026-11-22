package paymentservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"cityflow/app/payment/domain/payment"
)

func TestNotifyLogBufferBatchesAndFlushesOnClose(t *testing.T) {
	var mu sync.Mutex
	var batches [][]payment.NotifyLog
	buffer := NewNotifyLogBuffer(
		func(_ context.Context, logs []payment.NotifyLog) error {
			mu.Lock()
			defer mu.Unlock()
			batches = append(batches, append([]payment.NotifyLog(nil), logs...))
			return nil
		},
		nil, 16, 4, 20*time.Millisecond,
	)
	buffer.Start()
	for i := 0; i < 9; i++ {
		if !buffer.Enqueue(payment.NotifyLog{ProviderOrderNo: string(rune('a' + i))}) {
			t.Fatalf("enqueue %d failed", i)
		}
	}
	buffer.Close()

	mu.Lock()
	defer mu.Unlock()
	total := 0
	for _, batch := range batches {
		if len(batch) > 4 {
			t.Fatalf("batch size = %d, want at most 4", len(batch))
		}
		total += len(batch)
	}
	if total != 9 {
		t.Fatalf("flushed %d rows, want 9", total)
	}
}

func TestNotifyLogBufferFallsBackWhenFull(t *testing.T) {
	buffer := NewNotifyLogBuffer(nil, nil, 1, 10, time.Second)
	// Do not start the worker: the single slot is guaranteed to be occupied.
	if !buffer.Enqueue(payment.NotifyLog{ProviderOrderNo: "first"}) {
		t.Fatal("first enqueue failed")
	}
	if buffer.Enqueue(payment.NotifyLog{ProviderOrderNo: "second"}) {
		t.Fatal("second enqueue unexpectedly succeeded")
	}
	buffer.Close()
}
