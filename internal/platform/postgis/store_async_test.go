package postgis

import (
	"context"
	"testing"
	"time"
)

// NewStore must not block on a database round trip; startup can build the
// fallback pool before the database is reachable and ping it asynchronously.
func TestNewStoreDoesNotConnectEagerly(t *testing.T) {
	store, err := NewStore("postgres://cityflow:change-me@127.0.0.1:1/cityflow_map?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("configuration should parse without a connection: %v", err)
	}
	defer store.Close()
	if err := store.Ping(context.Background(), 300*time.Millisecond); err == nil {
		t.Fatal("ping unexpectedly succeeded against a closed port")
	}
}

func TestNewStoreRejectsInvalidDSN(t *testing.T) {
	if _, err := NewStore("not a dsn"); err == nil {
		t.Fatal("invalid dsn accepted")
	}
}
