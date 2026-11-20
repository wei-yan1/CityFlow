package payment

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// ParseRSAPrivateKey accepts the PKCS#8 or PKCS#1 PEM that both Alipay and
// WeChat merchant platforms export.
func ParseRSAPrivateKey(value string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rsaKey, ok := key.(*rsa.PrivateKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("private key is not RSA")
	}
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA private key: %w", err)
	}
	return rsaKey, nil
}

// ParseRSAPublicKey accepts a PKIX public key, a PKCS#1 public key, or an
// X.509 certificate body (Alipay sometimes publishes the key as a certificate).
func ParseRSAPublicKey(value string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(value)))
	if block == nil {
		return nil, errors.New("public key is not PEM encoded")
	}
	if parsed, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		if rsaKey, ok := parsed.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("public key is not RSA")
	}
	if certificate, err := x509.ParseCertificate(block.Bytes); err == nil {
		if rsaKey, ok := certificate.PublicKey.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("certificate does not carry an RSA key")
	}
	rsaKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA public key: %w", err)
	}
	return rsaKey, nil
}

// SignSHA256 returns the standard SHA256withRSA signature both providers use,
// already base64 encoded.
func SignSHA256(key *rsa.PrivateKey, content string) (string, error) {
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func VerifySHA256(key *rsa.PublicKey, content, signature string) error {
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("signature is not base64: %w", err)
	}
	digest := sha256.Sum256([]byte(content))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], decoded); err != nil {
		return fmt.Errorf("signature mismatch: %w", err)
	}
	return nil
}
