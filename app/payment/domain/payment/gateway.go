package payment

import (
	"context"
	"errors"
	"time"
)

// ErrNotConfigured means the selected provider is known but its credentials or
// callback URL are not filled in yet. Callers must surface it as 503 rather
// than pretending a payment happened.
var ErrNotConfigured = errors.New("payment provider is not configured")

type ChargeRequest struct {
	Subject   string
	OrderNo   string
	TaskID    string
	PayerID   string
	AmountFen int64
	ExpiresAt time.Time
}

// Charge is the result of creating a payment at the provider. QRCode is the
// content a client renders into a QR image; it is not a URL to open.
type Charge struct {
	Provider    string
	OrderNo     string
	TradeID     string
	QRCode      string
	PayURL      string
	ExpiresAt   time.Time
	AlreadyPaid bool
}

type ChargeState struct {
	OrderNo   string
	TradeID   string
	Status    string
	AmountFen int64
	PaidAt    *time.Time
}

type PayoutRequest struct {
	BillNo    string
	OrderNo   string
	RiderID   string
	AmountFen int64
	Account   PayoutAccount
	Remark    string
}

type Payout struct {
	BillNo     string
	Status     string
	FailReason string
}

type RefundRequest struct {
	OrderNo   string
	RefundNo  string
	TradeID   string
	AmountFen int64
	Reason    string
}

type Refund struct {
	RefundID   string
	Status     string
	FailReason string
}

// Notification is a verified provider callback. ParseNotify must reject
// anything whose signature does not check out; it returns ErrNotConfigured or
// a descriptive error and the caller records the failure.
//
// Kind separates a payment callback from a payout callback: the two flows carry
// different identifiers and must never be matched by each other's keys.
type Notification struct {
	Kind      string
	OrderNo   string
	BillNo    string
	RefundNo  string
	TradeID   string
	AmountFen int64
	Status    string
	PaidAt    *time.Time
	Raw       []byte
}

const (
	NotifyKindCharge = "charge"
	NotifyKindPayout = "payout"
	NotifyKindRefund = "refund"
)

// Gateway is the port every provider adapter implements. Implementations must
// be safe for concurrent use and must never return success for an unverified
// notification.
type Gateway interface {
	Name() string
	Configured() bool
	Precreate(context.Context, ChargeRequest) (*Charge, error)
	Query(context.Context, string) (*ChargeState, error)
	ParseNotify(context.Context, map[string]string, []byte) (*Notification, error)
	Transfer(context.Context, PayoutRequest) (*Payout, error)
	Refund(context.Context, RefundRequest) (*Refund, error)
}

// The interfaces below are optional capabilities. A gateway that cannot
// support close, refund query or payout query is still usable; the service
// simply skips that reconciliation path and reports it in the gap list.
type Closer interface {
	Close(context.Context, string) error
}

type RefundQuerier interface {
	QueryRefund(context.Context, string, string) (*Refund, error)
}

type PayoutQuerier interface {
	QueryPayout(context.Context, string) (*Payout, error)
}
