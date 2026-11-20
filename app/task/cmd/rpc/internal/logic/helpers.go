package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/platform/authctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	errInvalidKind = fmt.Errorf("%w: unsupported feedback kind", domain.ErrInvalidRequest)
	errUnavailable = domain.ErrUnavailable
)

func claims(ctx context.Context) (*auth.Claims, error) {
	value, ok := authctx.Claims(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	return value, nil
}

func decodeBody(data []byte, target any) error {
	if len(data) == 0 {
		return errors.New("request body is required")
	}
	if len(data) > 1<<20 {
		return errors.New("request body is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON: " + err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}

func encodeBody(value any) ([]byte, error) {
	return json.Marshal(value)
}

func grpcTaskError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, "authentication required")
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, "operation is not allowed")
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, domain.ErrConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, "task service unavailable")
	default:
		return status.Error(codes.Internal, "task service failed")
	}
}

func pageFromProto(limit, offset int64) (repository.Page, error) {
	page := repository.Page{Limit: 50}
	if limit != 0 {
		if limit < 1 || limit > 200 {
			return page, errors.New("limit must be between 1 and 200")
		}
		page.Limit = int(limit)
	}
	if offset < 0 || offset > 1000000 {
		return page, errors.New("offset must be between 0 and 1000000")
	}
	page.Offset = int(offset)
	return page, nil
}

func requireRole(want string, actual string) error {
	if actual != want {
		return status.Error(codes.PermissionDenied, "insufficient role")
	}
	return nil
}

func requireAnyRole(actual string, allowed ...string) error {
	for _, role := range allowed {
		if actual == role {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "insufficient role")
}
