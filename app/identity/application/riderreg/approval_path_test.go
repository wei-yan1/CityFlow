package riderregservice_test

import (
	"context"
	"testing"

	riderregservice "cityflow/app/identity/application/riderreg"
	"cityflow/app/identity/domain/rider"
	"cityflow/app/identity/repository"
)

// accountOnlyStore models the MySQL path: Identity creates the account and
// outbox event, while the rider profile is projected by Task from the event.
// The embedded repository is nil so an accidental fallback call fails loudly.
type accountOnlyStore struct {
	repository.RiderRegistrationRepository
	note string
}

func (s *accountOnlyStore) ApproveRiderRegistrationAccount(_ context.Context, id, adminID, note string) (*rider.Registration, error) {
	s.note = note
	return &rider.Registration{ID: id, UserID: "user-approved", Status: rider.RegistrationApproved}, nil
}

func TestApprovePrefersIdentityAccountOnlyPath(t *testing.T) {
	store := &accountOnlyStore{}
	svc := riderregservice.NewRiderRegistrationService(store)
	approved, err := svc.Approve(context.Background(), "registration-1", "admin-1", "  已核对账单  ")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != rider.RegistrationApproved || approved.UserID != "user-approved" {
		t.Fatalf("unexpected approval: %+v", approved)
	}
	if store.note != "已核对账单" {
		t.Fatalf("note was not trimmed: %q", store.note)
	}
}

func TestApproveRejectsOverlongNoteBeforeCallingStore(t *testing.T) {
	store := &accountOnlyStore{}
	svc := riderregservice.NewRiderRegistrationService(store)
	if _, err := svc.Approve(context.Background(), "registration-1", "admin-1", string(make([]byte, 256))); err == nil {
		t.Fatal("expected an invalid request for an overlong note")
	}
	if store.note != "" {
		t.Fatalf("store should not be called, got note %q", store.note)
	}
}
