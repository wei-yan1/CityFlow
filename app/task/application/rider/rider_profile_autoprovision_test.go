package riderservice

import (
	"context"
	"testing"

	"cityflow/internal/common/auth"
	"cityflow/internal/testfixture/memory"
)

func TestRiderProfileIsProvisionedOnFirstRead(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.CreateAccount(ctx, "new-rider@cityflow.local", "cityflow123", "新骑手", auth.RoleRider)
	if err != nil {
		t.Fatal(err)
	}
	riders := NewRiderService(store, store, nil)
	profile, err := riders.Profile(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.UserID != account.ID || profile.DisplayName != "新骑手" || profile.Status != "AVAILABLE" {
		t.Fatalf("unexpected auto-provisioned profile: %+v", profile)
	}
	// A second read must return the same profile rather than create another.
	again, err := riders.Profile(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != profile.ID {
		t.Fatalf("profile was recreated: %s != %s", again.ID, profile.ID)
	}
}
