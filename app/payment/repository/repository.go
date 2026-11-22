// Package paymentrepository defines the persistence ports owned by Payment.
package repository

import (
	"context"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/internal/common/pagination"
)

// PaymentRepository keeps money-in and money-out records independent. Updates
// are compare-and-set so a delayed provider callback can never resurrect a
// closed order.
type PaymentRepository interface {
	CreatePaymentOrder(context.Context, payment.PaymentOrder) (*payment.PaymentOrder, error)
	PaymentOrder(context.Context, string) (*payment.PaymentOrder, error)
	PaymentOrderByTask(context.Context, string) (*payment.PaymentOrder, error)
	PaymentOrderByProviderNo(context.Context, string, string) (*payment.PaymentOrder, error)
	// PendingPayments and RefundPendingPayments return a bounded reconciliation
	// batch: only records that have not changed recently, so a fresh QR code is
	// not queried before the user has had a chance to scan it.
	PendingPayments(context.Context, time.Time, int) ([]*payment.PaymentOrder, error)
	RefundPendingPayments(context.Context, time.Time, int) ([]*payment.PaymentOrder, error)
	RefundFailedPayments(context.Context, time.Time, int) ([]*payment.PaymentOrder, error)
	SaveChargePayload(context.Context, string, string, string, time.Time) error
	MarkPaymentPaid(context.Context, string, string, time.Time) (*payment.PaymentOrder, error)
	MarkPaymentFailed(context.Context, string, string) (*payment.PaymentOrder, error)
	ClosePaymentOrder(context.Context, string) (*payment.PaymentOrder, error)
	MarkPaymentRefundPending(context.Context, string, string) (*payment.PaymentOrder, error)
	MarkPaymentRefunded(context.Context, string) (*payment.PaymentOrder, error)
	MarkPaymentRefundFailed(context.Context, string, string) (*payment.PaymentOrder, error)
	NotePaymentNotify(context.Context, string, time.Time) error
	RecordNotify(context.Context, payment.NotifyLog) error

	CreateSettlement(context.Context, payment.Settlement) (*payment.Settlement, error)
	Settlement(context.Context, string) (*payment.Settlement, error)
	SettlementByTask(context.Context, string) (*payment.Settlement, error)
	SettlementByProviderBillNo(context.Context, string, string) (*payment.Settlement, error)
	SettlementsByRider(context.Context, string, pagination.Page) ([]*payment.Settlement, error)
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

// PaymentNotifyLogPurger is the optional retention surface for the
// append-only payment callback audit table. Keeping it separate from
// PaymentRepository lets a memory store omit maintenance without breaking
// checkout tests.
type PaymentNotifyLogPurger interface {
	PurgeNotifies(context.Context, time.Time, int) (int64, error)
}

// InboxEvent is a durably stored event that can be replayed after a consumer
// crashes between claiming the inbox row and completing the side effect.
type InboxEvent struct {
	EventID   string
	EventType string
	Payload   []byte
	Attempts  int
}

// PaymentInboxRepository makes event consumption idempotent.
type PaymentInboxRepository interface {
	ClaimInbox(context.Context, string, string, []byte) (bool, error)
	MarkInboxProcessed(context.Context, string, time.Time) error
	MarkInboxFailed(context.Context, string, string) error
	MarkInboxRetrying(context.Context, string, string, time.Time) error
	ClaimRecoverableInbox(context.Context, time.Time, int, int) ([]InboxEvent, error)
	InboxStatus(context.Context, string) (string, error)
	PurgeInbox(context.Context, time.Time, int) (int64, error)
}

type Page = pagination.Page
