package riderregservice_test

import (
	"context"
	"errors"
	"testing"

	riderregservice "cityflow/app/identity/application/riderreg"
	"cityflow/app/identity/domain/rider"
	"cityflow/internal/common/domain"
	"cityflow/internal/testfixture/memory"
)

func validRegistration(email, reference string) rider.RegistrationRequest {
	return rider.RegistrationRequest{
		Email: email, Password: "rider-pass-123", DisplayName: "测试骑手",
		Phone: "13900000001", VehicleType: "E_BIKE", Skills: []string{"DELIVERY"},
		ServiceArea: "测试区域", MaxDistanceMeters: 5000, MaxConcurrentTasks: 3,
		PaidAmountFen: 1, PaymentReference: reference, PaymentNote: "支付宝付款备注",
	}
}

func TestRiderRegistrationRequiresOneCentAndApproval(t *testing.T) {
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	registrations := riderregservice.NewRiderRegistrationService(store)
	ctx := context.Background()

	request := validRegistration("rider-apply@cityflow.local", "ALI-REF-001")
	request.PaidAmountFen = 0
	if _, err := registrations.Submit(ctx, request); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("expected invalid request for zero payment, got %v", err)
	}

	request = validRegistration("rider-apply@cityflow.local", "ALI-REF-001")
	created, err := registrations.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != rider.RegistrationPending || created.PaidAmountFen != 1 {
		t.Fatalf("unexpected registration: %+v", created)
	}
	if _, err := registrations.Submit(ctx, validRegistration("rider-apply@cityflow.local", "ALI-REF-002")); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected duplicate email conflict, got %v", err)
	}

	approved, err := registrations.Approve(ctx, created.ID, "admin-test", "已核对支付宝账单")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != rider.RegistrationApproved || approved.UserID == "" {
		t.Fatalf("unexpected approved registration: %+v", approved)
	}
	account, err := store.Authenticate(ctx, request.Email, request.Password)
	if err != nil {
		t.Fatalf("approved rider cannot log in: %v", err)
	}
	if account.Role != "RIDER" || account.Status != "ACTIVE" {
		t.Fatalf("unexpected account: %+v", account)
	}
	profile, err := store.RiderForUser(ctx, account.ID)
	if err != nil {
		t.Fatalf("approved rider profile missing: %v", err)
	}
	if profile.Status != "OFFLINE" || profile.VehicleType != "E_BIKE" {
		t.Fatalf("unexpected rider profile: %+v", profile)
	}
	if _, err := registrations.Approve(ctx, created.ID, "admin-test", "重复开通"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected approved registration conflict, got %v", err)
	}
}

func TestRiderRegistrationReject(t *testing.T) {
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	registrations := riderregservice.NewRiderRegistrationService(store)
	created, err := registrations.Submit(context.Background(), validRegistration("rider-reject@cityflow.local", "ALI-REF-REJECT"))
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := registrations.Reject(context.Background(), created.ID, "admin-test", "未找到付款")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != rider.RegistrationRejected || rejected.ReviewNote != "未找到付款" {
		t.Fatalf("unexpected rejected registration: %+v", rejected)
	}
	if _, err := store.Authenticate(context.Background(), "rider-reject@cityflow.local", "rider-pass-123"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("rejected rider should not have an account, got %v", err)
	}
	reapplied, err := registrations.Submit(context.Background(), validRegistration("rider-reject@cityflow.local", "ALI-REF-REJECT-2"))
	if err != nil {
		t.Fatalf("rejected applicant should be able to reapply with a new reference: %v", err)
	}
	if reapplied.Status != rider.RegistrationPending || reapplied.PaymentReference != "ALI-REF-REJECT-2" {
		t.Fatalf("unexpected reapplication: %+v", reapplied)
	}
}
