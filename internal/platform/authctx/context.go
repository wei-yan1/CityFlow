// Package authctx keeps the authenticated caller attached to one request's
// context. It intentionally uses context.Context rather than ThreadLocal-style
// global state: each HTTP request and each consumer message owns its own scope.
package authctx

import (
	"context"

	auth "cityflow/internal/common/auth"
)

type claimsKey struct{}

// WithClaims returns a child context carrying the verified caller identity.
func WithClaims(ctx context.Context, claims *auth.Claims) context.Context {
	if claims == nil {
		return ctx
	}
	return context.WithValue(ctx, claimsKey{}, claims)
}

// Claims returns the verified caller attached by the transport middleware.
func Claims(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*auth.Claims)
	return claims, ok && claims != nil
}
