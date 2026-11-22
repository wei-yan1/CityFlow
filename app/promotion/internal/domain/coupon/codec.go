package coupon

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
)

const (
	// RedeemCodeLength is the fixed 10-character Crockford Base32 form used by
	// coupon redeem codes. The 50-bit payload is 14 check bits + 4 fresh bits +
	// 32 serial bits.
	RedeemCodeLength = 10
	redeemCodeBits   = 50
)

const redeemCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var redeemCodePrimes = [10]uint64{23, 59, 241, 61, 607, 67, 977, 1217, 1289, 1601}

var (
	// ErrInvalidRedeemCode is returned when a code cannot be decoded or its
	// checksum does not match.
	ErrInvalidRedeemCode = errors.New("invalid redeem code")
)

// CouponFresh derives the four-bit fresh value from the coupon id. The code
// table still owns the authoritative coupon relation; fresh only makes codes
// from the same coupon share a visible prefix-like marker.
func CouponFresh(couponID string) uint64 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.TrimSpace(couponID)))
	return uint64(hash.Sum32() & 0xF)
}

// GenerateRedeemCode builds one code from a database-allocated sequence. The
// sequence is the only source of uniqueness; callers must persist it before
// exposing the code.
func GenerateRedeemCode(seq uint64, couponID string) (string, error) {
	if seq == 0 || seq > 0xFFFFFFFF {
		return "", fmt.Errorf("%w: sequence out of range", ErrInvalidRedeemCode)
	}
	fresh := CouponFresh(couponID)
	payload := fresh<<32 | seq
	check := redeemCheckCode(payload, fresh)
	return encodeRedeemPayload(check<<36 | payload), nil
}

// DecodeRedeemCode is used by tests and diagnostics. Redemption itself looks
// the code up in the database so a decoded sequence can never bypass the
// coupon/claim checks.
func DecodeRedeemCode(code string) (seq uint64, fresh uint64, err error) {
	code = NormalizeRedeemCode(code)
	if len(code) != RedeemCodeLength {
		return 0, 0, ErrInvalidRedeemCode
	}
	value, err := decodeRedeemPayload(code)
	if err != nil {
		return 0, 0, err
	}
	check := value >> 36
	payload := value & 0xFFFFFFFFF
	fresh = (payload >> 32) & 0xF
	if redeemCheckCode(payload, fresh) != check {
		return 0, 0, ErrInvalidRedeemCode
	}
	return payload & 0xFFFFFFFF, fresh, nil
}

// NormalizeRedeemCode uppercases and removes separators. Crockford's O/I/L
// aliases are folded into 0/1 so a user typing a printed code by hand has a
// reasonable chance of success without making ambiguous characters canonical.
func NormalizeRedeemCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var normalized strings.Builder
	for _, char := range value {
		switch char {
		case '-', ' ', '_':
			continue
		case 'O':
			char = '0'
		case 'I', 'L':
			char = '1'
		}
		normalized.WriteRune(char)
	}
	return normalized.String()
}

func redeemCheckCode(payload, fresh uint64) uint64 {
	var sum uint64
	for index := 0; payload > 0; index++ {
		sum += (payload & 0xF) * redeemCodePrimes[(index+int(fresh))%len(redeemCodePrimes)]
		payload >>= 4
	}
	return sum & 0x3FFF
}

func encodeRedeemPayload(value uint64) string {
	var builder strings.Builder
	builder.Grow(RedeemCodeLength)
	for shift := redeemCodeBits - 5; shift >= 0; shift -= 5 {
		builder.WriteByte(redeemCodeAlphabet[(value>>uint(shift))&0x1F])
	}
	return builder.String()
}

func decodeRedeemPayload(code string) (uint64, error) {
	var value uint64
	for _, char := range code {
		index := strings.IndexRune(redeemCodeAlphabet, char)
		if index < 0 {
			return 0, ErrInvalidRedeemCode
		}
		value = value<<5 | uint64(index)
	}
	return value, nil
}
