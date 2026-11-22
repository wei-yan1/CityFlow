package coupon

import "testing"

func TestRedeemCodeRoundTrip(t *testing.T) {
	const couponID = "00000000-0000-4000-8000-000000000101"
	for _, seq := range []uint64{1, 2, 42, 0xFFFFFFFF} {
		code, err := GenerateRedeemCode(seq, couponID)
		if err != nil {
			t.Fatalf("seq %d: %v", seq, err)
		}
		if len(code) != RedeemCodeLength {
			t.Fatalf("seq %d: code %q length=%d, want %d", seq, code, len(code), RedeemCodeLength)
		}
		decoded, fresh, err := DecodeRedeemCode(code)
		if err != nil {
			t.Fatalf("seq %d: decode %q: %v", seq, code, err)
		}
		if decoded != seq {
			t.Fatalf("seq %d: decoded %d", seq, decoded)
		}
		if fresh != CouponFresh(couponID) {
			t.Fatalf("seq %d: fresh %d, want %d", seq, fresh, CouponFresh(couponID))
		}
	}
}

func TestRedeemCodeDependsOnCouponFresh(t *testing.T) {
	first, err := GenerateRedeemCode(7, "coupon-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateRedeemCode(7, "coupon-b")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("codes from different coupons must differ: %q", first)
	}
}

func TestNormalizeRedeemCodeFoldsAmbiguousCharacters(t *testing.T) {
	if got := NormalizeRedeemCode(" abcd-efgh "); got != "ABCDEFGH" {
		t.Fatalf("normalized=%q", got)
	}
	if got := NormalizeRedeemCode("OIL0"); got != "0110" {
		t.Fatalf("ambiguous normalization=%q", got)
	}
}

func TestDecodeRedeemCodeRejectsTamperedChecksum(t *testing.T) {
	code, err := GenerateRedeemCode(99, "coupon-a")
	if err != nil {
		t.Fatal(err)
	}
	tampered := code[:len(code)-1] + string(redeemCodeAlphabet[(indexOf(code[len(code)-1])+1)%len(redeemCodeAlphabet)])
	if _, _, err := DecodeRedeemCode(tampered); err == nil {
		t.Fatalf("expected tampered code %q to fail checksum", tampered)
	}
}

func indexOf(char byte) int {
	for index := 0; index < len(redeemCodeAlphabet); index++ {
		if redeemCodeAlphabet[index] == char {
			return index
		}
	}
	return 0
}
