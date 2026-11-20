package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAccessLogWriterBatchesAndFlushes(t *testing.T) {
	var mu sync.Mutex
	var got string
	writer := NewAccessLogWriter(16, 2, 5*time.Millisecond, func(lines string) {
		mu.Lock()
		defer mu.Unlock()
		got += lines + "\n"
	})
	writer.Start()
	for _, line := range []string{"a", "b", "c"} {
		if !writer.Write(line) {
			t.Fatalf("write %q failed", line)
		}
	}
	writer.Close()

	mu.Lock()
	defer mu.Unlock()
	for _, line := range []string{"a", "b", "c"} {
		if !strings.Contains(got, line) {
			t.Fatalf("batch output %q missing %q", got, line)
		}
	}
}

func TestAccessLogMiddlewareFallsBackWhenWriterClosed(t *testing.T) {
	writer := NewAccessLogWriter(1, 1, time.Second, func(string) {})
	writer.Close()

	handler := AccessLogWithWriter(writer)(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}
