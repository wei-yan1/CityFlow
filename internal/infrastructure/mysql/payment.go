package mysqlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/payment"
)

type paymentOrderRecord struct {
	ProviderOrderNo   *string    `gorm:"column:provider_order_no;size:64"`
	PayPayload        *string    `gorm:"column:pay_payload;size:1024"`
	PayURL            *string    `gorm:"column:pay_url;size:1024"`
	CouponClaimID     *string    `gorm:"column:coupon_claim_id;type:char(36)"`
	CouponTitle       *string    `gorm:"column:coupon_title;size:120"`
	ExpiresAt         *time.Time `gorm:"column:expires_at"`
	PaidAt            *time.Time `gorm:"column:paid_at"`
	LastNotifyAt      *time.Time `gorm:"column:last_notify_at"`
	FailReason        *string    `gorm:"column:fail_reason;size:255"`
	Provider          *string    `gorm:"column:provider;size:32"`
	ProviderTradeID   *string    `gorm:"column:provider_trade_id;size:128"`
	ID                string     `gorm:"column:id;type:char(36);primaryKey"`
	TaskID            string     `gorm:"column:task_id;type:char(36);not null"`
	PayerID           string     `gorm:"column:payer_id;type:char(36);not null"`
	AmountFen         int64      `gorm:"column:amount_fen;not null"`
	OriginalAmountFen int64      `gorm:"column:original_amount_fen;not null"`
	DiscountFen       int64      `gorm:"column:discount_fen;not null"`
	Currency          string     `gorm:"column:currency;size:3;not null"`
	Status            string     `gorm:"column:status;size:24;not null"`
	IdempotencyKey    string     `gorm:"column:idempotency_key;size:128;not null"`
	NotifyCount       int        `gorm:"column:notify_count;not null"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
}

func (paymentOrderRecord) TableName() string { return "payment_orders" }

type settlementRecord struct {
	ProviderBillNo   *string    `gorm:"column:provider_bill_no;size:64"`
	FailReason       *string    `gorm:"column:fail_reason;size:255"`
	NextAttemptAt    *time.Time `gorm:"column:next_attempt_at"`
	LockedUntil      *time.Time `gorm:"column:locked_until"`
	PaidAt           *time.Time `gorm:"column:paid_at"`
	Provider         *string    `gorm:"column:provider;size:32"`
	ProviderPayoutID *string    `gorm:"column:provider_payout_id;size:128"`
	ID               string     `gorm:"column:id;type:char(36);primaryKey"`
	TaskID           string     `gorm:"column:task_id;type:char(36);not null"`
	PaymentOrderID   string     `gorm:"column:payment_order_id;type:char(36);not null"`
	RiderID          string     `gorm:"column:rider_id;type:char(36);not null"`
	AmountFen        int64      `gorm:"column:amount_fen;not null"`
	FeeFen           int64      `gorm:"column:fee_fen;not null"`
	Currency         string     `gorm:"column:currency;size:3;not null"`
	Status           string     `gorm:"column:status;size:24;not null"`
	Attempts         int        `gorm:"column:attempts;not null"`
	IdempotencyKey   string     `gorm:"column:idempotency_key;size:128;not null"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
}

func (settlementRecord) TableName() string { return "rider_settlements" }

type paymentNotifyRecord struct {
	Provider        string    `gorm:"column:provider;size:32;not null"`
	ProviderOrderNo *string   `gorm:"column:provider_order_no;size:64"`
	ProviderTradeID *string   `gorm:"column:provider_trade_id;size:128"`
	Status          *string   `gorm:"column:status;size:24"`
	Result          string    `gorm:"column:result;size:64;not null"`
	Payload         *string   `gorm:"column:payload;type:text"`
	ReceivedAt      time.Time `gorm:"column:received_at"`
	SignatureValid  bool      `gorm:"column:signature_valid;not null"`
}

func (paymentNotifyRecord) TableName() string { return "payment_notifies" }

type payoutAccountRecord struct {
	RiderID     string    `gorm:"column:rider_id;type:char(36);primaryKey"`
	Provider    string    `gorm:"column:provider;size:16;not null"`
	Account     string    `gorm:"column:account;size:128;not null"`
	AccountName *string   `gorm:"column:account_name;size:80"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (payoutAccountRecord) TableName() string { return "rider_payout_accounts" }

func toPaymentOrder(record paymentOrderRecord) *payment.Order {
	return &payment.Order{
		ProviderOrderNo: stringValue(record.ProviderOrderNo), PayPayload: stringValue(record.PayPayload),
		PayURL: stringValue(record.PayURL), CouponClaimID: stringValue(record.CouponClaimID),
		CouponTitle: stringValue(record.CouponTitle), ExpiresAt: record.ExpiresAt, PaidAt: record.PaidAt,
		LastNotifyAt: record.LastNotifyAt, ID: record.ID, TaskID: record.TaskID, PayerID: record.PayerID,
		AmountFen: record.AmountFen, OriginalAmountFen: record.OriginalAmountFen, DiscountFen: record.DiscountFen,
		Currency: record.Currency, Status: record.Status,
		Provider: stringValue(record.Provider), ProviderTradeID: stringValue(record.ProviderTradeID),
		FailReason: stringValue(record.FailReason), NotifyCount: record.NotifyCount,
		IdempotencyKey: record.IdempotencyKey, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func toSettlement(record settlementRecord) *payment.Settlement {
	return &payment.Settlement{
		ProviderBillNo: stringValue(record.ProviderBillNo), FailReason: stringValue(record.FailReason),
		NextAttemptAt: record.NextAttemptAt, LockedUntil: record.LockedUntil, PaidAt: record.PaidAt,
		ID: record.ID, TaskID: record.TaskID, PaymentOrderID: record.PaymentOrderID, RiderID: record.RiderID,
		AmountFen: record.AmountFen, FeeFen: record.FeeFen, Currency: record.Currency, Status: record.Status,
		Provider: stringValue(record.Provider), ProviderPayoutID: stringValue(record.ProviderPayoutID),
		Attempts: record.Attempts, IdempotencyKey: record.IdempotencyKey,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func (s *Store) CreatePaymentOrder(ctx context.Context, order payment.Order) (*payment.Order, error) {
	if order.Currency == "" {
		order.Currency = payment.CurrencyCNY
	}
	if order.Status == "" {
		order.Status = payment.PaymentPending
	}
	var existing *payment.Order
	err := s.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Payment owns its orders. The task row is intentionally not locked:
		// Task is another service and its database is not ours to read or lock.
		// A unique active_task_id index still makes concurrent clicks create at
		// most one live order for a task.
		var idem paymentOrderRecord
		idemQuery := tx.Where("payer_id = ? AND idempotency_key = ?", order.PayerID, order.IdempotencyKey).Limit(1).Find(&idem)
		if idemQuery.Error != nil {
			return fmt.Errorf("lookup payment idempotency: %w", idemQuery.Error)
		}
		if idemQuery.RowsAffected == 1 {
			current := toPaymentOrder(idem)
			if !payment.SameOrderRequest(*current, order) {
				return fmt.Errorf("%w: idempotency key belongs to another payment request", domain.ErrConflict)
			}
			existing = current
			return nil
		}

		var active paymentOrderRecord
		activeQuery := tx.Where("task_id = ? AND status = ?", order.TaskID, payment.PaymentPending).Limit(1).Find(&active)
		if activeQuery.Error != nil {
			return fmt.Errorf("lookup active payment: %w", activeQuery.Error)
		}
		if activeQuery.RowsAffected == 1 {
			current := toPaymentOrder(active)
			if !payment.SameOrderRequest(*current, order) {
				return fmt.Errorf("%w: task already has an active payment for another request", domain.ErrConflict)
			}
			existing = current
			return nil
		}

		record := paymentOrderRecord{
			ProviderOrderNo: optionalString(order.ProviderOrderNo), PayPayload: optionalString(order.PayPayload),
			PayURL: optionalString(order.PayURL), CouponClaimID: optionalString(order.CouponClaimID),
			CouponTitle: optionalString(order.CouponTitle),
			ExpiresAt:   order.ExpiresAt, PaidAt: order.PaidAt,
			LastNotifyAt: order.LastNotifyAt, FailReason: optionalString(order.FailReason),
			Provider: optionalString(order.Provider), ProviderTradeID: optionalString(order.ProviderTradeID),
			ID: order.ID, TaskID: order.TaskID, PayerID: order.PayerID, AmountFen: order.AmountFen,
			OriginalAmountFen: order.OriginalAmountFen, DiscountFen: order.DiscountFen,
			Currency: order.Currency, Status: order.Status, IdempotencyKey: order.IdempotencyKey,
			NotifyCount: order.NotifyCount,
		}
		if err := tx.Create(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("%w: payment order already exists", domain.ErrConflict)
			}
			return fmt.Errorf("insert payment order: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return s.PaymentOrder(ctx, order.ID)
}

func (s *Store) PaymentOrder(ctx context.Context, id string) (*payment.Order, error) {
	var record paymentOrderRecord
	err := s.orm.WithContext(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load payment order: %w", err)
	}
	return toPaymentOrder(record), nil
}

func (s *Store) PaymentOrderByTask(ctx context.Context, taskID string) (*payment.Order, error) {
	var record paymentOrderRecord
	result := s.orm.WithContext(ctx).Where("task_id = ?", taskID).Order("created_at DESC").Order("id DESC").Limit(1).Find(&record)
	if result.Error != nil {
		return nil, fmt.Errorf("load task payment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	return toPaymentOrder(record), nil
}

func (s *Store) PaymentOrderByProviderNo(ctx context.Context, provider, providerOrderNo string) (*payment.Order, error) {
	var record paymentOrderRecord
	result := s.orm.WithContext(ctx).Where("provider = ? AND provider_order_no = ?", provider, providerOrderNo).Limit(1).Find(&record)
	if result.Error != nil {
		return nil, fmt.Errorf("load provider payment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	return toPaymentOrder(record), nil
}

func (s *Store) SaveChargePayload(ctx context.Context, id, payload, payURL string, expiresAt time.Time) error {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).Where("id = ?", id).
		Updates(map[string]any{"pay_payload": payload, "pay_url": optionalString(payURL), "expires_at": expiresAt, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("save payment payload: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := s.PaymentOrder(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) MarkPaymentPaid(ctx context.Context, id, tradeID string, paidAt time.Time) (*payment.Order, error) {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).
		Where("id = ? AND status = ?", id, payment.PaymentPending).
		Updates(map[string]any{
			"status": payment.PaymentPaid, "provider_trade_id": tradeID, "paid_at": paidAt,
			"fail_reason": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark payment paid: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.PaymentOrder(ctx, id)
	}
	current, err := s.PaymentOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.PaymentPaid {
		if current.ProviderTradeID == "" || current.ProviderTradeID == tradeID {
			return current, nil
		}
		return nil, fmt.Errorf("%w: payment already settled by another trade", domain.ErrConflict)
	}
	return nil, fmt.Errorf("%w: payment cannot be paid from %s", domain.ErrConflict, current.Status)
}

func (s *Store) MarkPaymentFailed(ctx context.Context, id, reason string) (*payment.Order, error) {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).
		Where("id = ? AND status = ?", id, payment.PaymentPending).
		Updates(map[string]any{"status": payment.PaymentFailed, "fail_reason": reason, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return nil, fmt.Errorf("mark payment failed: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.PaymentOrder(ctx, id)
	}
	current, err := s.PaymentOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.PaymentFailed {
		return current, nil
	}
	return nil, fmt.Errorf("%w: payment cannot be failed from %s", domain.ErrConflict, current.Status)
}

func (s *Store) ClosePaymentOrder(ctx context.Context, id string) (*payment.Order, error) {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).
		Where("id = ? AND status = ?", id, payment.PaymentPending).
		Updates(map[string]any{"status": payment.PaymentClosed, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return nil, fmt.Errorf("close payment: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.PaymentOrder(ctx, id)
	}
	current, err := s.PaymentOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.PaymentClosed {
		return current, nil
	}
	return nil, fmt.Errorf("%w: payment cannot be closed from %s", domain.ErrConflict, current.Status)
}

func (s *Store) MarkPaymentRefundPending(ctx context.Context, id string) (*payment.Order, error) {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).
		Where("id = ? AND status IN ?", id, []string{payment.PaymentPaid, payment.PaymentRefundFailed}).
		Updates(map[string]any{
			"status": payment.PaymentRefundPending, "fail_reason": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark payment refund pending: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.PaymentOrder(ctx, id)
	}
	current, err := s.PaymentOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.PaymentRefundPending {
		return current, nil
	}
	return nil, fmt.Errorf("%w: payment cannot move to REFUND_PENDING from %s", domain.ErrConflict, current.Status)
}

func (s *Store) MarkPaymentRefunded(ctx context.Context, id string) (*payment.Order, error) {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).
		Where("id = ? AND status IN ?", id, []string{payment.PaymentPaid, payment.PaymentRefundPending, payment.PaymentRefundFailed}).
		Updates(map[string]any{
			"status": payment.PaymentRefunded, "fail_reason": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark payment refunded: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.PaymentOrder(ctx, id)
	}
	current, err := s.PaymentOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.PaymentRefunded {
		return current, nil
	}
	return nil, fmt.Errorf("%w: payment cannot move to REFUNDED from %s", domain.ErrConflict, current.Status)
}

func (s *Store) MarkPaymentRefundFailed(ctx context.Context, id, reason string) (*payment.Order, error) {
	result := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).
		Where("id = ? AND status = ?", id, payment.PaymentRefundPending).
		Updates(map[string]any{
			"status": payment.PaymentRefundFailed, "fail_reason": reason, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark payment refund failed: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.PaymentOrder(ctx, id)
	}
	current, err := s.PaymentOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.PaymentRefundFailed {
		return current, nil
	}
	return nil, fmt.Errorf("%w: payment cannot move to REFUND_FAILED from %s", domain.ErrConflict, current.Status)
}

func (s *Store) PendingPayments(ctx context.Context, updatedBefore time.Time, limit int) ([]*payment.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var records []paymentOrderRecord
	if err := s.orm.WithContext(ctx).
		Where("status = ? AND updated_at <= ?", payment.PaymentPending, updatedBefore).
		Order("updated_at ASC").Order("id").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list pending payments: %w", err)
	}
	result := make([]*payment.Order, 0, len(records))
	for _, record := range records {
		result = append(result, toPaymentOrder(record))
	}
	return result, nil
}

func (s *Store) RefundPendingPayments(ctx context.Context, updatedBefore time.Time, limit int) ([]*payment.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var records []paymentOrderRecord
	if err := s.orm.WithContext(ctx).
		Where("status = ? AND updated_at <= ?", payment.PaymentRefundPending, updatedBefore).
		Order("updated_at ASC").Order("id").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list refund pending payments: %w", err)
	}
	result := make([]*payment.Order, 0, len(records))
	for _, record := range records {
		result = append(result, toPaymentOrder(record))
	}
	return result, nil
}

func (s *Store) RefundFailedPayments(ctx context.Context, updatedBefore time.Time, limit int) ([]*payment.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var records []paymentOrderRecord
	if err := s.orm.WithContext(ctx).
		Where("status = ? AND updated_at <= ?", payment.PaymentRefundFailed, updatedBefore).
		Order("updated_at ASC").Order("id").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list refund failed payments: %w", err)
	}
	result := make([]*payment.Order, 0, len(records))
	for _, record := range records {
		result = append(result, toPaymentOrder(record))
	}
	return result, nil
}

func (s *Store) NotePaymentNotify(ctx context.Context, id string, at time.Time) error {
	err := s.orm.WithContext(ctx).Model(&paymentOrderRecord{}).Where("id = ?", id).
		Updates(map[string]any{"notify_count": gorm.Expr("notify_count + 1"), "last_notify_at": at, "updated_at": time.Now().UTC()}).Error
	if err != nil {
		return fmt.Errorf("note payment notify: %w", err)
	}
	return nil
}

func (s *Store) RecordNotify(ctx context.Context, log payment.NotifyLog) error {
	record := paymentNotifyRecord{
		Provider: log.Provider, ProviderOrderNo: optionalString(log.ProviderOrderNo),
		ProviderTradeID: optionalString(log.ProviderTradeID), Status: optionalString(log.Status),
		Result: log.Result, Payload: optionalString(log.Payload), ReceivedAt: log.ReceivedAt,
		SignatureValid: log.SignatureValid,
	}
	if err := s.orm.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("record payment notify: %w", err)
	}
	return nil
}

// RecordNotifyBatch inserts a batch of provider callback audit rows in one
// multi-row statement. The audit table is append-only, so ordering the inserts
// together does not change the order-sensitive order/settlement state machine.
func (s *Store) RecordNotifyBatch(ctx context.Context, logs []payment.NotifyLog) error {
	if len(logs) == 0 {
		return nil
	}
	records := make([]paymentNotifyRecord, 0, len(logs))
	for _, log := range logs {
		records = append(records, paymentNotifyRecord{
			Provider: log.Provider, ProviderOrderNo: optionalString(log.ProviderOrderNo),
			ProviderTradeID: optionalString(log.ProviderTradeID), Status: optionalString(log.Status),
			Result: log.Result, Payload: optionalString(log.Payload), ReceivedAt: log.ReceivedAt,
			SignatureValid: log.SignatureValid,
		})
	}
	if err := s.orm.WithContext(ctx).CreateInBatches(records, len(records)).Error; err != nil {
		return fmt.Errorf("record payment notify batch: %w", err)
	}
	return nil
}

func (s *Store) CreateSettlement(ctx context.Context, settlement payment.Settlement) (*payment.Settlement, error) {
	record := settlementRecord{
		ProviderBillNo: optionalString(settlement.ProviderBillNo), FailReason: optionalString(settlement.FailReason),
		NextAttemptAt: settlement.NextAttemptAt, LockedUntil: settlement.LockedUntil, PaidAt: settlement.PaidAt,
		Provider: optionalString(settlement.Provider), ProviderPayoutID: optionalString(settlement.ProviderPayoutID),
		ID: settlement.ID, TaskID: settlement.TaskID, PaymentOrderID: settlement.PaymentOrderID,
		RiderID: settlement.RiderID, AmountFen: settlement.AmountFen, FeeFen: settlement.FeeFen, Currency: settlement.Currency,
		Status: settlement.Status, Attempts: settlement.Attempts, IdempotencyKey: settlement.IdempotencyKey,
	}
	if err := s.orm.WithContext(ctx).Create(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			var existing settlementRecord
			lookup := s.orm.WithContext(ctx).Where("idempotency_key = ?", settlement.IdempotencyKey).Take(&existing).Error
			if lookup == nil {
				return toSettlement(existing), nil
			}
			return nil, fmt.Errorf("%w: settlement already exists", domain.ErrConflict)
		}
		return nil, fmt.Errorf("insert settlement: %w", err)
	}
	return s.Settlement(ctx, settlement.ID)
}

func (s *Store) Settlement(ctx context.Context, id string) (*payment.Settlement, error) {
	var record settlementRecord
	err := s.orm.WithContext(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load settlement: %w", err)
	}
	return toSettlement(record), nil
}

func (s *Store) SettlementByTask(ctx context.Context, taskID string) (*payment.Settlement, error) {
	var record settlementRecord
	result := s.orm.WithContext(ctx).Where("task_id = ?", taskID).Limit(1).Find(&record)
	if result.Error != nil {
		return nil, fmt.Errorf("load task settlement: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	return toSettlement(record), nil
}

func (s *Store) SettlementByProviderBillNo(ctx context.Context, provider, billNo string) (*payment.Settlement, error) {
	var record settlementRecord
	result := s.orm.WithContext(ctx).Where("provider = ? AND provider_bill_no = ?", provider, billNo).Limit(1).Find(&record)
	if result.Error != nil {
		return nil, fmt.Errorf("load provider settlement: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, domain.ErrNotFound
	}
	return toSettlement(record), nil
}

func (s *Store) SettlementsByRider(ctx context.Context, riderID string, page repository.Page) ([]*payment.Settlement, error) {
	var records []settlementRecord
	if err := s.orm.WithContext(ctx).Where("rider_id = ?", riderID).Order("created_at DESC").Order("id").
		Scopes(paginate(page)).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list rider settlements: %w", err)
	}
	result := make([]*payment.Settlement, 0, len(records))
	for _, record := range records {
		result = append(result, toSettlement(record))
	}
	return result, nil
}

func (s *Store) ClaimDueSettlements(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]*payment.Settlement, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	ids := make([]string, 0, limit)
	lockedUntil := now.Add(lease)
	err := s.orm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		type settlementID struct {
			ID string `gorm:"column:id"`
		}
		var rows []settlementID
		query := `SELECT id FROM rider_settlements
			WHERE (status = 'PENDING' AND (next_attempt_at IS NULL OR next_attempt_at <= ?))
			   OR (status = 'PROCESSING' AND locked_until IS NOT NULL AND locked_until <= ?)
			ORDER BY updated_at ASC LIMIT ? FOR UPDATE SKIP LOCKED`
		if err := tx.Raw(query, now, now, limit).Scan(&rows).Error; err != nil {
			return fmt.Errorf("select due settlements: %w", err)
		}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Model(&settlementRecord{}).Where("id IN ?", ids).Updates(map[string]any{
			"status": payment.SettlementProcessing, "attempts": gorm.Expr("attempts + 1"),
			"locked_until": lockedUntil, "updated_at": now,
		}).Error; err != nil {
			return fmt.Errorf("claim settlements: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var records []settlementRecord
	if err := s.orm.WithContext(ctx).Where("id IN ?", ids).Order("updated_at").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load claimed settlements: %w", err)
	}
	result := make([]*payment.Settlement, 0, len(records))
	for _, record := range records {
		result = append(result, toSettlement(record))
	}
	return result, nil
}

func (s *Store) MarkSettlementPaid(ctx context.Context, id, billID string, paidAt time.Time) (*payment.Settlement, error) {
	result := s.orm.WithContext(ctx).Model(&settlementRecord{}).
		Where("id = ? AND status IN ?", id, []string{payment.SettlementPending, payment.SettlementProcessing}).
		Updates(map[string]any{
			"status": payment.SettlementPaid, "provider_payout_id": billID, "paid_at": paidAt,
			"locked_until": nil, "fail_reason": nil, "next_attempt_at": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark settlement paid: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.Settlement(ctx, id)
	}
	current, err := s.Settlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.SettlementPaid {
		return current, nil
	}
	return nil, fmt.Errorf("%w: settlement cannot be paid from %s", domain.ErrConflict, current.Status)
}

func (s *Store) MarkSettlementFailed(ctx context.Context, id, reason string, retryAt *time.Time) (*payment.Settlement, error) {
	next := payment.SettlementFailed
	if retryAt != nil {
		next = payment.SettlementPending
	}
	result := s.orm.WithContext(ctx).Model(&settlementRecord{}).
		Where("id = ? AND status IN ?", id, []string{payment.SettlementPending, payment.SettlementProcessing}).
		Updates(map[string]any{
			"status": next, "fail_reason": reason, "next_attempt_at": retryAt,
			"locked_until": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark settlement failed: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.Settlement(ctx, id)
	}
	current, err := s.Settlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == next {
		return current, nil
	}
	return nil, fmt.Errorf("%w: settlement cannot move to %s from %s", domain.ErrConflict, next, current.Status)
}

func (s *Store) MarkSettlementUnknown(ctx context.Context, id, reason string) (*payment.Settlement, error) {
	result := s.orm.WithContext(ctx).Model(&settlementRecord{}).
		Where("id = ? AND status = ?", id, payment.SettlementProcessing).
		Updates(map[string]any{
			"status": payment.SettlementUnknown, "fail_reason": reason,
			"next_attempt_at": nil, "locked_until": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("mark settlement unknown: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.Settlement(ctx, id)
	}
	current, err := s.Settlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.SettlementUnknown {
		return current, nil
	}
	return nil, fmt.Errorf("%w: settlement cannot move to UNKNOWN from %s", domain.ErrConflict, current.Status)
}

func (s *Store) CancelPendingSettlement(ctx context.Context, id, reason string) (*payment.Settlement, error) {
	result := s.orm.WithContext(ctx).Model(&settlementRecord{}).
		Where("id = ? AND status = ?", id, payment.SettlementPending).
		Updates(map[string]any{
			"status": payment.SettlementFailed, "fail_reason": reason,
			"next_attempt_at": nil, "locked_until": nil, "updated_at": time.Now().UTC(),
		})
	if result.Error != nil {
		return nil, fmt.Errorf("cancel pending settlement: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		return s.Settlement(ctx, id)
	}
	current, err := s.Settlement(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Status == payment.SettlementFailed {
		return current, nil
	}
	return nil, fmt.Errorf("%w: settlement is already %s", domain.ErrConflict, current.Status)
}

func (s *Store) PayoutAccount(ctx context.Context, riderID string) (*payment.PayoutAccount, error) {
	var record payoutAccountRecord
	err := s.orm.WithContext(ctx).Where("rider_id = ?", riderID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load payout account: %w", err)
	}
	return &payment.PayoutAccount{
		RiderID: record.RiderID, Provider: record.Provider, Account: record.Account,
		AccountName: stringValue(record.AccountName), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

func (s *Store) SavePayoutAccount(ctx context.Context, account payment.PayoutAccount) (*payment.PayoutAccount, error) {
	record := payoutAccountRecord{
		RiderID: account.RiderID, Provider: account.Provider, Account: account.Account,
		AccountName: optionalString(account.AccountName),
	}
	if err := s.orm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "rider_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"provider", "account", "account_name", "updated_at"}),
	}).Create(&record).Error; err != nil {
		return nil, fmt.Errorf("save payout account: %w", err)
	}
	return s.PayoutAccount(ctx, account.RiderID)
}
