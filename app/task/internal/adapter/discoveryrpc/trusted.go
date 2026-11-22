// Package discoveryrpc adapts Discovery's trusted-place RPC to Task's local
// resolver port. It lives in Task because only Task consumes this read model.
package discoveryrpc

import (
	"context"
	"encoding/json"
	"fmt"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	discoverypb "cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/location"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Trusted struct{ Client discoveryclient.Discovery }

func (a Trusted) Resolve(ctx context.Context, ids []string) ([]location.Place, error) {
	if a.Client == nil {
		return nil, domain.ErrUnavailable
	}
	body, _ := json.Marshal(map[string]any{"ids": ids})
	reply, err := a.Client.ResolveTrustedPlacesInternal(forward(ctx), &discoverypb.JsonBody{BodyJson: body})
	if err != nil {
		return nil, toDomain(err)
	}
	var out []location.Place
	if err := json.Unmarshal(reply.GetBodyJson(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func forward(ctx context.Context) context.Context {
	if token, ok := authn.BearerFromMetadata(ctx); ok {
		return rpcctx.WithBearer(ctx, token)
	}
	return ctx
}

func toDomain(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.Unauthenticated:
		return domain.ErrUnauthorized
	case codes.PermissionDenied:
		return domain.ErrForbidden
	case codes.NotFound:
		return domain.ErrNotFound
	case codes.FailedPrecondition, codes.AlreadyExists, codes.Aborted:
		return fmt.Errorf("%w: %s", domain.ErrConflict, status.Convert(err).Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", domain.ErrInvalidRequest, status.Convert(err).Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		return domain.ErrUnavailable
	default:
		return fmt.Errorf("upstream service failed: %w", err)
	}
}
