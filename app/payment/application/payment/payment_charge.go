package paymentservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cityflow/internal/common/domain"

	"cityflow/app/payment/domain/payment"
)

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
	if item.Status != TaskStatusOpen {
		return nil, fmt.Errorf("%w: 只有待接单的任务可以支付", domain.ErrConflict)
	}
	if item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil, fmt.Errorf("%w: 任务未设置报酬金额", domain.ErrInvalidRequest)
	}
	amount := *item.QuotedAmountFen
	var quote *CouponQuote
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
			locked, lockErr := s.coupons.LockForTask(ctx, input.PayerID, quote.ClaimID, item.ID)
			if lockErr != nil {
				return nil, lockErr
			}
			quote = locked
			amount = locked.PayableAmountFen
		}
		order, err = s.newOrder(ctx, item, provider, amount, input.IdempotencyKey, now, quote)
		if err != nil {
			if quote != nil {
				s.coupons.Release(ctx, quote.ClaimID, item.ID)
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
	return &ChargeResult{Payment: order, QRCode: charge.QRCode, PayURL: charge.PayURL, ExpiresAt: &expiresAt}, nil
}

// existingCharge returns either a reusable pending order or a finished result.
// One live order per task keeps double clicks from creating two QR codes.
func (s *PaymentService) existingCharge(ctx context.Context, taskID, provider string, amount int64, couponClaimID string, now time.Time, gateway payment.Gateway) (*payment.PaymentOrder, *ChargeResult, error) {
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
			Payment: existing, QRCode: existing.PayPayload, PayURL: existing.PayURL,
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
			return nil, &ChargeResult{Payment: existing, QRCode: existing.PayPayload, PayURL: existing.PayURL, ExpiresAt: existing.ExpiresAt}, nil
		}
		return existing, nil, nil
	default:
		return nil, nil, nil
	}
}

// closeProviderOrder closes the trade at the provider before closing it
// locally. If the provider cannot confirm the close, the caller must not create
// a second QR code: the old code may still be payable.
func (s *PaymentService) closeProviderOrder(ctx context.Context, gateway payment.Gateway, order *payment.PaymentOrder) error {
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
func (s *PaymentService) afterClosedCharge(ctx context.Context, previous *payment.PaymentOrder) (*payment.PaymentOrder, *ChargeResult, error) {
	current, err := s.orders.PaymentOrder(ctx, previous.ID)
	if err != nil {
		return nil, nil, err
	}
	switch current.Status {
	case payment.PaymentPaid, payment.PaymentRefundPending:
		return nil, &ChargeResult{
			Payment: current, QRCode: current.PayPayload, PayURL: current.PayURL,
			ExpiresAt: current.ExpiresAt, AlreadyPaid: true,
		}, nil
	default:
		return nil, nil, nil
	}
}

func (s *PaymentService) newOrder(ctx context.Context, item *TaskView, provider string, amount int64, idempotencyKey string, now time.Time, quote *CouponQuote) (*payment.PaymentOrder, error) {
	id, err := payment.NewID()
	if err != nil {
		return nil, err
	}
	providerOrderNo, err := payment.NewProviderOrderNo("CF")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey, err = payment.NewProviderOrderNo("IK")
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
		claimID, couponTitle = quote.ClaimID, quote.CouponTitle
	}
	return s.orders.CreatePaymentOrder(ctx, payment.PaymentOrder{
		ID: id, TaskID: item.ID, PayerID: item.CreatedBy, AmountFen: amount,
		OriginalAmountFen: original, DiscountFen: discount,
		CouponClaimID: claimID, CouponTitle: couponTitle,
		Currency: payment.CurrencyCNY, Status: payment.PaymentPending, Provider: provider,
		ProviderOrderNo: providerOrderNo, IdempotencyKey: idempotencyKey, ExpiresAt: &expiresAt,
	})
}

func quoteClaimID(quote *CouponQuote) string {
	if quote == nil {
		return ""
	}
	return quote.ClaimID
}

// Coupon redemption is deliberately best-effort: the payment provider is the
// source of truth for money, so a coupon write failure must never mark an
// actually-paid order as unpaid. Reconciliation can retry the claim later.
func (s *PaymentService) noteCouponUsed(ctx context.Context, order *payment.PaymentOrder) {
	if s.coupons == nil || order == nil || strings.TrimSpace(order.CouponClaimID) == "" {
		return
	}
	if err := s.coupons.MarkUsed(ctx, order.CouponClaimID, order.TaskID, order.ID, order.DiscountFen); err != nil {
		log.Printf("warning: mark coupon %s used for payment %s: %v", order.CouponClaimID, order.ID, err)
	}
}

func (s *PaymentService) releaseCoupon(ctx context.Context, order *payment.PaymentOrder) {
	if s.coupons == nil || order == nil || strings.TrimSpace(order.CouponClaimID) == "" {
		return
	}
	s.coupons.Release(ctx, order.CouponClaimID, order.TaskID)
}

func paymentSubject(item *TaskView) string {
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
