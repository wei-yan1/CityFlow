package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestIDAddsHeaderAndContext(t *testing.T) {
	var seen string
	handler := RequestID()(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))

	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing X-Request-ID response header")
	}
	if seen == "" || seen != recorder.Header().Get("X-Request-ID") {
		t.Fatalf("context request id = %q, header = %q", seen, recorder.Header().Get("X-Request-ID"))
	}
}

func TestRateLimitRejectsSecondRequest(t *testing.T) {
	handler := RateLimit(1, 1)(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	first := httptest.NewRecorder()
	handler(first, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status = %d", first.Code)
	}

	second := httptest.NewRecorder()
	handler(second, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", second.Code)
	}
}

func TestRecoverReturnsJSONWithoutLeakingPanic(t *testing.T) {
	handler := Recover()(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"internal server error"`) || strings.Contains(body, "boom") {
		t.Fatalf("body leaked panic or missed error: %s", body)
	}
}

func TestIdleConnClosesWithoutClientReads(t *testing.T) {
	server, client := net.Pipe()
	wrapped := newIdleConn(client, 20*time.Millisecond)
	defer wrapped.Close()

	done := make(chan error, 1)
	go func() {
		_, err := server.Read(make([]byte, 1))
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("server read unexpectedly succeeded after idle close")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("idle connection was not closed")
	}
}
