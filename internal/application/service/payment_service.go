package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/coupon"
	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/task"
)

type PaymentService struct {
	orders       repository.PaymentRepository
	tasks        taskReadLister
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
	inboxPurgeEvery     = 5 * time.Minute
	inboxRetention      = 30 * 24 * time.Hour
	inboxPurgeBatch     = 1000
	inboxPurgeMaxPasses = 5

	inboxRecoveryLease    = 5 * time.Minute
	inboxRecoveryBatch    = 20
	inboxRecoveryInterval = 30 * time.Second
	inboxMaxAttempts      = 8
)

// CouponGate is the narrow payment-side contract. The coupon service owns
// inventory and claim state; payment only asks it to reserve and redeem a
// claim when a charge is actually created.
type CouponGate interface {
	QuoteForTask(context.Context, string, string, string) (*coupon.Quote, error)
	LockForTask(context.Context, string, string, string) (*coupon.Quote, error)
	MarkUsed(context.Context, string, string, string, int64) error
	Release(context.Context, string, string)
}

func NewPaymentService(orders repository.PaymentRepository, tasks taskReadLister, gateways []payment.Gateway, provider string) *PaymentService {
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
	Order       *payment.Order `json:"payment"`
	QRCode      string         `json:"qr_code,omitempty"`
	PayURL      string         `json:"pay_url,omitempty"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
	AlreadyPaid bool           `json:"already_paid"`
}

type PaymentSummary struct {
	Order      *payment.Order      `json:"payment"`
	Settlement *payment.Settlement `json:"settlement"`
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

// CreateCharge is the money-in entry point. The amount always comes from the
// task's quote on the server; a client can never name its own price.
func (s *PaymentService) CreateCharge(ctx context.Context, input CreateChargeInput) (*ChargeResult, error) {
	provider := strings.ToUpper(strings.TrimSpace(input.Provider))
	if provider == "" {
		provider = s.provider
	}
	if !payment.ValidProvider(provider) {
		return nil, fmt.Errorf("%w: unsupported payment provider", domain.ErrInvalidRequest)
	}
	gateway, err := s.gateway(provider)
	if err != nil {
		return nil, err
	}
	item, err := s.tasks.Get(ctx, input.TaskID)
	if err != nil {
		return nil, err
	}
	if item.CreatedBy != input.PayerID {
		return nil, domain.ErrForbidden
	}
	if item.Status != task.StatusOpen {
		return nil, fmt.Errorf("%w: 只有待接单的任务可以支付", domain.ErrConflict)
	}
	if item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil, fmt.Errorf("%w: 任务未设置报酬金额", domain.ErrInvalidRequest)
	}
	amount := *item.QuotedAmountFen
	var quote *coupon.Quote
	if claimID := strings.TrimSpace(input.UserCouponID); claimID != "" {
		if s.coupons == nil {
			return nil, fmt.Errorf("%w: 优惠券服务未启用", domain.ErrUnavailable)
		}
		quote, err = s.coupons.QuoteForTask(ctx, input.PayerID, claimID, item.ID)
		if err != nil {
			return nil, err
		}
		amount = quote.PayableAmountFen
	}
	now := s.now()

	reuse, result, err := s.existingCharge(ctx, item.ID, provider, amount, quoteClaimID(quote), now, gateway)
	if err != nil {
		return nil, err
	}
	if result != nil {
		return result, nil
	}

	order := reuse
	if order == nil {
		if quote != nil {
			// Reserve the claim only once we know a new order is needed. The
			// lock prevents the same coupon being spent in two browser tabs.
			locked, lockErr := s.coupons.LockForTask(ctx, input.PayerID, quote.UserCoupon.ID, item.ID)
			if lockErr != nil {
				return nil, lockErr
			}
			quote = locked
			amount = locked.PayableAmountFen
		}
		order, err = s.newOrder(ctx, item, provider, amount, input.IdempotencyKey, now, quote)
		if err != nil {
			if quote != nil {
				s.coupons.Release(ctx, quote.UserCoupon.ID, item.ID)
			}
			return nil, err
		}
	}

	charge, err := gateway.Precreate(ctx, payment.ChargeRequest{
		OrderNo: order.ProviderOrderNo, TaskID: item.ID, PayerID: input.PayerID,
		AmountFen: order.AmountFen, Subject: paymentSubject(item),
		ExpiresAt: derefTime(order.ExpiresAt, now.Add(s.expiry)),
	})
	if err != nil {
		if _, markErr := s.orders.MarkPaymentFailed(ctx, order.ID, paymentFailure(err)); markErr != nil {
			log.Printf("warning: mark payment %s failed: %v", order.ID, markErr)
		}
		s.releaseCoupon(ctx, order)
		return nil, fmt.Errorf("%w: 创建支付失败: %v", domain.ErrUnavailable, err)
	}
	expiresAt := charge.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = derefTime(order.ExpiresAt, now.Add(s.expiry))
	}
	if err := s.orders.SaveChargePayload(ctx, order.ID, charge.QRCode, charge.PayURL, expiresAt); err != nil {
		return nil, err
	}
	order.PayPayload, order.PayURL = charge.QRCode, charge.PayURL
	order.ExpiresAt = &expiresAt
	return &ChargeResult{Order: order, QRCode: charge.QRCode, PayURL: charge.PayURL, ExpiresAt: &expiresAt}, nil
}

// existingCharge returns either a reusable pending order or a finished result.
// One live order per task keeps double clicks from creating two QR codes.
func (s *PaymentService) existingCharge(ctx context.Context, taskID, provider string, amount int64, couponClaimID string, now time.Time, gateway payment.Gateway) (*payment.Order, *ChargeResult, error) {
	existing, err := s.orders.PaymentOrderByTask(ctx, taskID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	switch existing.Status {
	case payment.PaymentPaid, payment.PaymentRefundPending:
		return nil, &ChargeResult{
			Order: existing, QRCode: existing.PayPayload, PayURL: existing.PayURL,
			ExpiresAt: existing.ExpiresAt, AlreadyPaid: true,
		}, nil
	case payment.PaymentRefundFailed:
		// The user's money is still at the platform. Creating a second charge
		// here would make them pay twice, so this needs manual resolution.
		return nil, nil, fmt.Errorf("%w: 原支付退款失败，请先联系平台处理", domain.ErrConflict)
	case payment.PaymentPending:
		if existing.AmountFen != amount || existing.Provider != provider || existing.CouponClaimID != couponClaimID {
			if err := s.closeProviderOrder(ctx, gateway, existing); err != nil {
				return nil, nil, err
			}
			return s.afterClosedCharge(ctx, existing)
		}
		if existing.ExpiresAt != nil && !existing.ExpiresAt.After(now) {
			if err := s.closeProviderOrder(ctx, gateway, existing); err != nil {
				return nil, nil, err
			}
			return s.afterClosedCharge(ctx, existing)
		}
		if existing.PayPayload != "" {
			return nil, &ChargeResult{Order: existing, QRCode: existing.PayPayload, PayURL: existing.PayURL, ExpiresAt: existing.ExpiresAt}, nil
		}
		return existing, nil, nil
	default:
		return nil, nil, nil
	}
}

// closeProviderOrder closes the trade at the provider before closing it
// locally. If the provider cannot confirm the close, the caller must not create
// a second QR code: the old code may still be payable.
func (s *PaymentService) closeProviderOrder(ctx context.Context, gateway payment.Gateway, order *payment.Order) error {
	if closer, ok := gateway.(payment.Closer); ok {
		if err := closer.Close(ctx, order.ProviderOrderNo); err != nil {
			return fmt.Errorf("%w: 关闭原支付单失败，请稍后重试: %v", domain.ErrUnavailable, err)
		}
	}
	if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
		return err
	}
	s.releaseCoupon(ctx, order)
	return nil
}

// afterClosedCharge re-reads the order because a callback may have won the
// race while the provider close was in flight. A paid order must be returned as
// already paid instead of creating a second charge.
func (s *PaymentService) afterClosedCharge(ctx context.Context, previous *payment.Order) (*payment.Order, *ChargeResult, error) {
	current, err := s.orders.PaymentOrder(ctx, previous.ID)
	if err != nil {
		return nil, nil, err
	}
	switch current.Status {
	case payment.PaymentPaid, payment.PaymentRefundPending:
		return nil, &ChargeResult{
			Order: current, QRCode: current.PayPayload, PayURL: current.PayURL,
			ExpiresAt: current.ExpiresAt, AlreadyPaid: true,
		}, nil
	default:
		return nil, nil, nil
	}
}

func (s *PaymentService) newOrder(ctx context.Context, item *task.Task, provider string, amount int64, idempotencyKey string, now time.Time, quote *coupon.Quote) (*payment.Order, error) {
	id, err := payment.NewID()
	if err != nil {
		return nil, err
	}
	orderNo, err := payment.NewOrderNo("CF")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey, err = payment.NewOrderNo("IK")
		if err != nil {
			return nil, err
		}
	}
	if len(idempotencyKey) > 128 {
		return nil, fmt.Errorf("%w: idempotency key is too long", domain.ErrInvalidRequest)
	}
	expiresAt := now.Add(s.expiry)
	original := amount
	var claimID, couponTitle string
	var discount int64
	if quote != nil {
		original, discount = quote.OriginalAmountFen, quote.DiscountFen
		claimID, couponTitle = quote.UserCoupon.ID, quote.Coupon.Title
	}
	return s.orders.CreatePaymentOrder(ctx, payment.Order{
		ID: id, TaskID: item.ID, PayerID: item.CreatedBy, AmountFen: amount,
		OriginalAmountFen: original, DiscountFen: discount,
		CouponClaimID: claimID, CouponTitle: couponTitle,
		Currency: payment.CurrencyCNY, Status: payment.PaymentPending, Provider: provider,
		ProviderOrderNo: orderNo, IdempotencyKey: idempotencyKey, ExpiresAt: &expiresAt,
	})
}

func quoteClaimID(quote *coupon.Quote) string {
	if quote == nil || quote.UserCoupon == nil {
		return ""
	}
	return quote.UserCoupon.ID
}

// Coupon redemption is deliberately best-effort: the payment provider is the
// source of truth for money, so a coupon write failure must never mark an
// actually-paid order as unpaid. Reconciliation can retry the claim later.
func (s *PaymentService) noteCouponUsed(ctx context.Context, order *payment.Order) {
	if s.coupons == nil || order == nil || strings.TrimSpace(order.CouponClaimID) == "" {
		return
	}
	if err := s.coupons.MarkUsed(ctx, order.CouponClaimID, order.TaskID, order.ID, order.DiscountFen); err != nil {
		log.Printf("warning: mark coupon %s used for payment %s: %v", order.CouponClaimID, order.ID, err)
	}
}

func (s *PaymentService) releaseCoupon(ctx context.Context, order *payment.Order) {
	if s.coupons == nil || order == nil || strings.TrimSpace(order.CouponClaimID) == "" {
		return
	}
	s.coupons.Release(ctx, order.CouponClaimID, order.TaskID)
}

func paymentSubject(item *task.Task) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		return "CityFlow 配送服务"
	}
	return truncateRunes("CityFlow 配送服务 · "+title, 120)
}

func paymentFailure(err error) string {
	message := strings.TrimSpace(err.Error())
	if len([]rune(message)) > 250 {
		return string([]rune(message)[:250])
	}
	return message
}

func (s *PaymentService) Summary(ctx context.Context, taskID string) (*PaymentSummary, error) {
	summary := &PaymentSummary{}
	order, err := s.orders.PaymentOrderByTask(ctx, taskID)
	switch {
	case err == nil:
		summary.Order = order
	case !errors.Is(err, domain.ErrNotFound):
		return nil, err
	}
	settlement, err := s.orders.SettlementByTask(ctx, taskID)
	switch {
	case err == nil:
		summary.Settlement = settlement
	case !errors.Is(err, domain.ErrNotFound):
		return nil, err
	}
	return summary, nil
}

// HandleNotify applies a signature-checked provider callback. It is safe to
// call repeatedly: paid, closed and refunded states are all compare-and-set.
func (s *PaymentService) HandleNotify(ctx context.Context, provider string, headers map[string]string, body []byte) error {
	provider = strings.ToUpper(strings.TrimSpace(provider))
	gateway, ok := s.gateways[provider]
	if !ok {
		return fmt.Errorf("%w: unknown provider %q", domain.ErrInvalidRequest, provider)
	}
	now := s.now()
	notification, err := gateway.ParseNotify(ctx, headers, body)
	if err != nil {
		s.recordNotify(ctx, payment.NotifyLog{
			Provider: provider, Result: "rejected", Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
		})
		return err
	}
	if notification.Kind == payment.NotifyKindPayout {
		return s.applyPayoutNotify(ctx, provider, notification, body, now)
	}
	if notification.Kind == payment.NotifyKindRefund {
		return s.applyRefundNotify(ctx, provider, notification, body, now)
	}
	entry := payment.NotifyLog{
		Provider: provider, ProviderOrderNo: notification.OrderNo, ProviderTradeID: notification.TradeID,
		SignatureValid: true, Status: notification.Status, Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
	}
	order, err := s.orders.PaymentOrderByProviderNo(ctx, provider, notification.OrderNo)
	if err != nil {
		entry.Result = "unknown_order"
		s.recordNotify(ctx, entry)
		return err
	}
	if notification.AmountFen > 0 && notification.AmountFen != order.AmountFen {
		entry.Result = "amount_mismatch"
		s.recordNotify(ctx, entry)
		return fmt.Errorf("%w: provider amount %d does not match order amount %d", domain.ErrConflict, notification.AmountFen, order.AmountFen)
	}
	switch notification.Status {
	case payment.PaymentPaid:
		paidAt := derefTime(notification.PaidAt, now)
		paidOrder, payErr := s.orders.MarkPaymentPaid(ctx, order.ID, notification.TradeID, paidAt)
		if payErr != nil {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return payErr
		}
		s.noteCouponUsed(ctx, paidOrder)
		entry.Result = "paid"
	case payment.PaymentClosed:
		if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		s.releaseCoupon(ctx, order)
		entry.Result = "closed"
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refunded"
	case payment.PaymentRefundFailed:
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, "provider reported a refund failure"); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refund_failed"
	default:
		entry.Result = "ignored"
	}
	if err := s.orders.NotePaymentNotify(ctx, order.ID, now); err != nil {
		log.Printf("warning: note notify for payment %s: %v", order.ID, err)
	}
	s.recordNotify(ctx, entry)
	return nil
}

func (s *PaymentService) applyPayoutNotify(ctx context.Context, provider string, notification *payment.Notification, body []byte, now time.Time) error {
	entry := payment.NotifyLog{
		Provider: provider, ProviderTradeID: notification.TradeID, SignatureValid: true,
		Status: notification.Status, Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
	}
	settlement, err := s.orders.SettlementByProviderBillNo(ctx, provider, notification.BillNo)
	if err != nil {
		entry.Result = "unknown_bill"
		s.recordNotify(ctx, entry)
		return err
	}
	switch notification.Status {
	case payment.SettlementPaid:
		if _, err := s.orders.MarkSettlementPaid(ctx, settlement.ID, notification.TradeID, derefTime(notification.PaidAt, now)); err != nil {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "settled"
	case payment.SettlementFailed:
		if _, err := s.orders.MarkSettlementFailed(ctx, settlement.ID, "provider reported a failed transfer", nil); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "failed"
	default:
		entry.Result = "processing"
	}
	s.recordNotify(ctx, entry)
	return nil
}

func (s *PaymentService) applyRefundNotify(ctx context.Context, provider string, notification *payment.Notification, body []byte, now time.Time) error {
	entry := payment.NotifyLog{
		Provider: provider, ProviderOrderNo: notification.OrderNo, ProviderTradeID: notification.TradeID,
		SignatureValid: true, Status: notification.Status, Payload: truncateRunes(string(body), 4096), ReceivedAt: now,
	}
	order, err := s.orders.PaymentOrderByProviderNo(ctx, provider, notification.OrderNo)
	if err != nil {
		entry.Result = "unknown_order"
		s.recordNotify(ctx, entry)
		return err
	}
	if notification.AmountFen > 0 && notification.AmountFen != order.AmountFen {
		entry.Result = "amount_mismatch"
		s.recordNotify(ctx, entry)
		return fmt.Errorf("%w: refund amount %d does not match order amount %d", domain.ErrConflict, notification.AmountFen, order.AmountFen)
	}
	switch notification.Status {
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refunded"
	case payment.PaymentRefundFailed:
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, "provider reported a refund failure"); err != nil && !errors.Is(err, domain.ErrConflict) {
			entry.Result = "state_conflict"
			s.recordNotify(ctx, entry)
			return err
		}
		entry.Result = "refund_failed"
	default:
		entry.Result = "processing"
	}
	if err := s.orders.NotePaymentNotify(ctx, order.ID, now); err != nil {
		log.Printf("warning: note refund notify for payment %s: %v", order.ID, err)
	}
	s.recordNotify(ctx, entry)
	return nil
}

func (s *PaymentService) recordNotify(ctx context.Context, entry payment.NotifyLog) {
	if s.notifyBuffer != nil && s.notifyBuffer.Enqueue(entry) {
		return
	}
	if err := s.orders.RecordNotify(ctx, entry); err != nil {
		log.Printf("warning: record payment notify: %v", err)
	}
}

// SimulatePaid is the mock-only shortcut. It is refused unless the order was
// created by the mock gateway and the deployment explicitly allows it.
func (s *PaymentService) SimulatePaid(ctx context.Context, orderID, payerID string) (*payment.Order, error) {
	if !s.allowMockPay {
		return nil, domain.ErrForbidden
	}
	order, err := s.orders.PaymentOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.Provider != payment.ProviderMock {
		return nil, fmt.Errorf("%w: only mock payments can be simulated", domain.ErrForbidden)
	}
	if order.PayerID != payerID {
		return nil, domain.ErrForbidden
	}
	paid, err := s.orders.MarkPaymentPaid(ctx, order.ID, "MOCK-"+order.ID, s.now())
	if err != nil {
		return nil, err
	}
	s.noteCouponUsed(ctx, paid)
	return paid, nil
}

// SyncPayment actively queries the provider; webhooks can be delayed or lost,
// so the client and the reconciliation job both need this path.
func (s *PaymentService) SyncPayment(ctx context.Context, orderID, actorID, role string) (*payment.Order, error) {
	order, err := s.orders.PaymentOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && order.PayerID != actorID {
		return nil, domain.ErrForbidden
	}
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		return nil, err
	}
	state, err := gateway.Query(ctx, order.ProviderOrderNo)
	if err != nil {
		return nil, fmt.Errorf("%w: 查询支付状态失败: %v", domain.ErrUnavailable, err)
	}
	switch state.Status {
	case payment.PaymentPaid:
		if state.AmountFen > 0 && state.AmountFen != order.AmountFen {
			return nil, fmt.Errorf("%w: 渠道返回金额 %d 分与订单金额 %d 分不一致", domain.ErrConflict, state.AmountFen, order.AmountFen)
		}
		paidOrder, payErr := s.orders.MarkPaymentPaid(ctx, order.ID, state.TradeID, derefTime(state.PaidAt, s.now()))
		if payErr != nil {
			if !errors.Is(payErr, domain.ErrConflict) {
				return nil, payErr
			}
		} else {
			s.noteCouponUsed(ctx, paidOrder)
		}
	case payment.PaymentClosed:
		if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
		s.releaseCoupon(ctx, order)
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
	}
	return s.orders.PaymentOrder(ctx, order.ID)
}

func (s *PaymentService) Refund(ctx context.Context, orderID, actorID, role, reason string) (*payment.Order, error) {
	order, err := s.orders.PaymentOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && order.PayerID != actorID {
		return nil, domain.ErrForbidden
	}
	if order.Status == payment.PaymentRefunded {
		return order, nil
	}
	switch order.Status {
	case payment.PaymentPaid, payment.PaymentRefundPending, payment.PaymentRefundFailed:
	default:
		return nil, fmt.Errorf("%w: 只有已支付的订单可以退款", domain.ErrConflict)
	}
	if settlement, err := s.orders.SettlementByTask(ctx, order.TaskID); err == nil {
		switch settlement.Status {
		case payment.SettlementProcessing, payment.SettlementPaid, payment.SettlementUnknown:
			return nil, fmt.Errorf("%w: 骑手结算已开始或状态待核对，请先联系平台处理", domain.ErrConflict)
		case payment.SettlementPending:
			// Payout has not been claimed yet. Cancel it with a compare-and-set
			// first; if the worker has already claimed it, the refund is refused
			// rather than paying the rider and refunding the user twice.
			if _, err := s.orders.CancelPendingSettlement(ctx, settlement.ID, "退款前取消结算"); err != nil {
				if errors.Is(err, domain.ErrConflict) {
					return nil, fmt.Errorf("%w: 骑手结算已开始，请先联系平台处理", domain.ErrConflict)
				}
				return nil, err
			}
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		return nil, err
	}
	if _, err := s.orders.MarkPaymentRefundPending(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
		return nil, err
	}
	refund, err := gateway.Refund(ctx, payment.RefundRequest{
		OrderNo: order.ProviderOrderNo, RefundNo: refundNumber(order),
		TradeID: order.ProviderTradeID, AmountFen: order.AmountFen, Reason: reason,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: 退款失败: %v", domain.ErrUnavailable, err)
	}
	switch refund.Status {
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil {
			return nil, err
		}
	case payment.PaymentRefundFailed:
		reason := firstNonEmptyText(refund.FailReason, "provider rejected the refund")
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, truncateRunes(reason, 250)); err != nil && !errors.Is(err, domain.ErrConflict) {
			return nil, err
		}
	}
	return s.orders.PaymentOrder(ctx, order.ID)
}

func refundNumber(order *payment.Order) string {
	prefix := "RF-" + order.ProviderOrderNo
	if len(prefix) > 64 {
		prefix = "RF-" + order.ID
	}
	return prefix
}

// EnsureSettlement creates the payout record once a task is completed and its
// payment is verified. Its idempotency key is the task id, so a retry can never
// create a second payout for the same job.
func (s *PaymentService) EnsureSettlement(ctx context.Context, item *task.Task) (*payment.Settlement, error) {
	if item == nil || item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 || item.WorkerID == "" {
		return nil, nil
	}
	if existing, err := s.orders.SettlementByTask(ctx, item.ID); err == nil {
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if order.Status != payment.PaymentPaid {
		return nil, nil
	}
	payoutFen, feeFen := payment.SplitAmount(*item.QuotedAmountFen, s.feeBPS)
	if payoutFen <= 0 {
		return nil, nil
	}
	id, err := payment.NewID()
	if err != nil {
		return nil, err
	}
	billNo, err := payment.NewOrderNo("ST")
	if err != nil {
		return nil, err
	}
	settlement, err := s.orders.CreateSettlement(ctx, payment.Settlement{
		ID: id, TaskID: item.ID, PaymentOrderID: order.ID, RiderID: item.WorkerID,
		AmountFen: payoutFen, FeeFen: feeFen, Currency: payment.CurrencyCNY, Status: payment.SettlementPending,
		Provider: order.Provider, ProviderBillNo: billNo, IdempotencyKey: "task:" + item.ID,
	})
	if err != nil {
		return nil, err
	}
	return settlement, nil
}

// SettleOnce claims a small batch of due payouts and attempts each transfer.
// It is called by the worker and by tests; it is safe to run concurrently
// because claiming uses SKIP LOCKED in MySQL.
func (s *PaymentService) SettleOnce(ctx context.Context) (int, error) {
	due, err := s.orders.ClaimDueSettlements(ctx, s.now(), 10, 2*time.Minute)
	if err != nil {
		return 0, err
	}
	for _, item := range due {
		if err := s.settleOne(ctx, item); err != nil {
			log.Printf("warning: settle %s: %v", item.ID, err)
		}
	}
	return len(due), nil
}

func (s *PaymentService) settleOne(ctx context.Context, item *payment.Settlement) error {
	gateway, ok := s.gateways[item.Provider]
	if !ok || !gateway.Configured() {
		return s.retryLater(ctx, item, "支付渠道未配置")
	}
	// The mock channel exists so the whole flow can be demonstrated without
	// merchant credentials; it has no real payee, so it needs no payout account.
	account := payment.PayoutAccount{Provider: payment.ProviderMock, Account: "mock", AccountName: "mock rider"}
	if item.Provider != payment.ProviderMock {
		stored, err := s.orders.PayoutAccount(ctx, item.RiderID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return s.retryLater(ctx, item, "骑手未设置收款账户")
			}
			return err
		}
		if stored.Provider != item.Provider {
			return s.retryLater(ctx, item, fmt.Sprintf("骑手收款账户属于 %s，与支付渠道 %s 不一致", stored.Provider, item.Provider))
		}
		account = *stored
	}
	// On a retry, ask the provider first. A transfer that is merely slow must
	// not be sent twice, and a success that arrived after our timeout must be
	// booked instead of retried until it is marked failed.
	if item.Attempts > 1 {
		if query, ok := gateway.(payment.PayoutQuerier); ok {
			if payout, err := query.QueryPayout(ctx, item.ProviderBillNo); err == nil {
				switch payout.Status {
				case payment.SettlementPaid:
					_, err := s.orders.MarkSettlementPaid(ctx, item.ID, firstNonEmptyText(payout.BillNo, item.ProviderBillNo), s.now())
					return err
				case payment.SettlementFailed:
					return s.retryLater(ctx, item, firstNonEmptyText(payout.FailReason, "provider reported a failed transfer"))
				case payment.SettlementProcessing:
					log.Printf("settlement %s is still processing at the provider; leaving it for the next lease", item.ID)
					return nil
				}
			} else {
				log.Printf("warning: query payout %s before retry: %v; retrying with the same bill number", item.ID, err)
			}
		}
	}
	payout, err := gateway.Transfer(ctx, payment.PayoutRequest{
		BillNo: item.ProviderBillNo, OrderNo: "", RiderID: item.RiderID,
		AmountFen: item.AmountFen, Account: account, Remark: "CityFlow 任务结算 " + item.TaskID,
	})
	if err != nil {
		return s.retryLaterAfterTransfer(ctx, item, err.Error())
	}
	switch payout.Status {
	case payment.SettlementPaid:
		_, err := s.orders.MarkSettlementPaid(ctx, item.ID, payout.BillNo, s.now())
		return err
	case payment.SettlementFailed:
		return s.retryLater(ctx, item, firstNonEmptyText(payout.FailReason, "provider reported a failed transfer"))
	default:
		return s.retryLaterAfterTransfer(ctx, item, "provider is still processing the transfer")
	}
}

func (s *PaymentService) retryLater(ctx context.Context, item *payment.Settlement, reason string) error {
	return s.retryLaterAfter(ctx, item, reason, false)
}

// retryLaterAfterTransfer is used when the provider may already have accepted
// the transfer (timeout or "processing"). At the last attempt it must query the
// provider instead of marking FAILED, otherwise a later refund could pay the
// user while the rider has already received the money.
func (s *PaymentService) retryLaterAfterTransfer(ctx context.Context, item *payment.Settlement, reason string) error {
	return s.retryLaterAfter(ctx, item, reason, true)
}

func (s *PaymentService) retryLaterAfter(ctx context.Context, item *payment.Settlement, reason string, mayHaveLeft bool) error {
	if item.Attempts >= s.maxAttempts {
		if !mayHaveLeft {
			_, err := s.orders.MarkSettlementFailed(ctx, item.ID, truncateRunes(reason, 250), nil)
			return err
		}
		gateway, ok := s.gateways[item.Provider]
		if !ok {
			return s.markSettlementUnknown(ctx, item, "渠道不可用，无法确认打款最终状态")
		}
		query, ok := gateway.(payment.PayoutQuerier)
		if !ok {
			return s.markSettlementUnknown(ctx, item, "渠道不支持打款回查，无法确认最终状态")
		}
		payout, err := query.QueryPayout(ctx, item.ProviderBillNo)
		if err != nil {
			return s.markSettlementUnknown(ctx, item, "无法确认打款最终状态: "+err.Error())
		}
		switch payout.Status {
		case payment.SettlementPaid:
			_, err := s.orders.MarkSettlementPaid(ctx, item.ID, firstNonEmptyText(payout.BillNo, item.ProviderBillNo), s.now())
			return err
		case payment.SettlementFailed:
			_, err := s.orders.MarkSettlementFailed(ctx, item.ID, truncateRunes(firstNonEmptyText(payout.FailReason, reason), 250), nil)
			return err
		default:
			return s.markSettlementUnknown(ctx, item, "渠道仍在处理打款，需人工核对")
		}
	}
	backoff := time.Duration(1<<uint(minInt(item.Attempts-1, 8))) * time.Minute
	if backoff > 6*time.Hour {
		backoff = 6 * time.Hour
	}
	retryAt := s.now().Add(backoff)
	_, err := s.orders.MarkSettlementFailed(ctx, item.ID, truncateRunes(reason, 250), &retryAt)
	return err
}

func (s *PaymentService) markSettlementUnknown(ctx context.Context, item *payment.Settlement, reason string) error {
	log.Printf("warning: settlement %s state is unknown, manual reconciliation required: %s", item.ID, reason)
	_, err := s.orders.MarkSettlementUnknown(ctx, item.ID, truncateRunes(reason, 250))
	return err
}

// RunWorker runs the scalable payout loop alongside the singleton-style
// reconciliation scans. Deployments may run the payout loop on every Payment
// instance because claims use SKIP LOCKED and a lease, while electing one
// instance to run the scans.
func (s *PaymentService) RunWorker(ctx context.Context, interval time.Duration) {
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		s.RunSettlementWorker(ctx, interval)
	}()
	go func() {
		defer workers.Done()
		s.RunReconciliationWorker(ctx, interval)
	}()
	go func() {
		defer workers.Done()
		s.RunInboxRecoveryWorker(ctx, interval)
	}()
	workers.Wait()
}

// RunSettlementWorker can run on multiple instances. ClaimDueSettlements
// atomically leases disjoint batches using FOR UPDATE SKIP LOCKED.
func (s *PaymentService) RunSettlementWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if _, err := s.SettleOnce(ctx); err != nil {
		log.Printf("warning: settlement worker: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SettleOnce(ctx); err != nil {
				log.Printf("warning: settlement worker: %v", err)
			}
		}
	}
}

// RunReconciliationWorker scans payment/refund states and repairs missed
// settlement creation. Keep this on one elected instance until each scan has
// its own row claims/leases; these scans can call external channels.
func (s *PaymentService) RunReconciliationWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.reconcile(ctx)
	s.ReconcileOnce(ctx)
	s.purgeInboxIfDue(ctx)
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ticks++
			if ticks%4 == 0 {
				s.ReconcileOnce(ctx)
				s.reconcile(ctx)
				s.purgeInboxIfDue(ctx)
			}
		}
	}
}

// purgeInboxIfDue deletes old PROCESSED inbox rows. Keeping every historical
// event makes deduplication slower and the table larger; 30 days is longer than
// any broker replay/restore window we rely on.
func (s *PaymentService) purgeInboxIfDue(ctx context.Context) {
	if s == nil || s.inbox == nil {
		return
	}
	now := s.now()
	if !s.lastInboxPurge.IsZero() && now.Sub(s.lastInboxPurge) < inboxPurgeEvery {
		return
	}
	s.lastInboxPurge = now
	before := now.Add(-inboxRetention)
	var total int64
	for pass := 0; pass < inboxPurgeMaxPasses; pass++ {
		deleted, err := s.inbox.PurgeInbox(ctx, before, inboxPurgeBatch)
		if err != nil {
			log.Printf("warning: purge payment inbox: %v", err)
			return
		}
		total += deleted
		if deleted < int64(inboxPurgeBatch) {
			break
		}
	}
	if total > 0 {
		log.Printf("payment inbox purge: deleted %d processed rows older than %s", total, inboxRetention)
	}
}

// ReconcileOnce is the scheduled safety net that a webhook cannot replace:
// expiring payments are queried and closed, and refunds left in
// REFUND_PENDING are queried or retried with the same refund number.
func (s *PaymentService) ReconcileOnce(ctx context.Context) (int, int) {
	now := s.now()
	payments, err := s.orders.PendingPayments(ctx, now.Add(-time.Minute), 20)
	if err != nil {
		log.Printf("warning: reconcile pending payments: %v", err)
		payments = nil
	}
	checkedPayments := 0
	for _, order := range payments {
		checkedPayments++
		s.reconcilePayment(ctx, order, now)
	}

	refunds, err := s.orders.RefundPendingPayments(ctx, now.Add(-time.Minute), 20)
	if err != nil {
		log.Printf("warning: reconcile pending refunds: %v", err)
		refunds = nil
	}
	checkedRefunds := 0
	for _, order := range refunds {
		checkedRefunds++
		s.reconcileRefund(ctx, order)
	}

	// REFUND_FAILED is terminal for the synchronous caller but not for the
	// scheduled safety net. Retry a bounded batch after a cooldown; the provider
	// refund number is stable, so resubmission is idempotent.
	failedRefunds, err := s.orders.RefundFailedPayments(ctx, now.Add(-5*time.Minute), 20)
	if err != nil {
		log.Printf("warning: reconcile failed refunds: %v", err)
		failedRefunds = nil
	}
	for _, order := range failedRefunds {
		checkedRefunds++
		s.reconcileRefund(ctx, order)
	}
	return checkedPayments, checkedRefunds
}

func (s *PaymentService) reconcilePayment(ctx context.Context, order *payment.Order, now time.Time) {
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		log.Printf("warning: reconcile payment %s: %v", order.ID, err)
		return
	}
	state, err := gateway.Query(ctx, order.ProviderOrderNo)
	if err != nil {
		log.Printf("warning: query payment %s: %v", order.ID, err)
		return
	}
	switch state.Status {
	case payment.PaymentPaid:
		if state.AmountFen > 0 && state.AmountFen != order.AmountFen {
			log.Printf("warning: payment %s amount mismatch: provider=%d local=%d", order.ID, state.AmountFen, order.AmountFen)
			return
		}
		if paidOrder, payErr := s.orders.MarkPaymentPaid(ctx, order.ID, state.TradeID, derefTime(state.PaidAt, now)); payErr != nil {
			if !errors.Is(payErr, domain.ErrConflict) {
				log.Printf("warning: reconcile payment %s paid: %v", order.ID, payErr)
			}
		} else {
			s.noteCouponUsed(ctx, paidOrder)
		}
	case payment.PaymentClosed:
		if _, err := s.orders.ClosePaymentOrder(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: reconcile payment %s closed: %v", order.ID, err)
		}
		s.releaseCoupon(ctx, order)
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: reconcile payment %s refunded: %v", order.ID, err)
		}
	default:
		if order.ExpiresAt != nil && !order.ExpiresAt.After(now) {
			if err := s.closeProviderOrder(ctx, gateway, order); err != nil {
				log.Printf("warning: expire payment %s: %v", order.ID, err)
			}
		}
	}
}

func (s *PaymentService) reconcileRefund(ctx context.Context, order *payment.Order) {
	gateway, err := s.gateway(order.Provider)
	if err != nil {
		log.Printf("warning: reconcile refund %s: %v", order.ID, err)
		return
	}
	number := refundNumber(order)
	if query, ok := gateway.(payment.RefundQuerier); ok {
		refund, err := query.QueryRefund(ctx, order.ProviderOrderNo, number)
		if err == nil {
			switch refund.Status {
			case payment.PaymentRefunded:
				if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
					log.Printf("warning: mark refunded %s: %v", order.ID, err)
				}
				return
			case payment.PaymentRefundFailed:
				reason := firstNonEmptyText(refund.FailReason, "provider reported a refund failure")
				if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, truncateRunes(reason, 250)); err != nil && !errors.Is(err, domain.ErrConflict) {
					log.Printf("warning: mark refund failed %s: %v", order.ID, err)
				}
				return
			case payment.PaymentRefundPending:
				return
			}
		}
	}
	// The query could not confirm a refund (some providers return "not found"
	// when the original request never reached them). Re-submitting with the
	// same refund number is idempotent at both Alipay and WeChat.
	refund, err := gateway.Refund(ctx, payment.RefundRequest{
		OrderNo: order.ProviderOrderNo, RefundNo: number,
		TradeID: order.ProviderTradeID, AmountFen: order.AmountFen, Reason: "定时退款补偿",
	})
	if err != nil {
		log.Printf("warning: retry refund %s: %v", order.ID, err)
		return
	}
	switch refund.Status {
	case payment.PaymentRefunded:
		if _, err := s.orders.MarkPaymentRefunded(ctx, order.ID); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: mark refunded %s: %v", order.ID, err)
		}
	case payment.PaymentRefundFailed:
		reason := firstNonEmptyText(refund.FailReason, "provider rejected the refund")
		if _, err := s.orders.MarkPaymentRefundFailed(ctx, order.ID, truncateRunes(reason, 250)); err != nil && !errors.Is(err, domain.ErrConflict) {
			log.Printf("warning: mark refund failed %s: %v", order.ID, err)
		}
	}
}

const (
	settlementReconcilePageSize = 200
	settlementReconcileMaxPages = 10
)

func (s *PaymentService) reconcile(ctx context.Context) {
	seen := 0
	for page := 0; page < settlementReconcileMaxPages; page++ {
		items, err := s.tasks.List(ctx, task.StatusCompleted, repository.Page{
			Limit: settlementReconcilePageSize, Offset: page * settlementReconcilePageSize,
		})
		if err != nil {
			log.Printf("warning: settlement reconcile page %d: %v", page, err)
			return
		}
		for _, item := range items {
			if _, err := s.EnsureSettlement(ctx, item); err != nil {
				log.Printf("warning: reconcile settlement for task %s: %v", item.ID, err)
			}
		}
		seen += len(items)
		if len(items) < settlementReconcilePageSize {
			return
		}
	}
	log.Printf("warning: settlement reconcile stopped after %d completed tasks; older tasks need a bounded backfill", seen)
}

func (s *PaymentService) SettlementsForRider(ctx context.Context, riderID string, page repository.Page) ([]*payment.Settlement, error) {
	return s.orders.SettlementsByRider(ctx, riderID, page)
}

func (s *PaymentService) PayoutAccount(ctx context.Context, riderID string) (*payment.PayoutAccount, error) {
	return s.orders.PayoutAccount(ctx, riderID)
}

func (s *PaymentService) SavePayoutAccount(ctx context.Context, riderID string, request payment.PayoutAccountRequest) (*payment.PayoutAccount, error) {
	provider := strings.ToUpper(strings.TrimSpace(request.Provider))
	switch provider {
	case payment.ProviderAlipay, payment.ProviderWechat, "BANK":
	default:
		return nil, fmt.Errorf("%w: 收款渠道必须是 ALIPAY、WECHAT 或 BANK", domain.ErrInvalidRequest)
	}
	account := strings.TrimSpace(request.Account)
	if account == "" || len([]rune(account)) > 128 {
		return nil, fmt.Errorf("%w: 收款账户不能为空且不超过 128 个字符", domain.ErrInvalidRequest)
	}
	name := strings.TrimSpace(request.AccountName)
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("%w: 收款人姓名过长", domain.ErrInvalidRequest)
	}
	return s.orders.SavePayoutAccount(ctx, payment.PayoutAccount{
		RiderID: riderID, Provider: provider, Account: account, AccountName: name,
	})
}

// EnsureClaimable is the gate plugged into TaskService.Claim: a quoted task
// can only be taken after its payment is verified.
func (s *PaymentService) EnsureClaimable(ctx context.Context, item *task.Task) error {
	if item == nil || item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("%w: 请先完成支付后再接单", domain.ErrConflict)
	}
	if err != nil {
		return err
	}
	if order.Status != payment.PaymentPaid {
		return fmt.Errorf("%w: 任务尚未收到付款，暂不能接单", domain.ErrConflict)
	}
	return nil
}

// EnsureEditable blocks changing a task's amount after a payment attempt
// exists, which would otherwise desynchronise the paid amount and the task.
func (s *PaymentService) EnsureEditable(ctx context.Context, item *task.Task, nextAmount *int64) error {
	if item == nil || item.QuotedAmountFen == nil {
		return nil
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if order.Status != payment.PaymentPending && order.Status != payment.PaymentPaid {
		return nil
	}
	if nextAmount == nil || *nextAmount != order.AmountFen {
		return fmt.Errorf("%w: 已发起支付，任务金额不能修改", domain.ErrConflict)
	}
	return nil
}

// AfterCancel refunds a paid task in the background. The task is already
// cancelled at this point, so a provider failure must not resurrect it.
func (s *PaymentService) AfterCancel(ctx context.Context, item *task.Task) {
	if err := s.RefundCancelledTask(ctx, item); err != nil {
		log.Printf("warning: refund cancelled task: %v", err)
	}
}

// HandleTaskEvent is the Payment consumer's boundary. It is idempotent via the
// inbox (event ID) and again via settlement/refund business keys. Returning nil
// acknowledges the event; returning an error asks the transport to retry.
func (s *PaymentService) HandleTaskEvent(ctx context.Context, eventID, eventType string, payload []byte) error {
	if s.inbox != nil {
		claimed, err := s.inbox.ClaimInbox(ctx, eventID, eventType, payload)
		if err != nil {
			return fmt.Errorf("claim payment inbox: %w", err)
		}
		if !claimed {
			status, statusErr := s.inbox.InboxStatus(ctx, eventID)
			if statusErr != nil {
				return fmt.Errorf("payment inbox status %s: %w", eventID, statusErr)
			}
			switch status {
			case "PROCESSED":
				log.Printf("payment inbox: duplicate event %s (%s) already processed, acknowledged", eventID, eventType)
				return nil
			case "PROCESSING":
				// ClaimInbox returns false while another delivery still owns the
				// lease. ACKing here would silently drop the event for the whole
				// lease window if that owner crashes, so ask the transport to retry
				// instead. The payload is already durable and RecoverInbox replays
				// stale PROCESSING rows if retries are exhausted.
				return fmt.Errorf("payment inbox event %s is still PROCESSING; retry later", eventID)
			default:
				return fmt.Errorf("payment inbox event %s has unexpected state %q; retry", eventID, status)
			}
		}
	}
	if err := s.processTaskEvent(ctx, eventType, payload); err != nil {
		s.markInboxFailed(ctx, eventID, err)
		return err
	}
	if s.inbox != nil {
		if markErr := s.inbox.MarkInboxProcessed(ctx, eventID, s.now()); markErr != nil {
			return fmt.Errorf("mark payment inbox processed: %w", markErr)
		}
	}
	return nil
}

// processTaskEvent applies the event's business side effect without touching
// the inbox. It is shared by the live consumer and the Inbox recovery worker.
func (s *PaymentService) processTaskEvent(ctx context.Context, eventType string, payload []byte) error {
	var item task.Task
	if err := json.Unmarshal(payload, &item); err != nil {
		return fmt.Errorf("decode %s payload: %w", eventType, err)
	}
	switch eventType {
	case "task.completed.v1":
		if item.ID == "" || item.Status != task.StatusCompleted {
			return fmt.Errorf("%w: completed event has no completed task", domain.ErrInvalidRequest)
		}
		_, err := s.EnsureSettlement(ctx, &item)
		return err
	case "task.cancelled.v1":
		if item.ID == "" || item.Status != task.StatusCancelled {
			return fmt.Errorf("%w: cancelled event has no cancelled task", domain.ErrInvalidRequest)
		}
		return s.RefundCancelledTask(ctx, &item)
	default:
		log.Printf("payment consumer: ignoring unsupported event %s", eventType)
		return nil
	}
}

// RecoverInbox replays events whose consumer crashed after claiming the inbox
// row but before finishing the settlement/refund side effect. It only leases
// rows after the PROCESSING lease expires, so a live consumer is not raced.
func (s *PaymentService) RecoverInbox(ctx context.Context) (int, error) {
	if s.inbox == nil {
		return 0, nil
	}
	events, err := s.inbox.ClaimRecoverableInbox(ctx, s.now().Add(-inboxRecoveryLease), inboxRecoveryBatch, inboxMaxAttempts)
	if err != nil {
		return 0, fmt.Errorf("claim recoverable payment inbox: %w", err)
	}
	recovered := 0
	for _, event := range events {
		if err := s.processTaskEvent(ctx, event.EventType, event.Payload); err != nil {
			if event.Attempts >= inboxMaxAttempts {
				s.markInboxFailed(ctx, event.EventID, err)
			} else if retryErr := s.inbox.MarkInboxRetrying(ctx, event.EventID, truncateRunes(err.Error(), 250), s.now()); retryErr != nil {
				log.Printf("warning: reschedule payment inbox %s: %v", event.EventID, retryErr)
			}
			continue
		}
		if markErr := s.inbox.MarkInboxProcessed(ctx, event.EventID, s.now()); markErr != nil {
			log.Printf("warning: mark recovered payment inbox %s processed: %v", event.EventID, markErr)
			continue
		}
		recovered++
	}
	return recovered, nil
}

// RunInboxRecoveryWorker periodically replays stale inbox rows. Multiple
// Payment instances may run it because ClaimRecoverableInbox uses a lease and
// FOR UPDATE SKIP LOCKED.
func (s *PaymentService) RunInboxRecoveryWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = inboxRecoveryInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if count, err := s.RecoverInbox(ctx); err != nil {
		log.Printf("warning: inbox recovery worker: %v", err)
	} else if count > 0 {
		log.Printf("payment inbox recovery: replayed %d events", count)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if count, err := s.RecoverInbox(ctx); err != nil {
				log.Printf("warning: inbox recovery worker: %v", err)
			} else if count > 0 {
				log.Printf("payment inbox recovery: replayed %d events", count)
			}
		}
	}
}

func (s *PaymentService) markInboxFailed(ctx context.Context, eventID string, cause error) {
	if s.inbox == nil || eventID == "" {
		return
	}
	if err := s.inbox.MarkInboxFailed(ctx, eventID, truncateRunes(cause.Error(), 250)); err != nil {
		log.Printf("warning: mark payment inbox failed %s: %v", eventID, err)
	}
}

// RefundCancelledTask returns nil when there is no paid order to refund; that
// is a normal outcome for an unpaid task and must not be retried forever.
func (s *PaymentService) RefundCancelledTask(ctx context.Context, item *task.Task) error {
	if item == nil || item.ID == "" {
		return fmt.Errorf("%w: cancelled event has no task id", domain.ErrInvalidRequest)
	}
	order, err := s.orders.PaymentOrderByTask(ctx, item.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if order.Status == payment.PaymentRefunded {
		return nil
	}
	if order.Status != payment.PaymentPaid && order.Status != payment.PaymentRefundPending && order.Status != payment.PaymentRefundFailed {
		return nil
	}
	actorID := item.CreatedBy
	if actorID == "" {
		actorID = order.PayerID
	}
	if _, err := s.Refund(ctx, order.ID, actorID, auth.RoleUser, "任务取消"); err != nil {
		return fmt.Errorf("refund cancelled task %s: %w", item.ID, err)
	}
	return nil
}

func derefTime(value *time.Time, fallback time.Time) time.Time {
	if value == nil {
		return fallback
	}
	return *value
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
