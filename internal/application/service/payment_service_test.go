package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/task"
	"cityflow/internal/infrastructure/memory"
)

type fakeGateway struct {
	name         string
	notification *payment.Notification
	queryState   *payment.ChargeState
	queryErr     error
	closeErr     error
	closed       bool
	refund       *payment.Refund
	refundQuery  *payment.Refund
	payoutQuery  *payment.Payout
	payoutErr    error
	transferErr  error
}

func (f *fakeGateway) Name() string     { return f.name }
func (f *fakeGateway) Configured() bool { return true }

func (f *fakeGateway) Precreate(_ context.Context, request payment.ChargeRequest) (*payment.Charge, error) {
	return &payment.Charge{Provider: f.name, OrderNo: request.OrderNo, QRCode: "fake://" + request.OrderNo, ExpiresAt: request.ExpiresAt}, nil
}

func (f *fakeGateway) Query(_ context.Context, orderNo string) (*payment.ChargeState, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.queryState != nil {
		state := *f.queryState
		if state.OrderNo == "" {
			state.OrderNo = orderNo
		}
		return &state, nil
	}
	return &payment.ChargeState{OrderNo: orderNo, Status: payment.PaymentPending}, nil
}

func (f *fakeGateway) ParseNotify(_ context.Context, _ map[string]string, _ []byte) (*payment.Notification, error) {
	if f.notification == nil {
		return nil, payment.ErrNotConfigured
	}
	notification := *f.notification
	return &notification, nil
}

func (f *fakeGateway) Transfer(_ context.Context, request payment.PayoutRequest) (*payment.Payout, error) {
	if f.transferErr != nil {
		return nil, f.transferErr
	}
	return &payment.Payout{BillNo: request.BillNo, Status: payment.SettlementPaid}, nil
}

func (f *fakeGateway) Refund(_ context.Context, request payment.RefundRequest) (*payment.Refund, error) {
	if f.refund != nil {
		refund := *f.refund
		if refund.RefundID == "" {
			refund.RefundID = request.RefundNo
		}
		return &refund, nil
	}
	return &payment.Refund{RefundID: request.RefundNo, Status: payment.PaymentRefunded}, nil
}

func (f *fakeGateway) Close(context.Context, string) error {
	if f.closeErr != nil {
		return f.closeErr
	}
	f.closed = true
	return nil
}

func (f *fakeGateway) QueryRefund(context.Context, string, string) (*payment.Refund, error) {
	if f.refundQuery != nil {
		refund := *f.refundQuery
		return &refund, nil
	}
	return &payment.Refund{Status: payment.PaymentRefunded}, nil
}

func (f *fakeGateway) QueryPayout(_ context.Context, billNo string) (*payment.Payout, error) {
	if f.payoutErr != nil {
		return nil, f.payoutErr
	}
	if f.payoutQuery != nil {
		payout := *f.payoutQuery
		if payout.BillNo == "" {
			payout.BillNo = billNo
		}
		return &payout, nil
	}
	return &payment.Payout{BillNo: billNo, Status: payment.SettlementPaid}, nil
}

func newPaymentTestService(t *testing.T) (*memory.Store, *PaymentService, *fakeGateway) {
	t.Helper()
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	gateway := &fakeGateway{name: payment.ProviderMock}
	return store, NewPaymentService(store, store, []payment.Gateway{gateway}, payment.ProviderMock), gateway
}

func createQuotedTask(t *testing.T, store *memory.Store, amount int64) *task.Task {
	t.Helper()
	item, err := store.Create(context.Background(), task.CreateRequest{
		Title: "代取快递", Type: "DELIVERY", Pickup: "快递点", Destination: "宿舍", QuotedAmountFen: &amount,
	}, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestNotifyAmountMismatchIsRejectedAndReplayIsIdempotent(t *testing.T) {
	store, payments, gateway := newPaymentTestService(t)
	item := createQuotedTask(t, store, 1000)

	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if charge.Order.Status != payment.PaymentPending || charge.QRCode == "" {
		t.Fatalf("unexpected charge: %+v", charge)
	}

	gateway.notification = &payment.Notification{
		Kind: payment.NotifyKindCharge, OrderNo: charge.Order.ProviderOrderNo,
		TradeID: "TRADE-1", AmountFen: 500, Status: payment.PaymentPaid,
	}
	if err := payments.HandleNotify(context.Background(), "mock", nil, []byte("{}")); err == nil {
		t.Fatal("a mismatched amount must be rejected")
	}
	stored, err := store.PaymentOrder(context.Background(), charge.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.PaymentPending {
		t.Fatalf("a rejected notification must not change the order: %s", stored.Status)
	}

	gateway.notification.AmountFen = 1000
	if err := payments.HandleNotify(context.Background(), "mock", nil, []byte("{}")); err != nil {
		t.Fatalf("valid notification: %v", err)
	}
	stored, _ = store.PaymentOrder(context.Background(), charge.Order.ID)
	if stored.Status != payment.PaymentPaid || stored.ProviderTradeID != "TRADE-1" {
		t.Fatalf("valid notification was not applied: %+v", stored)
	}

	if err := payments.HandleNotify(context.Background(), "mock", nil, []byte("{}")); err != nil {
		t.Fatalf("replayed notification must stay idempotent: %v", err)
	}
	stored, _ = store.PaymentOrder(context.Background(), charge.Order.ID)
	if stored.Status != payment.PaymentPaid || stored.NotifyCount != 2 {
		t.Fatalf("replay should be counted but not re-applied: %+v", stored)
	}
}

func TestChargeGateAndRefundLifecycle(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	item := createQuotedTask(t, store, 800)

	if err := payments.EnsureClaimable(context.Background(), item); err == nil {
		t.Fatal("an unpaid quoted task must not be claimable")
	}
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	gateway := payments.gateways[payment.ProviderMock].(*fakeGateway)
	gateway.notification = &payment.Notification{
		Kind: payment.NotifyKindCharge, OrderNo: charge.Order.ProviderOrderNo, AmountFen: 800, Status: payment.PaymentPaid,
	}
	if err := payments.HandleNotify(context.Background(), "mock", nil, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := payments.EnsureClaimable(context.Background(), item); err != nil {
		t.Fatalf("paid task should be claimable: %v", err)
	}
	again, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if !again.AlreadyPaid || again.Order.ID != charge.Order.ID {
		t.Fatalf("a paid task should not create a second order: %+v", again)
	}

	refunded, err := payments.Refund(context.Background(), charge.Order.ID, "user-1", "USER", "用户取消")
	if err != nil {
		t.Fatal(err)
	}
	if refunded.Status != payment.PaymentRefunded {
		t.Fatalf("refund did not complete: %+v", refunded)
	}

	// A second refund is idempotent: the refund number is derived from the
	// order number, so a retry returns the same refunded state.
	refundedAgain, err := payments.Refund(context.Background(), charge.Order.ID, "user-1", "USER", "重复退款")
	if err != nil || refundedAgain.Status != payment.PaymentRefunded {
		t.Fatalf("a repeated refund should be idempotent: %+v err=%v", refundedAgain, err)
	}
}

func TestEnsureSettlementRequiresPaidAndQuote(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	amount := int64(1000)
	item := createQuotedTask(t, store, 1000)
	item.Status = task.StatusCompleted
	item.WorkerID = "00000000-0000-4000-8000-000000000003"

	settlement, err := payments.EnsureSettlement(context.Background(), item)
	if err != nil || settlement != nil {
		t.Fatalf("a task without a paid order must not create a settlement: %+v err=%v", settlement, err)
	}
	settlement, err = payments.EnsureSettlement(context.Background(), &task.Task{ID: item.ID, Status: task.StatusCompleted, QuotedAmountFen: &amount})
	if err != nil || settlement != nil {
		t.Fatalf("a completed task without a worker must not create a settlement: %+v err=%v", settlement, err)
	}
}

func TestSyncPaymentRejectsMismatchedProviderAmount(t *testing.T) {
	store, payments, gateway := newPaymentTestService(t)
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	gateway.queryState = &payment.ChargeState{
		Status: payment.PaymentPaid, AmountFen: 500, TradeID: "TRADE-WRONG",
	}
	if _, err := payments.SyncPayment(context.Background(), charge.Order.ID, "user-1", "USER"); err == nil {
		t.Fatal("a provider amount mismatch must be rejected")
	}
	stored, err := store.PaymentOrder(context.Background(), charge.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.PaymentPending {
		t.Fatalf("a mismatched query must not mark the order paid: %s", stored.Status)
	}
}

func TestReconcileMarksPaidAndClosesExpiredPayment(t *testing.T) {
	store, payments, gateway := newPaymentTestService(t)
	payments.WithExpiry(time.Minute)
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}

	gateway.queryState = &payment.ChargeState{Status: payment.PaymentPaid, AmountFen: 1000, TradeID: "TRADE-RECON"}
	payments.now = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }
	payments.ReconcileOnce(context.Background())
	stored, err := store.PaymentOrder(context.Background(), charge.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.PaymentPaid {
		t.Fatalf("reconcile did not mark the payment paid: %s", stored.Status)
	}

	second := createQuotedTask(t, store, 800)
	secondCharge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: second.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	gateway.queryState = &payment.ChargeState{Status: payment.PaymentPending}
	payments.now = func() time.Time { return time.Now().UTC().Add(4 * time.Minute) }
	payments.ReconcileOnce(context.Background())
	stored, err = store.PaymentOrder(context.Background(), secondCharge.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.PaymentClosed || !gateway.closed {
		t.Fatalf("expired payment was not closed at the provider and locally: status=%s closed=%v", stored.Status, gateway.closed)
	}
}

func TestRefundPendingIsReconciledFromProviderQuery(t *testing.T) {
	store, payments, gateway := newPaymentTestService(t)
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payments.SimulatePaid(context.Background(), charge.Order.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	gateway.refund = &payment.Refund{Status: payment.PaymentRefundPending}
	refunded, err := payments.Refund(context.Background(), charge.Order.ID, "user-1", "USER", "用户取消")
	if err != nil {
		t.Fatal(err)
	}
	if refunded.Status != payment.PaymentRefundPending {
		t.Fatalf("provider processing must leave the order refund pending: %s", refunded.Status)
	}
	gateway.refundQuery = &payment.Refund{Status: payment.PaymentRefunded}
	payments.now = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }
	payments.ReconcileOnce(context.Background())
	stored, err := store.PaymentOrder(context.Background(), charge.Order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.PaymentRefunded {
		t.Fatalf("provider refund success was not reconciled: %s", stored.Status)
	}
}

func TestRefundCancelsPendingSettlementBeforeRefunding(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payments.SimulatePaid(context.Background(), charge.Order.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	completed := *item
	completed.Status = task.StatusCompleted
	completed.WorkerID = "00000000-0000-4000-8000-000000000003"
	settlement, err := payments.EnsureSettlement(context.Background(), &completed)
	if err != nil || settlement == nil {
		t.Fatalf("create settlement: %+v %v", settlement, err)
	}
	if settlement.Status != payment.SettlementPending {
		t.Fatalf("unexpected settlement status: %s", settlement.Status)
	}

	refunded, err := payments.Refund(context.Background(), charge.Order.ID, "user-1", "USER", "用户取消")
	if err != nil {
		t.Fatal(err)
	}
	if refunded.Status != payment.PaymentRefunded {
		t.Fatalf("refund did not complete: %s", refunded.Status)
	}
	stored, err := store.Settlement(context.Background(), settlement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.SettlementFailed {
		t.Fatalf("pending payout must be cancelled before the refund, got %s", stored.Status)
	}
}

func TestSettlementRecordsPlatformFee(t *testing.T) {
	store, payments, _ := newPaymentTestService(t)
	payments.WithFeeBPS(1000)
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payments.SimulatePaid(context.Background(), charge.Order.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	completed := *item
	completed.Status = task.StatusCompleted
	completed.WorkerID = "00000000-0000-4000-8000-000000000003"
	settlement, err := payments.EnsureSettlement(context.Background(), &completed)
	if err != nil || settlement == nil {
		t.Fatalf("create settlement: %+v %v", settlement, err)
	}
	if settlement.AmountFen != 900 || settlement.FeeFen != 100 {
		t.Fatalf("settlement split is wrong: payout=%d fee=%d", settlement.AmountFen, settlement.FeeFen)
	}
}

func TestAmbiguousPayoutAtLastAttemptBecomesUnknown(t *testing.T) {
	store, payments, gateway := newPaymentTestService(t)
	payments.maxAttempts = 1
	item := createQuotedTask(t, store, 1000)
	charge, err := payments.CreateCharge(context.Background(), CreateChargeInput{TaskID: item.ID, PayerID: "user-1", Provider: "MOCK"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payments.SimulatePaid(context.Background(), charge.Order.ID, "user-1"); err != nil {
		t.Fatal(err)
	}
	completed := *item
	completed.Status = task.StatusCompleted
	completed.WorkerID = "00000000-0000-4000-8000-000000000003"
	settlement, err := payments.EnsureSettlement(context.Background(), &completed)
	if err != nil || settlement == nil {
		t.Fatalf("create settlement: %+v %v", settlement, err)
	}

	// The transfer timed out; the provider may or may not have paid. Marking it
	// FAILED would let a cancellation refund the user while the rider was paid.
	gateway.transferErr = errors.New("provider timeout")
	gateway.payoutQuery = &payment.Payout{Status: payment.SettlementProcessing}
	if _, err := payments.SettleOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Settlement(context.Background(), settlement.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != payment.SettlementUnknown {
		t.Fatalf("ambiguous payout must end UNKNOWN, got %s (%s)", stored.Status, stored.FailReason)
	}

	// Refunds must be refused while the payout may already have left.
	if _, err := payments.Refund(context.Background(), charge.Order.ID, "user-1", "USER", "退款"); err == nil {
		t.Fatal("refund must be refused when the payout state is unknown")
	}
}

func TestOneActivePaymentPerTask(t *testing.T) {
	store, err := memory.NewStore()
	if err != nil {
		t.Fatal(err)
	}
	item := createQuotedTask(t, store, 1000)
	now := time.Now().UTC()
	seed, err := store.CreatePaymentOrder(context.Background(), payment.Order{
		ID: "00000000-0000-4000-8000-000000000001", TaskID: item.ID, PayerID: "user-1",
		AmountFen: 1000, Currency: payment.CurrencyCNY, Status: payment.PaymentPending,
		Provider: payment.ProviderMock, ProviderOrderNo: "CF-SEED", IdempotencyKey: "primary",
		ExpiresAt: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	makeOrder := func(id, key string) payment.Order {
		return payment.Order{
			ID: id, TaskID: item.ID, PayerID: "user-1", AmountFen: 1000,
			Currency: payment.CurrencyCNY, Status: payment.PaymentPending,
			Provider: payment.ProviderMock, ProviderOrderNo: "CF-" + id, IdempotencyKey: key,
			ExpiresAt: &now,
		}
	}
	var wg sync.WaitGroup
	ids := make([]string, 2)
	for i := range ids {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			order, err := store.CreatePaymentOrder(context.Background(), makeOrder("00000000-0000-4000-8000-00000000000"+string(rune('2'+index)), "key-"+string(rune('1'+index))))
			if err != nil {
				t.Errorf("create order %d: %v", index, err)
				return
			}
			ids[index] = order.ID
		}(i)
	}
	wg.Wait()
	if ids[0] == "" || ids[0] != ids[1] || ids[0] != seed.ID {
		t.Fatalf("concurrent charge requests must share one active order: %v", ids)
	}

	// An idempotency key from another task must not return somebody else's
	// order or create a second active payment.
	other := createQuotedTask(t, store, 1000)
	_, err = store.CreatePaymentOrder(context.Background(), payment.Order{
		ID: "00000000-0000-4000-8000-000000000009", TaskID: other.ID, PayerID: "user-1",
		AmountFen: 1000, Currency: payment.CurrencyCNY, Status: payment.PaymentPending,
		Provider: payment.ProviderMock, ProviderOrderNo: "CF-OTHER", IdempotencyKey: "primary",
		ExpiresAt: &now,
	})
	if err == nil {
		t.Fatal("reusing an idempotency key across tasks must conflict")
	}
}
