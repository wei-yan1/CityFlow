// Package rpcctx carries request identity over gRPC metadata.
package rpcctx

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

const idempotencyKeyHeader = "x-idempotency-key"

// WithBearer attaches the caller's access token to outgoing gRPC metadata.
// Downstream RPC servers verify it again; a Gateway header is never trusted on
// its own.
func WithBearer(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// WithIdempotencyKey 把 HTTP 的 Idempotency-Key 透传到下游 RPC。
// 只做传递，不去重；去重由服务端和唯一键完成。
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	key = strings.TrimSpace(key)
	if key == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, idempotencyKeyHeader, key)
}

// IdempotencyKey 读取上游传入的幂等键。没有则为空字符串，调用方自行决定是否降级。
func IdempotencyKey(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	values := md.Get(idempotencyKeyHeader)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
