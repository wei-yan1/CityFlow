package mysqlstore

import (
	"context"
	"os"
	"testing"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
)

func TestPaymentRepositoryMySQL(t *testing.T) {
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
	account, err := store.CreateUser(ctx, user.CreateRequest{
		Email: suffix + "@payment.test", Password: "integration-test-password", Name: "支付测试", Role: "USER",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM users WHERE id=?", account.ID)

	amount := int64(1234)
	item, err := store.Create(ctx, task.CreateRequest{
		Title: "支付集成测试", Type: "DELIVERY", Pickup: "A", Destination: "B", QuotedAmountFen: &amount,
	}, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM tasks WHERE id=?", item.ID)

	orderNo := "CF" + suffix
	order, err := store.CreatePaymentOrder(ctx, payment.Order{
		ID: suffix, TaskID: item.ID, PayerID: account.ID, AmountFen: amount,
		Currency: payment.CurrencyCNY, Status: payment.PaymentPending,
		Provider: payment.ProviderMock, ProviderOrderNo: orderNo, IdempotencyKey: "key-" + suffix,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM payment_orders WHERE id=?", order.ID)

	found, err := store.PaymentOrderByProviderNo(ctx, payment.ProviderMock, orderNo)
	if err != nil || found.ID != order.ID {
		t.Fatalf("provider order lookup: %+v %v", found, err)
	}
	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	if err := store.SaveChargePayload(ctx, order.ID, "https://qr.example/abc", "https://qr.example/abc", expiresAt); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.PaymentOrder(ctx, order.ID)
	if err != nil || loaded.PayPayload != "https://qr.example/abc" || loaded.ExpiresAt == nil {
		t.Fatalf("charge payload was not persisted: %+v %v", loaded, err)
	}
	paid, err := store.MarkPaymentPaid(ctx, order.ID, "MOCK-TRADE-"+suffix, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != payment.PaymentPaid || paid.PaidAt == nil {
		t.Fatalf("order was not marked paid: %+v", paid)
	}
	replayed, err := store.MarkPaymentPaid(ctx, order.ID, "MOCK-TRADE-"+suffix, time.Now().UTC())
	if err != nil || replayed.Status != payment.PaymentPaid {
		t.Fatalf("repeated paid callback must be idempotent: %+v %v", replayed, err)
	}

	settlementID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	settlement, err := store.CreateSettlement(ctx, payment.Settlement{
		ID: settlementID, TaskID: item.ID, PaymentOrderID: paid.ID, RiderID: account.ID,
		AmountFen: amount, Currency: payment.CurrencyCNY, Status: payment.SettlementPending,
		Provider: payment.ProviderMock, ProviderBillNo: "ST" + suffix, IdempotencyKey: "task:" + item.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM rider_settlements WHERE id=?", settlement.ID)

	secondStore, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer secondStore.Close()
	start := make(chan struct{})
	type claimResult struct {
		items []*payment.Settlement
		err   error
	}
	claims := make(chan claimResult, 2)
	for _, claimant := range []*Store{store, secondStore} {
		go func(claimant *Store) {
			<-start
			items, err := claimant.ClaimDueSettlements(ctx, time.Now().UTC(), 10, time.Minute)
			claims <- claimResult{items: items, err: err}
		}(claimant)
	}
	close(start)
	first, second := <-claims, <-claims
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent claims: first=%v second=%v", first.err, second.err)
	}
	claimed := make(map[string]bool)
	for _, result := range []claimResult{first, second} {
		for _, item := range result.items {
			if claimed[item.ID] {
				t.Fatalf("settlement %s was claimed by both Payment instances", item.ID)
			}
			claimed[item.ID] = true
		}
	}
	if !claimed[settlement.ID] {
		t.Fatalf("due settlement %s was not claimed", settlement.ID)
	}
	claimedSettlement, err := store.Settlement(ctx, settlement.ID)
	if err != nil || claimedSettlement.Status != payment.SettlementProcessing {
		t.Fatalf("claimed settlement state: %+v %v", claimedSettlement, err)
	}
	settled, err := store.MarkSettlementPaid(ctx, settlement.ID, "MOCK-BILL-"+suffix, time.Now().UTC())
	if err != nil || settled.Status != payment.SettlementPaid || settled.ProviderPayoutID == "" {
		t.Fatalf("settlement payout: %+v %v", settled, err)
	}

	saved, err := store.SavePayoutAccount(ctx, payment.PayoutAccount{
		RiderID: account.ID, Provider: payment.ProviderAlipay, Account: "rider@alipay.test", AccountName: "测试骑手",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM rider_payout_accounts WHERE rider_id=?", account.ID)
	if saved.Provider != payment.ProviderAlipay || saved.Account != "rider@alipay.test" {
		t.Fatalf("payout account save: %+v", saved)
	}
	updated, err := store.SavePayoutAccount(ctx, payment.PayoutAccount{
		RiderID: account.ID, Provider: payment.ProviderWechat, Account: "openid-123", AccountName: "测试骑手",
	})
	if err != nil || updated.Provider != payment.ProviderWechat {
		t.Fatalf("payout account upsert: %+v %v", updated, err)
	}
	stored, err := store.PayoutAccount(ctx, account.ID)
	if err != nil || stored.Account != "openid-123" {
		t.Fatalf("payout account read: %+v %v", stored, err)
	}
}

func TestPaymentInboxMySQL(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}

	eventID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM payment_inbox WHERE event_id=?", eventID)
	claimed, err := store.ClaimInbox(ctx, eventID, "task.completed.v1", []byte(`{"id":"task-1","status":"COMPLETED"}`))
	if err != nil || !claimed {
		t.Fatalf("claim inbox: claimed=%v err=%v", claimed, err)
	}
	var received int64
	if err := store.orm.Raw("SELECT COUNT(*) FROM payment_inbox WHERE event_id=? AND received_at > '2000-01-01'", eventID).Scan(&received).Error; err != nil {
		t.Fatal(err)
	}
	if received != 1 {
		t.Fatalf("payment inbox persisted a zero received_at")
	}
	duplicate, err := store.ClaimInbox(ctx, eventID, "task.completed.v1", []byte(`{"id":"task-1","status":"COMPLETED"}`))
	if err != nil || duplicate {
		t.Fatalf("processing duplicate must not be claimed: duplicate=%v err=%v", duplicate, err)
	}
	if err := store.MarkInboxProcessed(ctx, eventID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if duplicate, err = store.ClaimInbox(ctx, eventID, "task.completed.v1", []byte(`{"id":"task-1","status":"COMPLETED"}`)); err != nil || duplicate {
		t.Fatalf("processed duplicate must not be claimed: duplicate=%v err=%v", duplicate, err)
	}
}

// A consumer can die between ClaimInbox and the business transaction. With a
// persisted payload, the row must become claimable again after the lease and
// return the original bytes to the recovery worker.
func TestPaymentInboxMySQLRecoversStaleProcessing(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateAllServices(ctx); err != nil {
		t.Fatal(err)
	}

	eventID, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	defer store.orm.Exec("DELETE FROM payment_inbox WHERE event_id=?", eventID)
	payload := []byte(`{"id":"task-recover","status":"COMPLETED"}`)
	claimed, err := store.ClaimInbox(ctx, eventID, "task.completed.v1", payload)
	if err != nil || !claimed {
		t.Fatalf("claim inbox: claimed=%v err=%v", claimed, err)
	}
	// Simulate the consumer crashing after the claim: leave PROCESSING and let
	// the row age past the recovery lease.
	if err := store.orm.Exec("UPDATE payment_inbox SET updated_at=? WHERE event_id=?", time.Now().UTC().Add(-10*time.Minute), eventID).Error; err != nil {
		t.Fatal(err)
	}
	events, err := store.ClaimRecoverableInbox(ctx, time.Now().UTC().Add(-5*time.Minute), 20, 8)
	if err != nil {
		t.Fatal(err)
	}
	var recovered *repository.InboxEvent
	for idx := range events {
		if events[idx].EventID == eventID {
			recovered = &events[idx]
			break
		}
	}
	if recovered == nil {
		t.Fatalf("stale PROCESSING event was not claimed for recovery: %+v", events)
	}
	if string(recovered.Payload) != string(payload) {
		t.Fatalf("recovery lost payload: %s", recovered.Payload)
	}
	if recovered.Attempts != 2 {
		t.Fatalf("expected second attempt, got %d", recovered.Attempts)
	}
	var status string
	if err := store.orm.Raw("SELECT status FROM payment_inbox WHERE event_id=?", eventID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != "PROCESSING" {
		t.Fatalf("recovered event status = %s", status)
	}
	if err := store.MarkInboxProcessed(ctx, eventID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}
