// Package rpcctx carries request identity over gRPC metadata.
package rpcctx

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// WithBearer attaches the caller's access token to outgoing gRPC metadata.
// Downstream RPC servers verify it again; a Gateway header is never trusted on
// its own.
func WithBearer(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}
