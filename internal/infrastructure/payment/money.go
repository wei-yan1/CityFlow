package payment

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// YuanToFen parses a provider amount string ("12.34") into integer fen without
// going through float64, so rounding can never create a one-fen mismatch.
func YuanToFen(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, errors.New("amount is empty")
	}
	trimmed = strings.TrimPrefix(trimmed, "+")
	parts := strings.SplitN(trimmed, ".", 2)
	if parts[0] == "" || strings.HasPrefix(parts[0], "-") {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	yuan, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return 0, fmt.Errorf("invalid amount %q", value)
		}
		fraction = parts[1]
		for len(fraction) < 2 {
			fraction += "0"
		}
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	if yuan > (math.MaxInt64-cents)/100 {
		return 0, fmt.Errorf("amount %q is out of range", value)
	}
	return yuan*100 + cents, nil
}

// FenToYuan formats integer fen as the "0.00" string providers expect.
func FenToYuan(fen int64) string {
	if fen <= 0 {
		return "0.00"
	}
	return fmt.Sprintf("%d.%02d", fen/100, fen%100)
}
