package payment

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
)

// Crockford base32 avoids characters providers reject, and 15 random bytes
// produce 24 characters. With a two-character prefix an order number stays
// inside WeChat Pay's 32-character out_trade_no limit.
var orderAlphabet = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

func randomBytes(length int) ([]byte, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

// NewID returns a random RFC 4122 version 4 UUID used for database keys.
func NewID() (string, error) {
	buffer, err := randomBytes(16)
	if err != nil {
		return "", err
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16]), nil
}

// NewOrderNo returns a provider-safe number: prefix + 24 base32 characters.
func NewOrderNo(prefix string) (string, error) {
	buffer, err := randomBytes(15)
	if err != nil {
		return "", err
	}
	return prefix + orderAlphabet.EncodeToString(buffer), nil
}
