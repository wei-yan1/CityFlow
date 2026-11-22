package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"cityflow/app/discovery/cmd/rpc/pb"
	"cityflow/app/discovery/repository"
	"cityflow/internal/common/auth"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/authctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func jsonReply(value any) (*pb.JsonReply, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not encode response")
	}
	return &pb.JsonReply{BodyJson: body}, nil
}

func decodeBody(data []byte, target any) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: request body is required", domain.ErrInvalidRequest)
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("%w: request body is too large", domain.ErrInvalidRequest)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", domain.ErrInvalidRequest, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: request body must contain a single JSON value", domain.ErrInvalidRequest)
	}
	return nil
}

func grpcError(err error) error {
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
		return status.Error(codes.Unavailable, "discovery service unavailable")
	default:
		return status.Error(codes.Internal, "discovery service failed")
	}
}

func currentClaims(ctx context.Context) (*auth.Claims, error) {
	value, ok := authctx.Claims(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	return value, nil
}

func currentUser(ctx context.Context) (string, error) {
	claims, err := currentClaims(ctx)
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

func optionalUser(ctx context.Context) string {
	claims, ok := authctx.Claims(ctx)
	if !ok {
		return ""
	}
	return claims.Subject
}

func requireAdmin(ctx context.Context) (string, error) {
	claims, err := currentClaims(ctx)
	if err != nil {
		return "", err
	}
	if claims.Role != auth.RoleAdmin {
		return "", status.Error(codes.PermissionDenied, "admin role required")
	}
	return claims.Subject, nil
}

func permissionDenied(message string) error {
	return status.Error(codes.PermissionDenied, message)
}

func pageFromProto(limit, offset int32) (repository.Page, error) {
	page := repository.Page{Limit: 50}
	if limit != 0 {
		if limit < 1 || limit > 200 {
			return page, status.Error(codes.InvalidArgument, "limit must be between 1 and 200")
		}
		page.Limit = int(limit)
	}
	if offset < 0 || offset > 1000000 {
		return page, status.Error(codes.InvalidArgument, "offset must be between 0 and 1000000")
	}
	page.Offset = int(offset)
	return page, nil
}
