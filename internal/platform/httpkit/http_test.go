package httpkit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWriteGRPCErrorResourceExhaustedKeepsRateLimitShape(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteGRPCError(recorder, status.Error(codes.ResourceExhausted, "report location at most once every 10 seconds"), GRPCErrorMessages{
		TooManyRequests: "location reports are too frequent; retry later",
	})
	result := recorder.Result()
	if result.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d want=%d", result.StatusCode, http.StatusTooManyRequests)
	}
	if result.Header.Get("Retry-After") != "1" {
		t.Fatalf("Retry-After=%q", result.Header.Get("Retry-After"))
	}
	var body map[string]string
	if err := json.NewDecoder(result.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "location reports are too frequent; retry later" {
		t.Fatalf("error=%q", body["error"])
	}
}
