package authn

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"

	"google.golang.org/grpc"
)

type paymentWorkerKey struct{}

func PaymentWorkerToken(secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("cityflow/payment-worker/task-list/v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

func IsPaymentWorker(ctx context.Context) bool {
	allowed, _ := ctx.Value(paymentWorkerKey{}).(bool)
	return allowed
}

// PaymentWorkerAuth preserves normal user authentication on all other calls.
func PaymentWorkerAuth(secret []byte, method string, next grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	expected := []byte(PaymentWorkerToken(secret))
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if info.FullMethod == method {
			if token, ok := BearerFromMetadata(ctx); ok && hmac.Equal([]byte(token), expected) {
				return handler(context.WithValue(ctx, paymentWorkerKey{}, true), req)
			}
		}
		return next(ctx, req, info, handler)
	}
}
