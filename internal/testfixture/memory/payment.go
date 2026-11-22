package memory

import (
	"context"
	"fmt"
	"sort"
	"time"

	"cityflow/app/payment/domain/payment"
	paymentrepo "cityflow/app/payment/repository"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/pagination"
)

var _ paymentrepo.PaymentRepository = (*Store)(nil)

func cloneOrder(item *payment.PaymentOrder) *payment.PaymentOrder {
	copy := *item
	copy.ExpiresAt = copyTime(item.ExpiresAt)
	copy.PaidAt = copyTime(item.PaidAt)
	copy.LastNotifyAt = copyTime(item.LastNotifyAt)
	return &copy
}

func cloneSettlement(item *payment.Settlement) *payment.Settlement {
	copy := *item
	copy.NextAttemptAt = copyTime(item.NextAttemptAt)
	copy.LockedUntil = copyTime(item.LockedUntil)
	copy.PaidAt = copyTime(item.PaidAt)
	return &copy
}

func (s *Store) CreatePaymentOrder(_ context.Context, order payment.PaymentOrder) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if order.PayerID == "" || order.AmountFen <= 0 || order.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid payment order", domain.ErrInvalidRequest)
	}
	if existingID, ok := s.paymentIdem[order.PayerID+":"+order.IdempotencyKey]; ok {
		existing := s.paymentOrders[existingID]
		if existing == nil || !payment.SamePaymentRequest(*existing, order) {
			return nil, fmt.Errorf("%w: idempotency key belongs to another payment request", domain.ErrConflict)
		}
		return cloneOrder(existing), nil
	}
	// A task can only have one live PENDING order. Without this check two
	// concurrent clicks can produce two valid QR codes and charge the user twice.
	for _, existing := range s.paymentOrders {
		if existing.TaskID != order.TaskID || existing.Status != payment.PaymentPending {
			continue
		}
		if !payment.SamePaymentRequest(*existing, order) {
			return nil, fmt.Errorf("%w: task already has an active payment for another request", domain.ErrConflict)
		}
		return cloneOrder(existing), nil
	}
	if order.ProviderOrderNo != "" {
		for _, existing := range s.paymentOrders {
			if existing.Provider == order.Provider && existing.ProviderOrderNo == order.ProviderOrderNo {
				return nil, fmt.Errorf("%w: provider order number already used", domain.ErrConflict)
			}
		}
	}
	now := time.Now().UTC()
	order.CreatedAt, order.UpdatedAt = now, now
	if order.Status == "" {
		order.Status = payment.PaymentPending
	}
	if order.Currency == "" {
		order.Currency = payment.CurrencyCNY
	}
	s.paymentOrders[order.ID] = cloneOrder(&order)
	s.paymentIdem[order.PayerID+":"+order.IdempotencyKey] = order.ID
	return cloneOrder(&order), nil
}

func (s *Store) PaymentOrder(_ context.Context, id string) (*payment.PaymentOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	return cloneOrder(order), nil
}

func (s *Store) PaymentOrderByTask(_ context.Context, taskID string) (*payment.PaymentOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *payment.PaymentOrder
	for _, order := range s.paymentOrders {
		if order.TaskID != taskID {
			continue
		}
		if latest == nil || order.CreatedAt.After(latest.CreatedAt) || (order.CreatedAt.Equal(latest.CreatedAt) && order.ID > latest.ID) {
			latest = order
		}
	}
	if latest == nil {
		return nil, domain.ErrNotFound
	}
	return cloneOrder(latest), nil
}

func (s *Store) PaymentOrderByProviderNo(_ context.Context, provider, providerOrderNo string) (*payment.PaymentOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, order := range s.paymentOrders {
		if order.Provider == provider && order.ProviderOrderNo == providerOrderNo {
			return cloneOrder(order), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) SaveChargePayload(_ context.Context, id, payload, payURL string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return domain.ErrNotFound
	}
	order.PayPayload, order.PayURL = payload, payURL
	order.ExpiresAt = copyTime(&expiresAt)
	order.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) MarkPaymentPaid(_ context.Context, id, tradeID string, paidAt time.Time) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	if order.Status == payment.PaymentPaid {
		if order.ProviderTradeID == "" || order.ProviderTradeID == tradeID {
			return cloneOrder(order), nil
		}
		return nil, fmt.Errorf("%w: payment already settled by another trade", domain.ErrConflict)
	}
	if !payment.CanTransitionPayment(order.Status, payment.PaymentPaid) {
		return nil, fmt.Errorf("%w: payment is not pending", domain.ErrConflict)
	}
	order.Status, order.ProviderTradeID = payment.PaymentPaid, tradeID
	order.PaidAt, order.UpdatedAt = copyTime(&paidAt), time.Now().UTC()
	return cloneOrder(order), nil
}

func (s *Store) MarkPaymentFailed(_ context.Context, id, reason string) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	if order.Status == payment.PaymentFailed {
		return cloneOrder(order), nil
	}
	if !payment.CanTransitionPayment(order.Status, payment.PaymentFailed) {
		return nil, fmt.Errorf("%w: payment cannot be failed from %s", domain.ErrConflict, order.Status)
	}
	order.Status, order.FailReason, order.UpdatedAt = payment.PaymentFailed, reason, time.Now().UTC()
	return cloneOrder(order), nil
}

func (s *Store) ClosePaymentOrder(_ context.Context, id string) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	if order.Status == payment.PaymentClosed {
		return cloneOrder(order), nil
	}
	if !payment.CanTransitionPayment(order.Status, payment.PaymentClosed) {
		return nil, fmt.Errorf("%w: payment cannot be closed from %s", domain.ErrConflict, order.Status)
	}
	order.Status, order.UpdatedAt = payment.PaymentClosed, time.Now().UTC()
	return cloneOrder(order), nil
}

func (s *Store) MarkPaymentRefundPending(_ context.Context, id, reason string) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	if order.Status == payment.PaymentRefundPending || order.Status == payment.PaymentRefunded {
		return cloneOrder(order), nil
	}
	if !payment.CanTransitionPayment(order.Status, payment.PaymentRefundPending) {
		return nil, fmt.Errorf("%w: only a paid payment can be refunded", domain.ErrConflict)
	}
	order.Status, order.UpdatedAt = payment.PaymentRefundPending, time.Now().UTC()
	order.FailReason, order.RefundReason = "", reason
	return cloneOrder(order), nil
}

func (s *Store) MarkPaymentRefunded(_ context.Context, id string) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	if order.Status == payment.PaymentRefunded {
		return cloneOrder(order), nil
	}
	if !payment.CanTransitionPayment(order.Status, payment.PaymentRefunded) {
		return nil, fmt.Errorf("%w: payment is not awaiting a refund", domain.ErrConflict)
	}
	order.Status, order.UpdatedAt = payment.PaymentRefunded, time.Now().UTC()
	order.FailReason = ""
	return cloneOrder(order), nil
}

func (s *Store) MarkPaymentRefundFailed(_ context.Context, id, reason string) (*payment.PaymentOrder, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return nil, domain.ErrNotFound
	}
	if order.Status == payment.PaymentRefundFailed {
		return cloneOrder(order), nil
	}
	if !payment.CanTransitionPayment(order.Status, payment.PaymentRefundFailed) {
		return nil, fmt.Errorf("%w: payment is not awaiting a refund", domain.ErrConflict)
	}
	order.Status, order.FailReason, order.UpdatedAt = payment.PaymentRefundFailed, reason, time.Now().UTC()
	return cloneOrder(order), nil
}

func (s *Store) PendingPayments(_ context.Context, updatedBefore time.Time, limit int) ([]*payment.PaymentOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	items := make([]*payment.PaymentOrder, 0)
	for _, order := range s.paymentOrders {
		if order.Status == payment.PaymentPending && !order.UpdatedAt.After(updatedBefore) {
			items = append(items, cloneOrder(order))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.Before(items[j].UpdatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) RefundPendingPayments(_ context.Context, updatedBefore time.Time, limit int) ([]*payment.PaymentOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	items := make([]*payment.PaymentOrder, 0)
	for _, order := range s.paymentOrders {
		if order.Status == payment.PaymentRefundPending && !order.UpdatedAt.After(updatedBefore) {
			items = append(items, cloneOrder(order))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.Before(items[j].UpdatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) RefundFailedPayments(_ context.Context, updatedBefore time.Time, limit int) ([]*payment.PaymentOrder, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	items := make([]*payment.PaymentOrder, 0)
	for _, order := range s.paymentOrders {
		if order.Status == payment.PaymentRefundFailed && !order.UpdatedAt.After(updatedBefore) {
			items = append(items, cloneOrder(order))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.Before(items[j].UpdatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Store) NotePaymentNotify(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.paymentOrders[id]
	if order == nil {
		return domain.ErrNotFound
	}
	order.NotifyCount++
	order.LastNotifyAt = copyTime(&at)
	order.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Store) RecordNotify(_ context.Context, log payment.NotifyLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifyLogs = append(s.notifyLogs, log)
	if len(s.notifyLogs) > 2000 {
		s.notifyLogs = s.notifyLogs[len(s.notifyLogs)-2000:]
	}
	return nil
}

func (s *Store) CreateSettlement(_ context.Context, settlement payment.Settlement) (*payment.Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if settlement.TaskID == "" || settlement.AmountFen <= 0 || settlement.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: invalid settlement", domain.ErrInvalidRequest)
	}
	for _, existing := range s.settlements {
		if existing.IdempotencyKey == settlement.IdempotencyKey {
			return cloneSettlement(existing), nil
		}
	}
	now := time.Now().UTC()
	settlement.CreatedAt, settlement.UpdatedAt = now, now
	if settlement.Status == "" {
		settlement.Status = payment.SettlementPending
	}
	if settlement.Currency == "" {
		settlement.Currency = payment.CurrencyCNY
	}
	s.settlements[settlement.ID] = cloneSettlement(&settlement)
	return cloneSettlement(&settlement), nil
}

func (s *Store) Settlement(_ context.Context, id string) (*payment.Settlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item := s.settlements[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	return cloneSettlement(item), nil
}

func (s *Store) SettlementByTask(_ context.Context, taskID string) (*payment.Settlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.settlements {
		if item.TaskID == taskID {
			return cloneSettlement(item), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) SettlementByProviderBillNo(_ context.Context, provider, billNo string) (*payment.Settlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.settlements {
		if item.Provider == provider && item.ProviderBillNo == billNo {
			return cloneSettlement(item), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) SettlementsByRider(_ context.Context, riderID string, page pagination.Page) ([]*payment.Settlement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*payment.Settlement, 0)
	for _, item := range s.settlements {
		if item.RiderID == riderID {
			items = append(items, cloneSettlement(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return pageSlice(items, page), nil
}

func (s *Store) ClaimDueSettlements(_ context.Context, now time.Time, limit int, lease time.Duration) ([]*payment.Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	due := make([]*payment.Settlement, 0)
	for _, item := range s.settlements {
		switch item.Status {
		case payment.SettlementPending:
			if item.NextAttemptAt == nil || !item.NextAttemptAt.After(now) {
				due = append(due, item)
			}
		case payment.SettlementProcessing:
			if item.LockedUntil != nil && !item.LockedUntil.After(now) {
				due = append(due, item)
			}
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].UpdatedAt.Before(due[j].UpdatedAt) })
	if len(due) > limit {
		due = due[:limit]
	}
	lockedUntil := now.Add(lease)
	result := make([]*payment.Settlement, 0, len(due))
	for _, item := range due {
		item.Status, item.Attempts = payment.SettlementProcessing, item.Attempts+1
		item.LockedUntil, item.UpdatedAt = copyTime(&lockedUntil), now
		result = append(result, cloneSettlement(item))
	}
	return result, nil
}

func (s *Store) MarkSettlementPaid(_ context.Context, id, billID string, paidAt time.Time) (*payment.Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.settlements[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status == payment.SettlementPaid {
		return cloneSettlement(item), nil
	}
	if !payment.CanTransitionSettlement(item.Status, payment.SettlementPaid) {
		return nil, fmt.Errorf("%w: settlement cannot be paid from %s", domain.ErrConflict, item.Status)
	}
	item.Status, item.ProviderPayoutID, item.PaidAt = payment.SettlementPaid, billID, copyTime(&paidAt)
	item.LockedUntil, item.FailReason, item.UpdatedAt = nil, "", time.Now().UTC()
	return cloneSettlement(item), nil
}

func (s *Store) MarkSettlementFailed(_ context.Context, id, reason string, retryAt *time.Time) (*payment.Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.settlements[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status == payment.SettlementPaid {
		return nil, fmt.Errorf("%w: settlement already paid", domain.ErrConflict)
	}
	next := payment.SettlementFailed
	if retryAt != nil {
		next = payment.SettlementPending
	}
	if item.Status != next && !payment.CanTransitionSettlement(item.Status, next) {
		return nil, fmt.Errorf("%w: settlement cannot move to %s from %s", domain.ErrConflict, next, item.Status)
	}
	item.Status, item.FailReason = next, reason
	item.NextAttemptAt, item.LockedUntil = copyTime(retryAt), nil
	item.UpdatedAt = time.Now().UTC()
	return cloneSettlement(item), nil
}

func (s *Store) MarkSettlementUnknown(_ context.Context, id, reason string) (*payment.Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.settlements[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status == payment.SettlementUnknown {
		return cloneSettlement(item), nil
	}
	if !payment.CanTransitionSettlement(item.Status, payment.SettlementUnknown) {
		return nil, fmt.Errorf("%w: settlement cannot move to UNKNOWN from %s", domain.ErrConflict, item.Status)
	}
	item.Status, item.FailReason = payment.SettlementUnknown, reason
	item.NextAttemptAt, item.LockedUntil = nil, nil
	item.UpdatedAt = time.Now().UTC()
	return cloneSettlement(item), nil
}

func (s *Store) CancelPendingSettlement(_ context.Context, id, reason string) (*payment.Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.settlements[id]
	if item == nil {
		return nil, domain.ErrNotFound
	}
	if item.Status != payment.SettlementPending {
		return nil, fmt.Errorf("%w: settlement is already %s", domain.ErrConflict, item.Status)
	}
	if !payment.CanTransitionSettlement(item.Status, payment.SettlementFailed) {
		return nil, fmt.Errorf("%w: settlement cannot be cancelled from %s", domain.ErrConflict, item.Status)
	}
	item.Status, item.FailReason = payment.SettlementFailed, reason
	item.NextAttemptAt, item.LockedUntil = nil, nil
	item.UpdatedAt = time.Now().UTC()
	return cloneSettlement(item), nil
}

func (s *Store) PayoutAccount(_ context.Context, riderID string) (*payment.PayoutAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	account := s.payoutAccounts[riderID]
	if account == nil {
		return nil, domain.ErrNotFound
	}
	copy := *account
	return &copy, nil
}

func (s *Store) SavePayoutAccount(_ context.Context, account payment.PayoutAccount) (*payment.PayoutAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if account.RiderID == "" || account.Provider == "" || account.Account == "" {
		return nil, fmt.Errorf("%w: rider, provider and account are required", domain.ErrInvalidRequest)
	}
	existing := s.payoutAccounts[account.RiderID]
	now := time.Now().UTC()
	if existing == nil {
		account.CreatedAt = now
	} else {
		account.CreatedAt = existing.CreatedAt
	}
	account.UpdatedAt = now
	copy := account
	s.payoutAccounts[account.RiderID] = &copy
	return &copy, nil
}
