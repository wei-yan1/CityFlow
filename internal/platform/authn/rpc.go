package authn

import (
	"context"
	"strings"
	"time"

	auth "cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/authctx"
	"cityflow/internal/security"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// FromToken is the RPC analogue of FromRequest. It verifies the bearer token
// and the account's current status before any protected RPC method runs.
type AccountActive func(context.Context, string, string) (bool, error)

// FromToken is the RPC analogue of FromRequest. It verifies the bearer token
// and then checks account ownership/status through the supplied verifier.
// Identity may check its local accounts table; Task calls Identity over zRPC.
func FromToken(ctx context.Context, token string, secret []byte, active AccountActive) (*auth.Claims, error) {
	if strings.TrimSpace(token) == "" {
		return nil, domain.ErrUnauthorized
	}
	claims, err := security.VerifyJWT(token, secret, time.Now().UTC())
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if active == nil {
		return nil, domain.ErrUnavailable
	}
	ok, err := active(ctx, claims.Subject, token)
	if err != nil {
		return nil, domain.ErrUnavailable
	}
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	return claims, nil
}

// BearerFromMetadata extracts the bearer token from incoming gRPC metadata.
func BearerFromMetadata(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", false
	}
	header := strings.TrimSpace(values[0])
	if !strings.HasPrefix(header, "Bearer ") {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	return token, token != ""
}

// OptionalUnaryServerInterceptor allows anonymous calls but verifies and
// attaches claims whenever a bearer token is present. Read-only public
// endpoints use it so an authenticated caller still gets user-specific state.
func OptionalUnaryServerInterceptor(secret []byte, active AccountActive) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		token, ok := BearerFromMetadata(ctx)
		if !ok {
			return handler(ctx, req)
		}
		claims, err := FromToken(ctx, token, secret, active)
		if err != nil {
			switch {
			case err == domain.ErrUnauthorized:
				return nil, status.Error(codes.Unauthenticated, "authentication required")
			case err == domain.ErrUnavailable:
				return nil, status.Error(codes.Unavailable, "identity service unavailable")
			default:
				return nil, status.Error(codes.Internal, "authentication failed")
			}
		}
		return handler(authctx.WithClaims(ctx, claims), req)
	}
}

// UnaryServerInterceptor enforces bearer authentication on every RPC method
// except the explicitly public methods. Protected logic reads claims from the
// standard context, never from package-level state.
func UnaryServerInterceptor(secret []byte, active AccountActive, publicMethods ...string) grpc.UnaryServerInterceptor {
	public := make(map[string]struct{}, len(publicMethods))
	for _, method := range publicMethods {
		public[method] = struct{}{}
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := public[info.FullMethod]; ok {
			return handler(ctx, req)
		}
		token, ok := BearerFromMetadata(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		claims, err := FromToken(ctx, token, secret, active)
		if err != nil {
			switch {
			case err == domain.ErrUnauthorized:
				return nil, status.Error(codes.Unauthenticated, "authentication required")
			case err == domain.ErrUnavailable:
				return nil, status.Error(codes.Unavailable, "identity service unavailable")
			default:
				return nil, status.Error(codes.Internal, "authentication failed")
			}
		}
		return handler(authctx.WithClaims(ctx, claims), req)
	}
}
