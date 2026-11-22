package notificationapp

import (
	"context"
	"testing"
	"time"

	"cityflow/app/notification/internal/domain/notification"
)

type fakeStore struct {
	purgeCalls   int
	readBefore   time.Time
	unreadBefore time.Time
	limit        int
}

func (f *fakeStore) List(context.Context, string, bool, Page) ([]*notification.Notification, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) Unread(context.Context, string) (int, error) { return 0, nil }
func (f *fakeStore) MarkRead(context.Context, string, []string, bool) error {
	return nil
}
func (f *fakeStore) Create(context.Context, *notification.Notification) error { return nil }
func (f *fakeStore) Purge(_ context.Context, readBefore, unreadBefore time.Time, limit int) (int64, error) {
	f.purgeCalls++
	f.readBefore, f.unreadBefore, f.limit = readBefore, unreadBefore, limit
	return 0, nil
}

func TestPurgeRetentionPassesBothWindows(t *testing.T) {
	store := &fakeStore{}
	service := New(store)
	before := time.Now().UTC()
	if _, err := service.PurgeRetention(context.Background(), 90*24*time.Hour, 180*24*time.Hour, 100, 1); err != nil {
		t.Fatal(err)
	}
	if store.purgeCalls != 1 || store.limit != 100 {
		t.Fatalf("calls=%d limit=%d", store.purgeCalls, store.limit)
	}
	if gap := before.Sub(store.readBefore); gap < 89*24*time.Hour || gap > 91*24*time.Hour {
		t.Fatalf("read window=%s", gap)
	}
	if gap := before.Sub(store.unreadBefore); gap < 179*24*time.Hour || gap > 181*24*time.Hour {
		t.Fatalf("unread window=%s", gap)
	}
}

func TestPurgeRetentionRejectsNonPositiveWindow(t *testing.T) {
	service := New(&fakeStore{})
	if _, err := service.PurgeRetention(context.Background(), 0, 180*24*time.Hour, 100, 1); err == nil {
		t.Fatal("expected an error for a zero retention window")
	}
}
