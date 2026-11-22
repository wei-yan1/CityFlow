package logic

import (
	"context"
	"testing"

	"cityflow/internal/common/contract"

	"google.golang.org/grpc/metadata"
)

func TestInternalJobAuthorized(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(contract.InternalJobTokenHeader, "job-secret"))
	if !internalJobAuthorized(ctx, "job-secret") {
		t.Fatal("matching token should be accepted")
	}
	if internalJobAuthorized(ctx, "wrong-secret") {
		t.Fatal("wrong token must be rejected")
	}
	if internalJobAuthorized(context.Background(), "job-secret") {
		t.Fatal("missing metadata must be rejected")
	}
	if internalJobAuthorized(ctx, "") {
		t.Fatal("empty configured token must be rejected")
	}
}
