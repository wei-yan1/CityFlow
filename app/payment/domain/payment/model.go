// Package payment defines the platform's payment and rider-settlement model.
// Money movement only happens through a provider gateway; records in this
// package are bookkeeping until a verified provider notification arrives.
package payment

import "time"

const CurrencyCNY = "CNY"

// Provider names are stored on orders and settlements so a record always
// identifies which external system it belongs to.
const (
	ProviderMock   = "MOCK"
	ProviderAlipay = "ALIPAY"
	ProviderWechat = "WECHAT"
)

const (
	PaymentPending       = "PENDING"
	PaymentPaid          = "PAID"
	PaymentFailed        = "FAILED"
	PaymentClosed        = "CLOSED"
	PaymentRefundPending = "REFUND_PENDING"
	PaymentRefundFailed  = "REFUND_FAILED"
	PaymentRefunded      = "REFUNDED"
)

const (
	SettlementPending    = "PENDING"
	SettlementProcessing = "PROCESSING"
	SettlementPaid       = "PAID"
	SettlementFailed     = "FAILED"
	// SettlementUnknown means the provider may have accepted the transfer but
	// the final state cannot be confirmed. It blocks refunds until reconciled.
	SettlementUnknown = "UNKNOWN"
)

// PaymentOrder is the money-in record. Amounts are positive integers in fen.
// PayPayload is the string a client renders as a QR code (for example a
// weixin:// or https://qr.alipay.com payload); it is never a price.
type PaymentOrder struct {
	ProviderOrderNo   string     `json:"provider_order_no,omitempty"`
	PayPayload        string     `json:"pay_payload,omitempty"`
	PayURL            string     `json:"pay_url,omitempty"`
	CouponClaimID     string     `json:"coupon_claim_id,omitempty"`
	CouponTitle       string     `json:"coupon_title,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	LastNotifyAt      *time.Time `json:"last_notify_at,omitempty"`
	ID                string     `json:"id"`
	TaskID            string     `json:"task_id"`
	PayerID           string     `json:"payer_id"`
	AmountFen         int64      `json:"amount_fen"`
	OriginalAmountFen int64      `json:"original_amount_fen"`
	DiscountFen       int64      `json:"discount_fen"`
	Currency          string     `json:"currency"`
	Status            string     `json:"status"`
	Provider          string     `json:"provider,omitempty"`
	ProviderTradeID   string     `json:"provider_trade_id,omitempty"`
	FailReason        string     `json:"fail_reason,omitempty"`
	RefundReason      string     `json:"refund_reason,omitempty"`
	NotifyCount       int        `json:"notify_count"`
	IdempotencyKey    string     `json:"-"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// Settlement is the money-out record. It is created only after the task is
// completed and a payment order is PAID, and it is never merged with the
// payment status.
type Settlement struct {
	ProviderBillNo   string     `json:"provider_bill_no,omitempty"`
	FailReason       string     `json:"fail_reason,omitempty"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	LockedUntil      *time.Time `json:"-"`
	PaidAt           *time.Time `json:"paid_at,omitempty"`
	ID               string     `json:"id"`
	TaskID           string     `json:"task_id"`
	PaymentOrderID   string     `json:"payment_order_id"`
	RiderID          string     `json:"rider_id"`
	AmountFen        int64      `json:"amount_fen"`
	FeeFen           int64      `json:"fee_fen"`
	Currency         string     `json:"currency"`
	Status           string     `json:"status"`
	Provider         string     `json:"provider,omitempty"`
	ProviderPayoutID string     `json:"provider_payout_id,omitempty"`
	Attempts         int        `json:"attempts"`
	IdempotencyKey   string     `json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// PayoutAccount is the rider's receiving account at the provider. It is kept
// outside the rider profile so payment details never leak into listing APIs.
type PayoutAccount struct {
	RiderID     string    `json:"rider_id"`
	Provider    string    `json:"provider"`
	Account     string    `json:"account,omitempty"`
	AccountName string    `json:"account_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PayoutAccountRequest is the write shape; Account is never echoed by list APIs.
type PayoutAccountRequest struct {
	Provider    string `json:"provider"`
	Account     string `json:"account"`
	AccountName string `json:"account_name"`
}

// NotifyLog keeps the provider callback trail for replay and reconciliation.
type NotifyLog struct {
	Provider        string    `json:"provider"`
	ProviderOrderNo string    `json:"provider_order_no"`
	ProviderTradeID string    `json:"provider_trade_id"`
	SignatureValid  bool      `json:"signature_valid"`
	Status          string    `json:"status"`
	Result          string    `json:"result"`
	Payload         string    `json:"-"`
	ReceivedAt      time.Time `json:"received_at"`
}

func ValidProvider(value string) bool {
	switch value {
	case ProviderMock, ProviderAlipay, ProviderWechat:
		return true
	default:
		return false
	}
}

// CanTransitionPayment is the single allowed payment state machine. Repeating
// a transition to the same state is treated as idempotent by the repository.
func CanTransitionPayment(from, to string) bool {
	switch from {
	case PaymentPending:
		return to == PaymentPaid || to == PaymentFailed || to == PaymentClosed
	case PaymentPaid:
		return to == PaymentRefundPending || to == PaymentRefunded
	case PaymentRefundPending:
		return to == PaymentRefunded || to == PaymentRefundFailed
	case PaymentRefundFailed:
		return to == PaymentRefundPending || to == PaymentRefunded
	default:
		return false
	}
}

// SamePaymentRequest reports whether an existing payment order was created for
// the same business request. It is used to reject an idempotency key or an
// active payment that belongs to a different task or amount, instead of
// silently handing the caller somebody else's QR code.
func SamePaymentRequest(existing, candidate PaymentOrder) bool {
	return existing.TaskID == candidate.TaskID &&
		existing.PayerID == candidate.PayerID &&
		existing.AmountFen == candidate.AmountFen &&
		existing.CouponClaimID == candidate.CouponClaimID &&
		existing.Provider == candidate.Provider &&
		existing.Currency == candidate.Currency
}

// CanTransitionSettlement allows retries because payouts move between PENDING,
// PROCESSING and FAILED while a transfer is being attempted.
func CanTransitionSettlement(from, to string) bool {
	switch from {
	case SettlementPending:
		return to == SettlementProcessing || to == SettlementFailed
	case SettlementProcessing:
		return to == SettlementPaid || to == SettlementFailed || to == SettlementPending || to == SettlementUnknown
	case SettlementFailed:
		return to == SettlementPending
	default:
		return false
	}
}

// SplitAmount returns the platform fee and the rider payout in fen. A zero
// fee rate pays the rider the full quoted amount, which is the default.
func SplitAmount(amountFen, feeBPS int64) (payoutFen, feeFen int64) {
	if amountFen <= 0 {
		return 0, 0
	}
	if feeBPS < 0 {
		feeBPS = 0
	}
	if feeBPS > 10000 {
		feeBPS = 10000
	}
	feeFen = amountFen * feeBPS / 10000
	payoutFen = amountFen - feeFen
	return payoutFen, feeFen
}
