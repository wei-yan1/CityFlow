package authn

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestPaymentWorkerAuthScope(t *testing.T) {
	secret := []byte("test-secret")
	const method = "/task.Task/ListTasksInternal"
	interceptor := PaymentWorkerAuth(secret, method, UnaryServerInterceptor(secret, nil))
	for _, tc := range []struct {
		name, method, token string
		want                codes.Code
	}{
		{"worker read", method, PaymentWorkerToken(secret), codes.OK},
		{"worker write", "/task.Task/Claim", PaymentWorkerToken(secret), codes.Unauthenticated},
		{"wrong credential", method, PaymentWorkerToken([]byte("wrong")), codes.Unauthenticated},
		{"missing credential", method, "", codes.Unauthenticated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+tc.token))
			_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: tc.method}, func(ctx context.Context, _ any) (any, error) {
				if !IsPaymentWorker(ctx) {
					t.Error("worker identity was not propagated")
				}
				return nil, nil
			})
			if got := status.Code(err); got != tc.want {
				t.Fatalf("code = %s; want %s", got, tc.want)
			}
		})
	}
}
