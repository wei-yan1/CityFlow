package promotionapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cityflow/app/promotion/internal/domain/coupon"
)

// ClaimStore 是异步链路真正需要的两个 DB 能力：幂等落库、按 claimID 回读。
type ClaimStore interface {
	ClaimCoupon(ctx context.Context, userID, couponID, idempotencyKey string, now time.Time) (*coupon.UserCoupon, error)
	UserCoupon(ctx context.Context, userID, id string) (*coupon.UserCoupon, error)
}

// ClaimRequest / ClaimResult 是批量落库的输入输出。结果必须和请求同序，
// 这样 worker 才能把结果精确映射回每一条 Redis 预约。
type ClaimRequest struct {
	UserID         string
	CouponID       string
	IdempotencyKey string
}

type ClaimResult struct {
	Item *coupon.UserCoupon
	Err  error
}

// ClaimBatchStore 是可选的批量落库能力。实现可以只支持同一 coupon 的请求，
// worker 会先按 coupon 分组再调用，避免跨券事务导致部分成功、部分失败。
type ClaimBatchStore interface {
	ClaimCouponBatch(ctx context.Context, requests []ClaimRequest, now time.Time) ([]ClaimResult, error)
}

// ClaimReservation 是领券在 Redis 端的一次预约：预扣了库存、写入了幂等键、进了 Stream。
type ClaimReservation struct {
	EventID        string
	ClaimID        string
	CouponID       string
	UserID         string
	IdempotencyKey string
	Status         string // pending | ok | failed | miss | reject | conflict
	Reason         string
}

// ReservationStore 是 Redis 预扣层的抽象，实现必须保证 Reserve 是一个原子操作：
// 校验 + 扣剩余 + 校验/累加限领 + 写幂等键 + XADD 全部在同一个 Lua 里完成。
type ReservationStore interface {
	Reserve(ctx context.Context, userID, couponID, idempotencyKey, claimID string, now time.Time) (*ClaimReservation, error)
	Release(ctx context.Context, reservation *ClaimReservation, reason string) error
	Complete(ctx context.Context, reservation *ClaimReservation, claimID, status, reason string) error
	Result(ctx context.Context, userID, couponID, idempotencyKey string) (*ClaimReservation, bool, error)
}

// ClaimQueue 是 Redis Stream 消费者组的抽象。
type ClaimQueue interface {
	EnsureGroup(ctx context.Context) error
	Read(ctx context.Context, consumer string, count int64, block time.Duration) ([]*ClaimReservation, error)
	Ack(ctx context.Context, eventID string) error
	Reclaim(ctx context.Context, consumer string, minIdle time.Duration, count int64) ([]*ClaimReservation, error)
	Trim(ctx context.Context) error
}

// AsyncClaimer 是领券的异步入口：先在 Redis 原子预扣并投递事件，再短暂等待消费者落库。
// 等待超时返回 coupon.ErrPending，调用方必须用同一个幂等键重试；Redis 不可用时
// 返回 handled=false，由 Service 回退到同步 DB 事务。
type AsyncClaimer struct {
	store        ClaimStore
	reservations ReservationStore
	wait         time.Duration
	now          func() time.Time
}

func NewAsyncClaimer(store ClaimStore, reservations ReservationStore, wait time.Duration) *AsyncClaimer {
	return &AsyncClaimer{
		store: store, reservations: reservations, wait: wait,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (a *AsyncClaimer) Claim(ctx context.Context, userID, couponID, idempotencyKey string) (*coupon.UserCoupon, bool, error) {
	claimID, err := coupon.NewID()
	if err != nil {
		return nil, false, err
	}
	reservation, err := a.reservations.Reserve(ctx, userID, couponID, idempotencyKey, claimID, a.now())
	if err != nil {
		// Redis 客户端超时并不代表脚本没执行。先按幂等键回读一次：
		// 如果预扣和 XADD 已经成功，继续等结果，避免回退同步造成重复扣减。
		current, found, stateErr := a.reservations.Result(ctx, userID, couponID, idempotencyKey)
		if stateErr == nil && found {
			reservation = current
		} else {
			// 确实没留下预约（或 Redis 整体不可用）：交回 Service 走同步 DB 事务。
			log.Printf("promotion: reserve claim %s/%s: %v", couponID, userID, err)
			return nil, false, nil
		}
	}
	if reservation == nil {
		return nil, false, nil
	}
	switch reservation.Status {
	case "miss":
		return nil, false, nil
	case "reject":
		return nil, true, reservationFailure(reservation.Reason)
	case "conflict":
		return nil, true, reservationFailure("duplicate")
	case "failed":
		return nil, true, reservationFailure(reservation.Reason)
	}
	return a.waitForResult(ctx, userID, couponID, idempotencyKey)
}

func (a *AsyncClaimer) waitForResult(ctx context.Context, userID, couponID, idempotencyKey string) (*coupon.UserCoupon, bool, error) {
	deadline := time.Now().Add(a.wait)
	for {
		current, found, err := a.reservations.Result(ctx, userID, couponID, idempotencyKey)
		if err != nil {
			return nil, true, err
		}
		if found {
			switch current.Status {
			case "ok":
				if current.ClaimID != "" {
					item, loadErr := a.store.UserCoupon(ctx, userID, current.ClaimID)
					if loadErr == nil {
						return item, true, nil
					}
					if !errors.Is(loadErr, coupon.ErrNotFound) {
						return nil, true, loadErr
					}
				}
			case "failed":
				return nil, true, reservationFailure(current.Reason)
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, true, coupon.ErrPending
		}
		if remaining > 25*time.Millisecond {
			remaining = 25 * time.Millisecond
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, true, ctx.Err()
		case <-timer.C:
		}
	}
}

func reservationFailure(reason string) error {
	switch reason {
	case "soldout":
		return fmt.Errorf("%w: 该券已领完", coupon.ErrConflict)
	case "limit":
		return fmt.Errorf("%w: 你已达到该券的领取上限", coupon.ErrConflict)
	case "missing", "invalid":
		return fmt.Errorf("%w: 该券当前不可领取", coupon.ErrConflict)
	case "busy":
		return fmt.Errorf("%w: 领取人数过多，请稍后重试", coupon.ErrUnavailable)
	case "duplicate":
		return fmt.Errorf("%w: 该券已被领取", coupon.ErrConflict)
	default:
		return fmt.Errorf("%w: 领取失败，请稍后重试", coupon.ErrConflict)
	}
}

// ClaimWorker 消费 Redis Stream，把预约落成 MySQL claim。
// 消费幂等由 claim 表的 (coupon_id, user_id, idempotency_key) 唯一键保证：
// 同一条事件被重复投递，只会返回同一条 claim。
type ClaimWorker struct {
	store        ClaimStore
	batchStore   ClaimBatchStore
	queue        ClaimQueue
	reservations ReservationStore
	consumer     string
	batch        int64
	block        time.Duration
	reclaimIdle  time.Duration
	now          func() time.Time
}

func NewClaimWorker(store ClaimStore, queue ClaimQueue, reservations ReservationStore, consumer string, batch int64, block, reclaimIdle time.Duration) *ClaimWorker {
	if batch <= 0 {
		batch = 50
	}
	if block <= 0 {
		block = time.Second
	}
	if reclaimIdle <= 0 {
		reclaimIdle = 30 * time.Second
	}
	return &ClaimWorker{
		store: store, batchStore: asClaimBatchStore(store),
		queue: queue, reservations: reservations, consumer: consumer,
		batch: batch, block: block, reclaimIdle: reclaimIdle,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func asClaimBatchStore(store ClaimStore) ClaimBatchStore {
	batchStore, _ := store.(ClaimBatchStore)
	return batchStore
}

// Run 阻塞运行，直到 ctx 取消。启动时 Redis 不可用会按 2 秒退避重试，不退出进程。
func (w *ClaimWorker) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := w.queue.EnsureGroup(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("promotion: claim stream group: %v", err)
			if !sleepContext(ctx, 2*time.Second) {
				return
			}
			continue
		}
		break
	}

	lastReclaim := time.Now()
	for ctx.Err() == nil {
		batch, err := w.queue.Read(ctx, w.consumer, w.batch, w.block)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("promotion: read claim stream: %v", err)
			if !sleepContext(ctx, 500*time.Millisecond) {
				return
			}
		}
		w.processReservations(ctx, batch)
		if time.Since(lastReclaim) >= 30*time.Second {
			lastReclaim = time.Now()
			items, err := w.queue.Reclaim(ctx, w.consumer, w.reclaimIdle, w.batch)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("promotion: reclaim claim stream: %v", err)
				continue
			}
			w.processReservations(ctx, items)
			// 没有 pending 时裁剪已 ACK 的历史，避免 Stream 长度只增不减，
			// 最终把 XLEN 上限永久顶满。
			if terr := w.queue.Trim(ctx); terr != nil {
				log.Printf("promotion: trim claim stream: %v", terr)
			}
		}
	}
}

func (w *ClaimWorker) processReservations(ctx context.Context, reservations []*ClaimReservation) {
	if len(reservations) == 0 {
		return
	}
	if w.batchStore != nil && len(reservations) > 1 {
		w.ProcessBatch(ctx, reservations)
		return
	}
	for _, reservation := range reservations {
		w.Process(ctx, reservation)
	}
}

// ProcessBatch 按 coupon 分组，把一组预约交给一次数据库事务。
// 批量失败时回退逐条 Process，保证单条坏数据不会拖垮整批。
func (w *ClaimWorker) ProcessBatch(ctx context.Context, reservations []*ClaimReservation) {
	if w.batchStore == nil {
		for _, reservation := range reservations {
			w.Process(ctx, reservation)
		}
		return
	}
	groups := make(map[string][]*ClaimReservation)
	for _, reservation := range reservations {
		if reservation == nil || reservation.EventID == "" {
			continue
		}
		if reservation.UserID == "" || reservation.CouponID == "" || reservation.IdempotencyKey == "" {
			w.Process(ctx, reservation)
			continue
		}
		if w.terminalReservation(ctx, reservation) {
			continue
		}
		groups[reservation.CouponID] = append(groups[reservation.CouponID], reservation)
	}
	for _, group := range groups {
		requests := make([]ClaimRequest, 0, len(group))
		for _, reservation := range group {
			requests = append(requests, ClaimRequest{
				UserID: reservation.UserID, CouponID: reservation.CouponID,
				IdempotencyKey: reservation.IdempotencyKey,
			})
		}
		results, err := w.batchStore.ClaimCouponBatch(ctx, requests, w.now())
		if err != nil || len(results) != len(group) {
			if err == nil {
				err = fmt.Errorf("batch claim returned %d results for %d requests", len(results), len(group))
			}
			log.Printf("promotion: batch claim coupon %s failed, fallback to single: %v", group[0].CouponID, err)
			for _, reservation := range group {
				w.Process(ctx, reservation)
			}
			continue
		}
		for index, reservation := range group {
			w.applyOutcome(ctx, reservation, results[index].Item, results[index].Err)
		}
	}
}

// Process 处理单条预约。抽成导出方法，便于测试、排障和手工补偿。
func (w *ClaimWorker) Process(ctx context.Context, reservation *ClaimReservation) {
	if reservation == nil || reservation.EventID == "" {
		return
	}
	if reservation.UserID == "" || reservation.CouponID == "" || reservation.IdempotencyKey == "" {
		log.Printf("promotion: drop malformed claim event %s", reservation.EventID)
		_ = w.queue.Ack(ctx, reservation.EventID)
		return
	}
	if w.terminalReservation(ctx, reservation) {
		return
	}
	item, err := w.store.ClaimCoupon(ctx, reservation.UserID, reservation.CouponID, reservation.IdempotencyKey, w.now())
	w.applyOutcome(ctx, reservation, item, err)
}

// terminalReservation 检查预约是否已经有终态。
// 已经是 ok/failed 说明这条事件之前处理过，只是 ACK 丢了：直接补 ACK，
// 绝不能重跑数据库。失败补偿已经把库存还回去了，重跑可能在库存被别的
// 用户领走后“二次成功”。
func (w *ClaimWorker) terminalReservation(ctx context.Context, reservation *ClaimReservation) bool {
	current, found, err := w.reservations.Result(ctx, reservation.UserID, reservation.CouponID, reservation.IdempotencyKey)
	if err != nil {
		log.Printf("promotion: read reservation state %s: %v", reservation.EventID, err)
		return false
	}
	if found && (current.Status == "ok" || current.Status == "failed") {
		_ = w.queue.Ack(ctx, reservation.EventID)
		return true
	}
	return false
}

// applyOutcome 把一次落库结果写回预约：成功 Complete+Ack；永久失败 Release+Complete+Ack；
// 瞬时失败不 ACK，留给 XAUTOCLAIM 重试。
func (w *ClaimWorker) applyOutcome(ctx context.Context, reservation *ClaimReservation, item *coupon.UserCoupon, err error) {
	if err == nil && (item == nil || item.ID == "") {
		err = fmt.Errorf("%w: empty claim result", coupon.ErrUnavailable)
	}
	if err == nil {
		if cerr := w.reservations.Complete(ctx, reservation, item.ID, "ok", ""); cerr != nil {
			log.Printf("promotion: complete claim reservation %s: %v", reservation.EventID, cerr)
			return
		}
		if aerr := w.queue.Ack(ctx, reservation.EventID); aerr != nil {
			log.Printf("promotion: ack claim event %s: %v", reservation.EventID, aerr)
		}
		return
	}
	reason, permanent := claimFailureReason(err)
	if !permanent {
		// 数据库瞬时故障：不 ACK，留在 pending，由 XAUTOCLAIM 在空闲后重试。
		log.Printf("promotion: claim %s failed, will retry: %v", reservation.EventID, err)
		return
	}
	if rerr := w.reservations.Release(ctx, reservation, reason); rerr != nil {
		log.Printf("promotion: release reservation %s: %v", reservation.EventID, rerr)
		return
	}
	if cerr := w.reservations.Complete(ctx, reservation, "", "failed", reason); cerr != nil {
		log.Printf("promotion: fail reservation %s: %v", reservation.EventID, cerr)
		return
	}
	if aerr := w.queue.Ack(ctx, reservation.EventID); aerr != nil {
		log.Printf("promotion: ack failed event %s: %v", reservation.EventID, aerr)
	}
}

func claimFailureReason(err error) (string, bool) {
	switch {
	case errors.Is(err, coupon.ErrConflict):
		message := err.Error()
		if strings.Contains(message, "已领完") {
			return "soldout", true
		}
		if strings.Contains(message, "上限") {
			return "limit", true
		}
		return "conflict", true
	case errors.Is(err, coupon.ErrNotFound), errors.Is(err, coupon.ErrInvalidRequest):
		return "invalid", true
	default:
		return "", false
	}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
