package promotionapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cityflow/app/promotion/internal/domain/coupon"
)

type fakeStore struct {
	redeemCode       string
	redeemUser       string
	generatedInput   coupon.GenerateRedeemCodesInput
	generatedCodes   []*coupon.RedeemCode
	listFilter       coupon.RedeemCodeFilter
	disabledRedeemID string
	expireCalls      int
	releaseCalls     int
	claimUser        string
	claimCoupon      string
	claimKey         string
	coupons          []*coupon.Coupon
}

func (f *fakeStore) ListCoupons(context.Context, string, time.Time, Page) ([]*coupon.Coupon, error) {
	return f.coupons, nil
}
func (f *fakeStore) ListUserCoupons(context.Context, string, string, Page) ([]*coupon.UserCoupon, error) {
	return nil, nil
}
func (f *fakeStore) UserCoupon(context.Context, string, string) (*coupon.UserCoupon, error) {
	return nil, nil
}
func (f *fakeStore) ClaimCoupon(_ context.Context, userID, couponID, idempotencyKey string, _ time.Time) (*coupon.UserCoupon, error) {
	f.claimUser, f.claimCoupon, f.claimKey = userID, couponID, idempotencyKey
	return &coupon.UserCoupon{ID: "claim-1", UserID: userID, CouponID: couponID}, nil
}
func (f *fakeStore) ClaimByIdempotency(context.Context, string, string, string) (*coupon.UserCoupon, error) {
	return nil, coupon.ErrNotFound
}
func (f *fakeStore) Coupon(context.Context, string) (*coupon.Coupon, error) {
	return nil, coupon.ErrNotFound
}
func (f *fakeStore) RedeemCode(_ context.Context, userID, code string, _ time.Time) (*coupon.UserCoupon, error) {
	f.redeemUser, f.redeemCode = userID, code
	return &coupon.UserCoupon{ID: "claim", UserID: userID}, nil
}
func (f *fakeStore) GenerateRedeemCodes(_ context.Context, input coupon.GenerateRedeemCodesInput, _ time.Time) ([]*coupon.RedeemCode, error) {
	f.generatedInput = input
	return f.generatedCodes, nil
}
func (f *fakeStore) ListRedeemCodes(_ context.Context, filter coupon.RedeemCodeFilter, _ Page) ([]*coupon.RedeemCode, error) {
	f.listFilter = filter
	return f.generatedCodes, nil
}
func (f *fakeStore) DisableRedeemCode(_ context.Context, id string, _ time.Time) error {
	f.disabledRedeemID = id
	return nil
}
func (f *fakeStore) Lock(context.Context, string, string, string, time.Time) (*coupon.UserCoupon, error) {
	return nil, nil
}
func (f *fakeStore) MarkUsed(context.Context, string, string, string, int64, time.Time) error {
	return nil
}
func (f *fakeStore) Release(context.Context, string, string) error { return nil }
func (f *fakeStore) ExpireClaims(context.Context, time.Time, int) (int64, error) {
	f.expireCalls++
	return 0, nil
}
func (f *fakeStore) ReleaseStaleLocked(context.Context, time.Time, int) (int64, error) {
	f.releaseCalls++
	return 0, nil
}

func TestRedeemNormalizesCodeBeforeStore(t *testing.T) {
	store := &fakeStore{}
	service := New(store)
	if _, err := service.Redeem(context.Background(), "user-1", " abcd-efgh "); err != nil {
		t.Fatal(err)
	}
	if store.redeemUser != "user-1" || store.redeemCode != "ABCDEFGH" {
		t.Fatalf("store got user=%q code=%q", store.redeemUser, store.redeemCode)
	}
}

func TestGenerateRedeemCodesValidation(t *testing.T) {
	store := &fakeStore{generatedCodes: []*coupon.RedeemCode{{ID: "code-1"}}}
	service := New(store)
	ctx := context.Background()

	cases := []coupon.GenerateRedeemCodesInput{
		{CouponID: "coupon-1", CreatedBy: "admin", Count: 0, MaxRedeem: 10},
		{CouponID: "coupon-1", CreatedBy: "admin", Count: 2, MaxRedeem: 0},
		{CouponID: "", CreatedBy: "admin", Count: 2, MaxRedeem: 10},
		{CouponID: "coupon-1", CreatedBy: "", Count: 2, MaxRedeem: 10},
	}
	for index, input := range cases {
		if _, err := service.GenerateRedeemCodes(ctx, input); !errors.Is(err, coupon.ErrInvalidRequest) {
			t.Fatalf("case %d: err=%v, want invalid request", index, err)
		}
	}
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	end := start.Add(-time.Hour)
	if _, err := service.GenerateRedeemCodes(ctx, coupon.GenerateRedeemCodesInput{
		CouponID: "coupon-1", CreatedBy: "admin", Count: 2, MaxRedeem: 10, StartsAt: &start, EndsAt: &end,
	}); !errors.Is(err, coupon.ErrInvalidRequest) {
		t.Fatalf("reversed window err=%v, want invalid request", err)
	}

	valid := coupon.GenerateRedeemCodesInput{CouponID: "coupon-1", CreatedBy: "admin", Count: 2, MaxRedeem: 10}
	if _, err := service.GenerateRedeemCodes(ctx, valid); err != nil {
		t.Fatal(err)
	}
	if store.generatedInput.Count != 2 || store.generatedInput.CouponID != "coupon-1" {
		t.Fatalf("store got %+v", store.generatedInput)
	}
}

func TestRedeemCodesRejectsUnknownStatusAndDisableRequiresID(t *testing.T) {
	store := &fakeStore{}
	service := New(store)
	if _, err := service.RedeemCodes(context.Background(), coupon.RedeemCodeFilter{Status: "BROKEN"}, Page{}); !errors.Is(err, coupon.ErrInvalidRequest) {
		t.Fatalf("unknown status err=%v", err)
	}
	if err := service.DisableRedeemCode(context.Background(), " "); !errors.Is(err, coupon.ErrInvalidRequest) {
		t.Fatalf("empty id err=%v", err)
	}
	if err := service.DisableRedeemCode(context.Background(), "code-1"); err != nil {
		t.Fatal(err)
	}
	if store.disabledRedeemID != "code-1" {
		t.Fatalf("disabled id=%q", store.disabledRedeemID)
	}
}

func TestExpireClaimsRunsReleaseThenExpire(t *testing.T) {
	store := &fakeStore{}
	service := New(store)
	if _, _, err := service.ExpireClaims(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.releaseCalls != 1 || store.expireCalls != 1 {
		t.Fatalf("release=%d expire=%d, want one bounded pass each", store.releaseCalls, store.expireCalls)
	}
}

func TestClaimWithIdempotencyTrimsAndForwardsKey(t *testing.T) {
	store := &fakeStore{}
	service := New(store)
	item, err := service.ClaimWithIdempotency(context.Background(), " user-1 ", " coupon-1 ", " req-123 ")
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.UserID != "user-1" || item.CouponID != "coupon-1" {
		t.Fatalf("unexpected claim: %+v", item)
	}
	if store.claimUser != "user-1" || store.claimCoupon != "coupon-1" || store.claimKey != "req-123" {
		t.Fatalf("store got user=%q coupon=%q key=%q", store.claimUser, store.claimCoupon, store.claimKey)
	}
	if _, err := service.ClaimWithIdempotency(context.Background(), " ", "coupon-1", "req"); !errors.Is(err, coupon.ErrInvalidRequest) {
		t.Fatalf("empty user err=%v", err)
	}
	if _, err := service.ClaimWithIdempotency(context.Background(), "user-1", "coupon-1", strings.Repeat("x", 65)); !errors.Is(err, coupon.ErrInvalidRequest) {
		t.Fatalf("oversized key err=%v", err)
	}
}

func TestCouponCacheMaintenanceUsesModeSpecificRefresh(t *testing.T) {
	ctx := context.Background()
	item := &coupon.Coupon{ID: "coupon-1", Remaining: 8}
	store := &fakeStore{coupons: []*coupon.Coupon{item}}

	gate := &fakeGate{remaining: 10, hasRemaining: true}
	syncService := New(store).WithGate(gate)
	if _, err := syncService.RefreshCouponCache(ctx); err != nil {
		t.Fatal(err)
	}
	if gate.warmed != 1 || gate.warmRules != 0 {
		t.Fatalf("sync refresh warm=%d warmRules=%d, want full warm", gate.warmed, gate.warmRules)
	}
	if checked, repaired, err := syncService.ReconcileCouponCache(ctx); err != nil || checked != 1 || repaired != 1 {
		t.Fatalf("sync reconcile checked=%d repaired=%d err=%v", checked, repaired, err)
	}

	gate = &fakeGate{remaining: 10, hasRemaining: true}
	asyncService := New(store).WithGate(gate).WithAsyncClaimer(&AsyncClaimer{})
	if _, err := asyncService.RefreshCouponCache(ctx); err != nil {
		t.Fatal(err)
	}
	if gate.warmed != 0 || gate.warmRules != 1 {
		t.Fatalf("async refresh warm=%d warmRules=%d, want rules only", gate.warmed, gate.warmRules)
	}
	// 缓存高于 DB 是确定的漂移，异步模式也要向下修正。
	if _, repaired, err := asyncService.ReconcileCouponCache(ctx); err != nil || repaired != 1 {
		t.Fatalf("async reconcile repaired=%d err=%v", repaired, err)
	}
}

type fakeGate struct {
	decision     GateDecision
	reason       string
	warmed       int
	warmRules    int
	consumed     int
	reset        int
	remaining    int64
	hasRemaining bool
}

func (g *fakeGate) Check(context.Context, string, time.Time) (GateDecision, string, error) {
	return g.decision, g.reason, nil
}
func (g *fakeGate) Warm(context.Context, *coupon.Coupon) error {
	g.warmed++
	return nil
}
func (g *fakeGate) WarmRules(context.Context, *coupon.Coupon) error {
	g.warmRules++
	return nil
}
func (g *fakeGate) Consume(context.Context, string) error {
	g.consumed++
	return nil
}
func (g *fakeGate) Remaining(context.Context, string) (int64, bool, error) {
	return g.remaining, g.hasRemaining, nil
}
func (g *fakeGate) Reset(context.Context, *coupon.Coupon) error {
	g.reset++
	return nil
}

func TestClaimGateRejectsBeforeTouchingStore(t *testing.T) {
	store := &fakeStore{}
	service := New(store).WithGate(&fakeGate{decision: GateReject, reason: "该券已领完"})
	_, err := service.ClaimWithIdempotency(context.Background(), "user-1", "coupon-1", "req-1")
	if !errors.Is(err, coupon.ErrConflict) {
		t.Fatalf("gate reject err=%v, want conflict", err)
	}
	if store.claimUser != "" {
		t.Fatalf("gate reject should not reach the store")
	}
}

func TestClaimGatePassConsumesAfterStore(t *testing.T) {
	store := &fakeStore{}
	gate := &fakeGate{decision: GatePass}
	service := New(store).WithGate(gate)
	if _, err := service.ClaimWithIdempotency(context.Background(), "user-1", "coupon-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	if gate.consumed != 1 {
		t.Fatalf("consume calls = %d, want 1", gate.consumed)
	}
}
