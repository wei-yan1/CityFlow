package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"cityflow/internal/common/auth"
)

// SignJWT signs the stable identity facts. Callers pass primitives so the
// shared security package does not import any service's domain model.
func SignJWT(subject, email, role string, secret []byte, now time.Time) (string, time.Time, error) {
	return SignClaims(auth.Claims{Subject: subject, Email: email, Role: role}, secret, now)
}

// SignClaims renews a verified identity; callers must verify the old token and
// account status first. Tokens are never renewed after expiry.
func SignClaims(claims auth.Claims, secret []byte, now time.Time) (string, time.Time, error) {
	expires := now.Add(auth.AccessTokenLifetime)
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", time.Time{}, err
	}
	claims.Issuer = "cityflow"
	claims.IssuedAt = now.Unix()
	claims.Expires = expires.Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	encode := base64.RawURLEncoding.EncodeToString
	unsigned := encode(header) + "." + encode(payload)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + encode(mac.Sum(nil)), expires, nil
}

func VerifyJWT(token string, secret []byte, now time.Time) (*auth.Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid token header")
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if json.Unmarshal(headerJSON, &header) != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return nil, errors.New("unsupported token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("invalid token signature")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token payload")
	}
	var claims auth.Claims
	if json.Unmarshal(payload, &claims) != nil || claims.Issuer != "cityflow" || claims.Subject == "" || claims.Role == "" || claims.Expires <= now.Unix() || claims.IssuedAt <= 0 || claims.Expires-claims.IssuedAt > int64(auth.AccessTokenLifetime.Seconds()) {
		return nil, errors.New("expired or invalid token")
	}
	return &claims, nil
}
