package repository

import (
	"context"
	"time"

	"cityflow/internal/domain/payment"
)

// PaymentRepository keeps money-in and money-out records independent. Updates
// are compare-and-set so a delayed provider callback can never resurrect a
// closed order.
type PaymentRepository interface {
	CreatePaymentOrder(context.Context, payment.Order) (*payment.Order, error)
	PaymentOrder(context.Context, string) (*payment.Order, error)
	PaymentOrderByTask(context.Context, string) (*payment.Order, error)
	PaymentOrderByProviderNo(context.Context, string, string) (*payment.Order, error)
	// PendingPayments and RefundPendingPayments return a bounded reconciliation
	// batch: only records that have not changed recently, so a fresh QR code is
	// not queried before the user has had a chance to scan it.
	PendingPayments(context.Context, time.Time, int) ([]*payment.Order, error)
	RefundPendingPayments(context.Context, time.Time, int) ([]*payment.Order, error)
	RefundFailedPayments(context.Context, time.Time, int) ([]*payment.Order, error)
	SaveChargePayload(context.Context, string, string, string, time.Time) error
	MarkPaymentPaid(context.Context, string, string, time.Time) (*payment.Order, error)
	MarkPaymentFailed(context.Context, string, string) (*payment.Order, error)
	ClosePaymentOrder(context.Context, string) (*payment.Order, error)
	MarkPaymentRefundPending(context.Context, string) (*payment.Order, error)
	MarkPaymentRefunded(context.Context, string) (*payment.Order, error)
	MarkPaymentRefundFailed(context.Context, string, string) (*payment.Order, error)
	NotePaymentNotify(context.Context, string, time.Time) error
	RecordNotify(context.Context, payment.NotifyLog) error

	CreateSettlement(context.Context, payment.Settlement) (*payment.Settlement, error)
	Settlement(context.Context, string) (*payment.Settlement, error)
	SettlementByTask(context.Context, string) (*payment.Settlement, error)
	SettlementByProviderBillNo(context.Context, string, string) (*payment.Settlement, error)
	SettlementsByRider(context.Context, string, Page) ([]*payment.Settlement, error)
	ClaimDueSettlements(context.Context, time.Time, int, time.Duration) ([]*payment.Settlement, error)
	MarkSettlementPaid(context.Context, string, string, time.Time) (*payment.Settlement, error)
	MarkSettlementFailed(context.Context, string, string, *time.Time) (*payment.Settlement, error)
	MarkSettlementUnknown(context.Context, string, string) (*payment.Settlement, error)
	CancelPendingSettlement(context.Context, string, string) (*payment.Settlement, error)

	PayoutAccount(context.Context, string) (*payment.PayoutAccount, error)
	SavePayoutAccount(context.Context, payment.PayoutAccount) (*payment.PayoutAccount, error)
}

// PaymentNotifyBatchWriter is an optional capability of a payment store. The
// service uses it when present so notify audit rows can be inserted in one
// round trip instead of one INSERT per channel callback.
type PaymentNotifyBatchWriter interface {
	RecordNotifyBatch(context.Context, []payment.NotifyLog) error
}
