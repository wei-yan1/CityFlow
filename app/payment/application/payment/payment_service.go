package paymentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cityflow/app/payment/domain/payment"
	"cityflow/app/payment/repository"
	"cityflow/internal/common/domain"
)

// TaskReadLister is the Task fact Payment needs to price a charge and reconcile
// settlements. Payment declares it locally instead of importing Task's store.
type TaskReadLister interface {
	Get(context.Context, string) (*TaskView, error)
	List(context.Context, string, repository.Page) ([]*TaskView, error)
}

type PaymentService struct {
	orders       repository.PaymentRepository
	tasks        TaskReadLister
	coupons      CouponGate
	inbox        repository.PaymentInboxRepository
	notifyBuffer *NotifyLogBuffer
	gateways     map[string]payment.Gateway
	provider     string
	feeBPS       int64
	expiry       time.Duration
	allowMockPay bool
	maxAttempts  int
	now          func() time.Time

	lastInboxPurge time.Time
}

const (
	inboxPurgeEvery = 5 * time.Minute
	inboxRetention  = 30 * 24 * time.Hour
	inboxPurgeBatch = 1000

	inboxRecoveryLease    = 5 * time.Minute
	inboxRecoveryBatch    = 20
	inboxRecoveryInterval = 30 * time.Second
	inboxMaxAttempts      = 8
)

// CouponGate is the narrow payment-side contract. The coupon service owns
// inventory and claim state; payment only asks it to reserve and redeem a
// claim when a charge is actually created.
// CouponQuote is the payment service's local view of a Promotion quote. The
// coupon domain belongs to Promotion; Payment must not import it.
type CouponQuote struct {
	ClaimID           string
	CouponID          string
	CouponTitle       string
	OriginalAmountFen int64
	DiscountFen       int64
	PayableAmountFen  int64
}

type CouponGate interface {
	QuoteForTask(context.Context, string, string, string) (*CouponQuote, error)
	LockForTask(context.Context, string, string, string) (*CouponQuote, error)
	MarkUsed(context.Context, string, string, string, int64) error
	Release(context.Context, string, string)
}

func NewPaymentService(orders repository.PaymentRepository, tasks TaskReadLister, gateways []payment.Gateway, provider string) *PaymentService {
	registry := make(map[string]payment.Gateway, len(gateways))
	for _, gateway := range gateways {
		if gateway != nil {
			registry[gateway.Name()] = gateway
		}
	}
	if provider == "" {
		provider = payment.ProviderMock
	}
	return &PaymentService{
		orders: orders, tasks: tasks, gateways: registry, provider: strings.ToUpper(provider),
		feeBPS: 0, expiry: 30 * time.Minute, allowMockPay: true, maxAttempts: 8,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *PaymentService) WithExpiry(expiry time.Duration) *PaymentService {
	if expiry > 0 {
		s.expiry = expiry
	}
	return s
}

func (s *PaymentService) WithFeeBPS(feeBPS int64) *PaymentService {
	if feeBPS >= 0 && feeBPS <= 10000 {
		s.feeBPS = feeBPS
	}
	return s
}

func (s *PaymentService) WithMockPay(allow bool) *PaymentService {
	s.allowMockPay = allow
	return s
}

func (s *PaymentService) WithClock(now func() time.Time) *PaymentService {
	if now != nil {
		s.now = now
	}
	return s
}

// WithInbox installs the durable consumer inbox. Without it the service still
// processes events, but duplicate deliveries are not filtered.
func (s *PaymentService) WithInbox(inbox repository.PaymentInboxRepository) *PaymentService {
	s.inbox = inbox
	return s
}

// WithNotifyBuffer batches the append-only provider callback audit log. The
// order state transition remains inside HandleNotify.
func (s *PaymentService) WithNotifyBuffer(buffer *NotifyLogBuffer) *PaymentService {
	s.notifyBuffer = buffer
	return s
}

func (s *PaymentService) StartNotifyBuffer() {
	if s.notifyBuffer != nil {
		s.notifyBuffer.Start()
	}
}

func (s *PaymentService) StopNotifyBuffer() {
	if s.notifyBuffer != nil {
		s.notifyBuffer.Close()
	}
}

func (s *PaymentService) WithCoupons(gate CouponGate) *PaymentService {
	s.coupons = gate
	return s
}

type CreateChargeInput struct {
	TaskID         string
	PayerID        string
	Provider       string
	UserCouponID   string
	IdempotencyKey string
}

type ChargeResult struct {
	Payment     *payment.PaymentOrder `json:"payment"`
	QRCode      string                `json:"qr_code,omitempty"`
	PayURL      string                `json:"pay_url,omitempty"`
	ExpiresAt   *time.Time            `json:"expires_at,omitempty"`
	AlreadyPaid bool                  `json:"already_paid"`
}

type PaymentSummary struct {
	Payment    *payment.PaymentOrder `json:"payment"`
	Settlement *payment.Settlement   `json:"settlement"`
}

func (s *PaymentService) gateway(name string) (payment.Gateway, error) {
	gateway, ok := s.gateways[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown provider %q", domain.ErrInvalidRequest, name)
	}
	if !gateway.Configured() {
		return nil, fmt.Errorf("%w: provider %s has no credentials yet", payment.ErrNotConfigured, name)
	}
	return gateway, nil
}
