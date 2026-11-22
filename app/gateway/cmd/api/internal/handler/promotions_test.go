package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWritePromotionErrorPendingReturnsAccepted(t *testing.T) {
	recorder := httptest.NewRecorder()
	writePromotionError(recorder, status.Error(codes.Aborted, "领取处理中，请使用同一 Idempotency-Key 重试"))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d, want 202", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "processing" {
		t.Fatalf("body=%v", body)
	}
}

func TestWritePromotionErrorBusyReturnsServiceUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	writePromotionError(recorder, status.Error(codes.Unavailable, "领取人数过多，请稍后重试"))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "busy" {
		t.Fatalf("body=%v", body)
	}
}
