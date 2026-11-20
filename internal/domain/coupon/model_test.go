package coupon

import (
	"testing"
	"time"
)

func TestDiscountForFixedAmountRespectsMinimumAndLeavesOneFen(t *testing.T) {
	item := Coupon{Type: TypeAmount, ValueFen: 500, MinSpendFen: 1000}
	if _, err := item.DiscountFor(900); err == nil {
		t.Fatalf("expected minimum-spend rejection")
	}
	discount, err := item.DiscountFor(1000)
	if err != nil || discount != 500 {
		t.Fatalf("discount=%d err=%v, want 500", discount, err)
	}
	small := Coupon{Type: TypeAmount, ValueFen: 500}
	discount, err = small.DiscountFor(300)
	if err != nil || discount != 299 {
		t.Fatalf("discount=%d err=%v, want 299 so the payer still owes one fen", discount, err)
	}
}

func TestDiscountForPercentCapsAtMaximum(t *testing.T) {
	item := Coupon{Type: TypePercent, DiscountPercent: 20, MaxDiscountFen: 800}
	discount, err := item.DiscountFor(2000)
	if err != nil || discount != 400 {
		t.Fatalf("discount=%d err=%v, want 400", discount, err)
	}
	discount, err = item.DiscountFor(10000)
	if err != nil || discount != 800 {
		t.Fatalf("discount=%d err=%v, want cap 800", discount, err)
	}
}

func TestQuoteRejectsInactiveAndWrongTaskType(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	item := Coupon{
		ID: "c1", Type: TypeAmount, ValueFen: 300, MinSpendFen: 800,
		Status: StatusActive, StartsAt: &start, EndsAt: &end,
		ApplicableTaskTypes: []string{"DELIVERY"},
		TotalQuantity:       10, Remaining: 10,
	}
	if _, err := QuoteFor(&item, nil, 1000, "PICKUP", start.Add(time.Hour)); err == nil {
		t.Fatalf("expected task-type rejection")
	}
	if _, err := QuoteFor(&item, nil, 1000, "DELIVERY", end); err == nil {
		t.Fatalf("expected expiry rejection")
	}
	quote, err := QuoteFor(&item, nil, 1000, "DELIVERY", start.Add(time.Hour))
	if err != nil || quote.DiscountFen != 300 || quote.PayableAmountFen != 700 {
		t.Fatalf("quote=%+v err=%v", quote, err)
	}
}
