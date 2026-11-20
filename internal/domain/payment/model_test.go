package payment

import (
	"strings"
	"testing"
)

func TestOrderTransitionsAreClosedByDefault(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{PaymentPending, PaymentPaid, true},
		{PaymentPending, PaymentFailed, true},
		{PaymentPending, PaymentClosed, true},
		{PaymentPending, PaymentRefundPending, false},
		{PaymentPaid, PaymentRefundPending, true},
		{PaymentPaid, PaymentClosed, false},
		{PaymentPaid, PaymentRefunded, true},
		{PaymentRefundPending, PaymentRefunded, true},
		{PaymentRefundPending, PaymentRefundFailed, true},
		{PaymentRefundFailed, PaymentRefundPending, true},
		{PaymentRefundFailed, PaymentRefunded, true},
		{PaymentRefunded, PaymentPaid, false},
		{PaymentFailed, PaymentPaid, false},
	}
	for _, item := range cases {
		if got := CanTransitionOrder(item.from, item.to); got != item.want {
			t.Errorf("CanTransitionOrder(%s, %s) = %v, want %v", item.from, item.to, got, item.want)
		}
	}
}

func TestSettlementTransitionsAllowRetryOnly(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{SettlementPending, SettlementProcessing, true},
		{SettlementProcessing, SettlementPaid, true},
		{SettlementProcessing, SettlementPending, true},
		{SettlementProcessing, SettlementUnknown, true},
		{SettlementPending, SettlementUnknown, false},
		{SettlementUnknown, SettlementPaid, false},
		{SettlementUnknown, SettlementPending, false},
		{SettlementFailed, SettlementPending, true},
		{SettlementFailed, SettlementPaid, false},
		{SettlementPaid, SettlementPending, false},
	}
	for _, item := range cases {
		if got := CanTransitionSettlement(item.from, item.to); got != item.want {
			t.Errorf("CanTransitionSettlement(%s, %s) = %v, want %v", item.from, item.to, got, item.want)
		}
	}
}

func TestSplitAmount(t *testing.T) {
	cases := []struct {
		amount, feeBPS, payout, fee int64
	}{
		{1000, 0, 1000, 0},
		{1000, 1000, 900, 100},
		{999, 500, 950, 49},
		{1000, 10000, 0, 1000},
		{0, 1200, 0, 0},
	}
	for _, item := range cases {
		payout, fee := SplitAmount(item.amount, item.feeBPS)
		if payout != item.payout || fee != item.fee {
			t.Errorf("SplitAmount(%d, %d) = (%d, %d), want (%d, %d)", item.amount, item.feeBPS, payout, fee, item.payout, item.fee)
		}
	}
}

func TestNewOrderNoFitsProviderLimit(t *testing.T) {
	number, err := NewOrderNo("CF")
	if err != nil {
		t.Fatal(err)
	}
	if len(number) > 32 || !strings.HasPrefix(number, "CF") {
		t.Fatalf("order number %q does not fit out_trade_no limits", number)
	}
	another, err := NewOrderNo("CF")
	if err != nil {
		t.Fatal(err)
	}
	if number == another {
		t.Fatal("order numbers must be unique")
	}
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 36 || strings.Count(id, "-") != 4 {
		t.Fatalf("uuid %q is not canonical", id)
	}
}
