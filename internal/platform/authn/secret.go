package authn

import (
	"log"
	"os"
	"strings"
)

// ResolveSecret applies the deployment contract shared by every process:
// explicit config first, then CITYFLOW_JWT_SECRET from the process environment
// (loaded from the git-ignored .env at startup). There is deliberately no
// compiled-in fallback: a signing key that ships with the source code is a
// public key, and every token it mints can be forged by anyone who reads the
// repository. Missing configuration must fail loudly at startup instead.
func ResolveSecret(configured string) []byte {
	secret := strings.TrimSpace(configured)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("CITYFLOW_JWT_SECRET"))
	}
	if secret == "" {
		log.Fatalf("JWT secret is not configured: set CITYFLOW_JWT_SECRET in .env")
	}
	return []byte(secret)
}
