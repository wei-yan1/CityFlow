// Package rpckit holds the small JSON/error/claim conventions shared by the
// go-zero domain services, mirroring the Gateway's httpkit so every service
// maps domain errors to the same gRPC codes.
package rpckit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	auth "cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/common/pagination"
	"cityflow/internal/platform/authctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Claims(ctx context.Context) (*auth.Claims, error) {
	value, ok := authctx.Claims(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	return value, nil
}

func DecodeBody(data []byte, target any) error {
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

func Encode(value any) ([]byte, error) { return json.Marshal(value) }

// EndpointList parses a comma-separated service endpoint override such as
// "127.0.0.1:8094,127.0.0.1:8096". It keeps configuration files unchanged for
// the single-instance case while allowing local multi-instance runs.
func EndpointList(raw string) []string {
	parts := strings.Split(raw, ",")
	endpoints := make([]string, 0, len(parts))
	for _, part := range parts {
		if endpoint := strings.TrimSpace(part); endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

// Error maps a domain error to a gRPC status. service names the owning service
// for the generic Unavailable/Internal wording.
func Error(err error, service string) error {
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
		return status.Error(codes.Unavailable, service+" service unavailable")
	default:
		return status.Error(codes.Internal, service+" service failed")
	}
}

func Page(limit, offset int32) (pagination.Page, error) {
	page := pagination.Page{Limit: 50}
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

func RequireRole(want, actual string) error {
	if actual != want {
		return status.Error(codes.PermissionDenied, "insufficient role")
	}
	return nil
}

func RequireAnyRole(actual string, allowed ...string) error {
	for _, role := range allowed {
		if actual == role {
			return nil
		}
	}
	return status.Error(codes.PermissionDenied, "insufficient role")
}
