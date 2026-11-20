package mysqlstore

import (
	"context"
	"os"
	"testing"
	"time"

	"cityflow/internal/domain/rider"
)

func TestRiderRegistrationRepositoryMySQL(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}

	suffix, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	email := suffix + "@rider-registration.test"
	reference := "ALI-" + suffix
	registration := rider.Registration{
		ID: suffix, Email: email, Phone: "1390000" + suffix[:4],
		PasswordHash: "test-password-hash", DisplayName: "MySQL 注册骑手",
		VehicleType: "E_BIKE", Skills: []string{"DELIVERY", "PICKUP"}, ServiceArea: "测试区域",
		MaxDistanceMeters: 5000, MaxConcurrentTasks: 3,
		PaidAmountFen: rider.RegistrationMinimumFen, PaymentReference: reference,
		PaymentNote: "支付宝付款", Status: rider.RegistrationPending,
	}
	created, err := store.CreateRiderRegistration(ctx, registration)
	if err != nil {
		t.Fatal(err)
	}
	deferredCleanup := func() {
		store.orm.Exec("DELETE FROM rider_profiles WHERE user_id IN (SELECT id FROM users WHERE email = ?)", email)
		store.orm.Exec("DELETE FROM users WHERE email = ?", email)
		store.orm.Exec("DELETE FROM rider_registrations WHERE id = ?", created.ID)
	}
	defer deferredCleanup()

	exists, err := store.RiderRegistrationExists(ctx, email, registration.Phone)
	if err != nil || !exists {
		t.Fatalf("pending registration should block duplicate email/phone: exists=%v err=%v", exists, err)
	}
	approved, err := store.ApproveRiderRegistration(ctx, created.ID, "admin-mysql-test", "已核对支付宝账单")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != rider.RegistrationApproved || approved.UserID == "" {
		t.Fatalf("unexpected approved registration: %+v", approved)
	}
	profile, err := store.RiderForUser(ctx, approved.UserID)
	if err != nil {
		t.Fatalf("approved rider profile missing: %v", err)
	}
	if profile.Status != "OFFLINE" || profile.VehicleType != "E_BIKE" || profile.DisplayName != "MySQL 注册骑手" {
		t.Fatalf("unexpected rider profile: %+v", profile)
	}
	account, err := store.GetUser(ctx, approved.UserID)
	if err != nil {
		t.Fatalf("approved user missing: %v", err)
	}
	if account.Role != "RIDER" || account.Status != "ACTIVE" {
		t.Fatalf("unexpected account: %+v", account)
	}

	second := registration
	second.ID = ""
	second.Email = suffix + "-reject@rider-registration.test"
	second.Phone = ""
	second.PaymentReference = reference + "-reject"
	rejectedRegistration, err := store.CreateRiderRegistration(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	deferredCleanup()
	defer store.orm.Exec("DELETE FROM rider_registrations WHERE id = ?", rejectedRegistration.ID)
	rejected, err := store.RejectRiderRegistration(ctx, rejectedRegistration.ID, "admin-mysql-test", "未找到付款")
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != rider.RegistrationRejected || rejected.UserID != "" {
		t.Fatalf("unexpected rejected registration: %+v", rejected)
	}
	var rejectedUsers int64
	if err := store.orm.Model(&userRecord{}).Where("email = ?", second.Email).Count(&rejectedUsers).Error; err != nil {
		t.Fatal(err)
	}
	if rejectedUsers != 0 {
		t.Fatalf("rejected registration must not create an account")
	}
}
