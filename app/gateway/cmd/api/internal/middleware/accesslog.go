package middleware

import (
	"log"
	"strings"
	"sync"
	"time"
)

// AccessLogWriter is a bounded, asynchronous batch sink for edge request
// lines. A full queue never blocks the request goroutine: Write returns false
// and the caller emits the line synchronously.
type AccessLogWriter struct {
	queue    chan string
	batch    int
	interval time.Duration
	sink     func(string)

	mu     sync.RWMutex
	closed bool
	once   sync.Once
	wg     sync.WaitGroup
}

func NewAccessLogWriter(capacity, batch int, interval time.Duration, sink func(string)) *AccessLogWriter {
	if capacity <= 0 {
		capacity = 2048
	}
	if batch <= 0 {
		batch = 128
	}
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	if sink == nil {
		sink = func(lines string) { log.Print(lines) }
	}
	return &AccessLogWriter{
		queue:    make(chan string, capacity),
		batch:    batch,
		interval: interval,
		sink:     sink,
	}
}

func (w *AccessLogWriter) Start() {
	if w == nil {
		return
	}
	w.wg.Add(1)
	go w.run()
}

func (w *AccessLogWriter) Write(line string) bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return false
	}
	select {
	case w.queue <- line:
		return true
	default:
		return false
	}
}

func (w *AccessLogWriter) Close() {
	if w == nil {
		return
	}
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.queue)
		w.mu.Unlock()
		w.wg.Wait()
	})
}

func (w *AccessLogWriter) run() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	lines := make([]string, 0, w.batch)
	for {
		select {
		case line, ok := <-w.queue:
			if !ok {
				w.flush(lines)
				return
			}
			lines = append(lines, line)
			if len(lines) >= w.batch {
				w.flush(lines)
				lines = lines[:0]
			}
		case <-ticker.C:
			if len(lines) > 0 {
				w.flush(lines)
				lines = lines[:0]
			}
		}
	}
}

func (w *AccessLogWriter) flush(lines []string) {
	if len(lines) == 0 {
		return
	}
	w.sink(strings.Join(lines, "\n"))
}
