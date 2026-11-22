package promotionadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/app/promotion/internal/domain/coupon"
	platformmysql "cityflow/internal/platform/mysql"
)

func TestStoreRedeemCodeConcurrencyAndIdempotency(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_PROMOTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set CITYFLOW_PROMOTION_TEST_DSN to run MySQL integration test")
	}
	ctx := context.Background()
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := platformmysql.MigrateService(ctx, db, "promotion"); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	now := time.Now().UTC()
	couponID, err := coupon.NewID()
	if err != nil {
		t.Fatal(err)
	}
	code := "TEST" + couponID[len(couponID)-8:]
	campaign := couponRecord{
		ID: couponID, Code: code, Title: "兑换码并发测试券", Type: coupon.TypeAmount,
		ValueFen: 100, TotalQuantity: 10, PerUserLimit: 1, Status: coupon.StatusActive,
		ApplicableTaskTypes: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.ORM.WithContext(ctx).Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupon_redeem_record WHERE coupon_id = ?", couponID).Error
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupon_redeem_code WHERE coupon_id = ?", couponID).Error
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupon_claims WHERE coupon_id = ?", couponID).Error
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupons WHERE id = ?", couponID).Error
	}()

	codes, err := store.GenerateRedeemCodes(ctx, coupon.GenerateRedeemCodesInput{
		CouponID: couponID, Count: 3, MaxRedeem: 1, BatchNo: "TEST-" + couponID[:8],
		CreatedBy: "integration-test", Note: "concurrency",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 3 || codes[0].Code == codes[1].Code || codes[1].Code == codes[2].Code {
		t.Fatalf("codes=%+v", codes)
	}
	legacyID, err := coupon.NewID()
	if err != nil {
		t.Fatal(err)
	}
	legacyCode := "LEG" + couponID[len(couponID)-8:]
	if err := db.ORM.WithContext(ctx).Create(&redeemCodeRecord{
		ID: legacyID, CouponID: couponID, BatchNo: "LEGACY-TEST", Code: legacyCode,
		MaxRedeem: 1, Status: coupon.RedeemStatusActive, CreatedBy: "legacy",
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	legacyRows, err := store.ListRedeemCodes(ctx, coupon.RedeemCodeFilter{CouponID: couponID, BatchNo: "LEGACY-TEST"}, promotionapp.Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(legacyRows) != 1 || legacyRows[0].Seq != 0 {
		t.Fatalf("legacy rows=%+v, want one row with seq=0", legacyRows)
	}
	generatedInput := coupon.GenerateRedeemCodesInput{
		CouponID: couponID, Count: 3, MaxRedeem: 1, BatchNo: "TEST-" + couponID[:8],
		CreatedBy: "integration-test", Note: "concurrency",
	}
	replayedBatch, err := store.GenerateRedeemCodes(ctx, generatedInput, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(replayedBatch) != len(codes) || replayedBatch[0].ID != codes[0].ID {
		t.Fatalf("replayed batch=%+v, want original codes", replayedBatch)
	}
	var batchCount int64
	if err := db.ORM.WithContext(ctx).Model(&redeemCodeRecord{}).
		Where("coupon_id = ? AND batch_no = ?", couponID, generatedInput.BatchNo).
		Count(&batchCount).Error; err != nil {
		t.Fatal(err)
	}
	if batchCount != int64(len(codes)) {
		t.Fatalf("batch rows=%d, want %d", batchCount, len(codes))
	}

	raceBatch := "RACE-" + couponID[:8]
	raceInput := coupon.GenerateRedeemCodesInput{
		CouponID: couponID, Count: 2, MaxRedeem: 1, BatchNo: raceBatch,
		CreatedBy: "integration-test", Note: "batch-race",
	}
	startGenerate := make(chan struct{})
	raceResults := make([][]*coupon.RedeemCode, 2)
	raceErrors := make([]error, 2)
	var raceWG sync.WaitGroup
	for index := 0; index < 2; index++ {
		raceWG.Add(1)
		go func(index int) {
			defer raceWG.Done()
			<-startGenerate
			raceResults[index], raceErrors[index] = store.GenerateRedeemCodes(ctx, raceInput, now)
		}(index)
	}
	close(startGenerate)
	raceWG.Wait()
	for index, raceErr := range raceErrors {
		if raceErr != nil {
			t.Fatalf("concurrent generate %d: %v", index, raceErr)
		}
	}
	if len(raceResults[0]) != 2 || len(raceResults[1]) != 2 || raceResults[0][0].ID != raceResults[1][0].ID {
		t.Fatalf("concurrent generate returned different batches: %+v / %+v", raceResults[0], raceResults[1])
	}
	var raceCount int64
	if err := db.ORM.WithContext(ctx).Model(&redeemCodeRecord{}).
		Where("coupon_id = ? AND batch_no = ?", couponID, raceBatch).Count(&raceCount).Error; err != nil {
		t.Fatal(err)
	}
	if raceCount != 2 {
		t.Fatalf("race batch rows=%d, want 2", raceCount)
	}

	start := make(chan struct{})
	results := make([]error, 2)
	var wg sync.WaitGroup
	for index, userID := range []string{"user-a", "user-b"} {
		wg.Add(1)
		go func(index int, userID string) {
			defer wg.Done()
			<-start
			_, results[index] = store.RedeemCode(ctx, userID, codes[0].Code, now)
		}(index, userID)
	}
	close(start)
	wg.Wait()
	successes := 0
	for _, result := range results {
		if result == nil {
			successes++
		} else if !isConflict(result) {
			t.Fatalf("unexpected concurrent redeem error: %v", result)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d, want exactly one winner", successes)
	}
	var codeRow redeemCodeRecord
	if err := db.ORM.WithContext(ctx).Where("id = ?", codes[0].ID).Take(&codeRow).Error; err != nil {
		t.Fatal(err)
	}
	if codeRow.UsedCount != 1 {
		t.Fatalf("used_count=%d, want 1", codeRow.UsedCount)
	}
	var claimCount int64
	if err := db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).Where("coupon_id = ?", couponID).Count(&claimCount).Error; err != nil {
		t.Fatal(err)
	}
	if claimCount != 1 {
		t.Fatalf("claims=%d, want 1", claimCount)
	}

	first, err := store.RedeemCode(ctx, "user-c", codes[1].Code, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.RedeemCode(ctx, "user-c", codes[1].Code, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("idempotent replay returned %q then %q", first.ID, second.ID)
	}
	var replayRow redeemCodeRecord
	if err := db.ORM.WithContext(ctx).Where("id = ?", codes[1].ID).Take(&replayRow).Error; err != nil {
		t.Fatal(err)
	}
	if replayRow.UsedCount != 1 {
		t.Fatalf("replay used_count=%d, want 1", replayRow.UsedCount)
	}

	start = make(chan struct{})
	duplicateResults := make([]*coupon.UserCoupon, 2)
	duplicateErrors := make([]error, 2)
	var duplicateWG sync.WaitGroup
	for index := 0; index < 2; index++ {
		duplicateWG.Add(1)
		go func(index int) {
			defer duplicateWG.Done()
			<-start
			duplicateResults[index], duplicateErrors[index] = store.RedeemCode(ctx, "user-d", codes[2].Code, now)
		}(index)
	}
	close(start)
	duplicateWG.Wait()
	for index, duplicateErr := range duplicateErrors {
		if duplicateErr != nil {
			t.Fatalf("same-user concurrent redeem %d: %v", index, duplicateErr)
		}
	}
	if duplicateResults[0].ID != duplicateResults[1].ID {
		t.Fatalf("same-user concurrent claims differ: %q vs %q", duplicateResults[0].ID, duplicateResults[1].ID)
	}
	var duplicateRow redeemCodeRecord
	if err := db.ORM.WithContext(ctx).Where("id = ?", codes[2].ID).Take(&duplicateRow).Error; err != nil {
		t.Fatal(err)
	}
	if duplicateRow.UsedCount != 1 {
		t.Fatalf("concurrent duplicate used_count=%d, want 1", duplicateRow.UsedCount)
	}
}

func TestStoreClaimIdempotencyAndOversell(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_PROMOTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set CITYFLOW_PROMOTION_TEST_DSN to run MySQL integration test")
	}
	ctx := context.Background()
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := platformmysql.MigrateService(ctx, db, "promotion"); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	now := time.Now().UTC()
	couponID, err := coupon.NewID()
	if err != nil {
		t.Fatal(err)
	}
	campaign := couponRecord{
		ID: couponID, Code: "CLM" + couponID[len(couponID)-8:], Title: "领券幂等测试",
		Type: coupon.TypeAmount, ValueFen: 100, TotalQuantity: 3, PerUserLimit: 2,
		Status: coupon.StatusActive, ApplicableTaskTypes: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.ORM.WithContext(ctx).Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupon_claims WHERE coupon_id = ?", couponID).Error
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupons WHERE id = ?", couponID).Error
	}()

	const workers = 8
	results := make([]*coupon.UserCoupon, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index], errs[index] = store.ClaimCoupon(ctx, "user-a", couponID, "same-key", now)
		}(index)
	}
	close(start)
	wg.Wait()
	for index, claimErr := range errs {
		if claimErr != nil {
			t.Fatalf("concurrent claim %d: %v", index, claimErr)
		}
	}
	for index := 1; index < workers; index++ {
		if results[index].ID != results[0].ID {
			t.Fatalf("same idempotency key returned different claims: %q vs %q", results[0].ID, results[index].ID)
		}
	}
	var claimCount int64
	if err := db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).
		Where("coupon_id = ?", couponID).Count(&claimCount).Error; err != nil {
		t.Fatal(err)
	}
	if claimCount != 1 {
		t.Fatalf("claims after idempotent burst = %d, want 1", claimCount)
	}

	if _, err := store.ClaimCoupon(ctx, "user-a", couponID, "second-key", now); err != nil {
		t.Fatalf("second claim under per-user limit: %v", err)
	}
	if _, err := store.ClaimCoupon(ctx, "user-a", couponID, "third-key", now); !isConflict(err) {
		t.Fatalf("third claim error=%v, want per-user limit conflict", err)
	}

	userResults := make([]*coupon.UserCoupon, 5)
	userErrs := make([]error, 5)
	start = make(chan struct{})
	var userWG sync.WaitGroup
	for index := 0; index < 5; index++ {
		userWG.Add(1)
		go func(index int) {
			defer userWG.Done()
			<-start
			userResults[index], userErrs[index] = store.ClaimCoupon(
				ctx, fmt.Sprintf("user-%d", index), couponID, fmt.Sprintf("user-key-%d", index), now)
		}(index)
	}
	close(start)
	userWG.Wait()
	successes := 0
	for index, claimErr := range userErrs {
		if claimErr == nil {
			successes++
			if userResults[index] == nil {
				t.Fatalf("nil claim without error at %d", index)
			}
			continue
		}
		if !isConflict(claimErr) {
			t.Fatalf("unexpected oversell error at %d: %v", index, claimErr)
		}
	}
	if successes != 1 {
		t.Fatalf("successes=%d, want exactly one remaining unit", successes)
	}
	var row couponRecord
	if err := db.ORM.WithContext(ctx).Where("id = ?", couponID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ClaimedCount != row.TotalQuantity {
		t.Fatalf("claimed_count=%d total=%d, want sold out without oversell", row.ClaimedCount, row.TotalQuantity)
	}
}

func TestStoreClaimCouponBatchGroupsAndLimits(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_PROMOTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set CITYFLOW_PROMOTION_TEST_DSN to run MySQL integration test")
	}
	ctx := context.Background()
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := platformmysql.MigrateService(ctx, db, "promotion"); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	now := time.Now().UTC()
	couponID, err := coupon.NewID()
	if err != nil {
		t.Fatal(err)
	}
	campaign := couponRecord{
		ID: couponID, Code: "BATCH" + couponID[len(couponID)-5:], Title: "批量领券测试",
		Type: coupon.TypeAmount, ValueFen: 100, TotalQuantity: 5, PerUserLimit: 2,
		Status: coupon.StatusActive, ApplicableTaskTypes: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.ORM.WithContext(ctx).Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupon_claims WHERE coupon_id = ?", couponID).Error
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupons WHERE id = ?", couponID).Error
	}()

	first, err := store.ClaimCouponBatch(ctx, []promotionapp.ClaimRequest{
		{UserID: "batch-a", CouponID: couponID, IdempotencyKey: "a1"},
		{UserID: "batch-b", CouponID: couponID, IdempotencyKey: "b1"},
		{UserID: "batch-c", CouponID: couponID, IdempotencyKey: "c1"},
		{UserID: "batch-a", CouponID: couponID, IdempotencyKey: "a1"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 4 {
		t.Fatalf("first results=%d", len(first))
	}
	for index := 0; index < 3; index++ {
		if first[index].Err != nil || first[index].Item == nil {
			t.Fatalf("first result %d = %+v", index, first[index])
		}
	}
	if first[3].Err != nil || first[3].Item == nil || first[3].Item.ID != first[0].Item.ID {
		t.Fatalf("duplicate key result=%+v, want same claim as first", first[3])
	}

	second, err := store.ClaimCouponBatch(ctx, []promotionapp.ClaimRequest{
		{UserID: "batch-a", CouponID: couponID, IdempotencyKey: "a2"},
		{UserID: "batch-a", CouponID: couponID, IdempotencyKey: "a3"},
		{UserID: "batch-b", CouponID: couponID, IdempotencyKey: "b1"},
		{UserID: "batch-d", CouponID: couponID, IdempotencyKey: "d1"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Err != nil || second[0].Item == nil {
		t.Fatalf("a2 should be admitted: %+v", second[0])
	}
	if !isConflict(second[1].Err) {
		t.Fatalf("a3 error=%v, want per-user limit conflict", second[1].Err)
	}
	if second[2].Err != nil || second[2].Item == nil || second[2].Item.ID != first[1].Item.ID {
		t.Fatalf("b1 replay=%+v, want existing claim", second[2])
	}
	if second[3].Err != nil || second[3].Item == nil {
		t.Fatalf("d1 should fill last unit: %+v", second[3])
	}

	third, err := store.ClaimCouponBatch(ctx, []promotionapp.ClaimRequest{
		{UserID: "batch-e", CouponID: couponID, IdempotencyKey: "e1"},
		{UserID: "batch-f", CouponID: couponID, IdempotencyKey: "f1"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	for index := range third {
		if !isConflict(third[index].Err) {
			t.Fatalf("soldout result %d error=%v", index, third[index].Err)
		}
	}

	var row couponRecord
	if err := db.ORM.WithContext(ctx).Where("id = ?", couponID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ClaimedCount != 5 {
		t.Fatalf("claimed_count=%d, want 5", row.ClaimedCount)
	}
	var claims int64
	if err := db.ORM.WithContext(ctx).Model(&couponClaimRecord{}).
		Where("coupon_id = ?", couponID).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 5 {
		t.Fatalf("claims=%d, want 5", claims)
	}
}

func TestStoreClaimCouponBatchConcurrentNoOversell(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_PROMOTION_TEST_DSN")
	if dsn == "" {
		t.Skip("set CITYFLOW_PROMOTION_TEST_DSN to run MySQL integration test")
	}
	ctx := context.Background()
	db, err := platformmysql.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := platformmysql.MigrateService(ctx, db, "promotion"); err != nil {
		t.Fatal(err)
	}
	store := New(db)
	now := time.Now().UTC()
	couponID, err := coupon.NewID()
	if err != nil {
		t.Fatal(err)
	}
	campaign := couponRecord{
		ID: couponID, Code: "RACE" + couponID[len(couponID)-5:], Title: "批量并发测试",
		Type: coupon.TypeAmount, ValueFen: 100, TotalQuantity: 3, PerUserLimit: 2,
		Status: coupon.StatusActive, ApplicableTaskTypes: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.ORM.WithContext(ctx).Create(&campaign).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupon_claims WHERE coupon_id = ?", couponID).Error
		_ = db.ORM.WithContext(ctx).Exec("DELETE FROM coupons WHERE id = ?", couponID).Error
	}()
	batches := [][]promotionapp.ClaimRequest{
		{
			{UserID: "race-a", CouponID: couponID, IdempotencyKey: "a"},
			{UserID: "race-b", CouponID: couponID, IdempotencyKey: "b"},
			{UserID: "race-c", CouponID: couponID, IdempotencyKey: "c"},
		},
		{
			{UserID: "race-d", CouponID: couponID, IdempotencyKey: "d"},
			{UserID: "race-e", CouponID: couponID, IdempotencyKey: "e"},
			{UserID: "race-f", CouponID: couponID, IdempotencyKey: "f"},
		},
	}
	results := make([][]promotionapp.ClaimResult, len(batches))
	errs := make([]error, len(batches))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := range batches {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index], errs[index] = store.ClaimCouponBatch(ctx, batches[index], now)
		}(index)
	}
	close(start)
	wg.Wait()
	for index, batchErr := range errs {
		if batchErr != nil {
			t.Fatalf("batch %d: %v", index, batchErr)
		}
	}
	successes := 0
	for _, batch := range results {
		for _, result := range batch {
			if result.Err == nil {
				successes++
			} else if !isConflict(result.Err) {
				t.Fatalf("unexpected batch error: %v", result.Err)
			}
		}
	}
	if successes != 3 {
		t.Fatalf("batch successes=%d, want exactly stock", successes)
	}
	var row couponRecord
	if err := db.ORM.WithContext(ctx).Where("id = ?", couponID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.ClaimedCount != row.TotalQuantity {
		t.Fatalf("claimed_count=%d total=%d, want no oversell", row.ClaimedCount, row.TotalQuantity)
	}
}

func isConflict(err error) bool {
	return err != nil && errors.Is(err, coupon.ErrConflict)
}
