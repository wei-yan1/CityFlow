package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginGateLocksAccountThenClearsOnSuccess(t *testing.T) {
	gate := newLoginGate()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for i := 0; i < loginAccountFailureLimit; i++ {
		gate.failed("User@Example.com", "203.0.113.7", now)
	}
	if wait, blocked := gate.blocked("user@example.com", "203.0.113.7", now); !blocked || wait <= 0 {
		t.Fatalf("blocked=%v wait=%s, want a positive wait", blocked, wait)
	}
	gate.succeeded("USER@example.com")
	if _, blocked := gate.blocked("user@example.com", "203.0.113.7", now); blocked {
		t.Fatal("successful login must clear the account bucket")
	}
}

func TestLoginGateExpiresAfterWindow(t *testing.T) {
	gate := newLoginGate()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for i := 0; i < loginAccountFailureLimit; i++ {
		gate.failed("user@example.com", "203.0.113.7", now)
	}
	if _, blocked := gate.blocked("user@example.com", "203.0.113.7", now.Add(loginAccountFailureWindow)); blocked {
		t.Fatal("bucket must expire after the window")
	}
}

func TestLoginGateLocksSourceIPAcrossAccounts(t *testing.T) {
	gate := newLoginGate()
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	for i := 0; i < loginIPFailureLimit; i++ {
		gate.failed("user"+string(rune('a'+i))+"@example.com", "198.51.100.9", now)
	}
	if _, blocked := gate.blocked("fresh@example.com", "198.51.100.9", now); !blocked {
		t.Fatal("source IP must be locked after the IP budget is spent")
	}
	if _, blocked := gate.blocked("fresh@example.com", "198.51.100.10", now); blocked {
		t.Fatal("a different source IP must not inherit the lock")
	}
}

func TestClientIPIgnoresForwardedHeaderFromPublicPeer(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.RemoteAddr = "198.51.100.20:34567"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := clientIP(request); got != "198.51.100.20" {
		t.Fatalf("clientIP=%q, want public peer address", got)
	}
}

func TestClientIPUsesForwardedHeaderFromLoopbackProxy(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/auth/login", nil)
	request.RemoteAddr = "127.0.0.1:34567"
	request.Header.Set("X-Forwarded-For", "203.0.113.99, 10.0.0.1")
	if got := clientIP(request); got != "203.0.113.99" {
		t.Fatalf("clientIP=%q, want first forwarded hop", got)
	}
}
