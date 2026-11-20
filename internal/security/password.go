package security

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

const passwordRounds = 210_000

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2_sha256$" + base64.RawURLEncoding.EncodeToString(salt) + "$" + base64.RawURLEncoding.EncodeToString(derived), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	salt, err1 := base64.RawURLEncoding.DecodeString(parts[1])
	expected, err2 := base64.RawURLEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordRounds, len(expected))
	return err == nil && len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
}
