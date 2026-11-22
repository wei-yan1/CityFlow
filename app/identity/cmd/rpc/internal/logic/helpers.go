package logic

import (
	"encoding/json"
	"errors"
	"time"

	"cityflow/app/identity/domain/user"
	"cityflow/internal/common/domain"

	"cityflow/app/identity/cmd/rpc/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func mapError(err error, unauthorizedMessage string) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, unauthorizedMessage)
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, "operation is not allowed")
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "resource not found")
	case errors.Is(err, domain.ErrConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, "identity service unavailable")
	default:
		return status.Error(codes.Internal, "identity service failed")
	}
}

func userMessage(u *user.User) *identity.User {
	if u == nil {
		return nil
	}
	return &identity.User{
		Id:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		Phone:     u.Phone,
		AvatarUrl: u.AvatarURL,
		Role:      u.Role,
		Status:    u.Status,
		CreatedAt: formatTime(u.CreatedAt),
		UpdatedAt: formatTime(u.UpdatedAt),
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func jsonReply(value any) (*identity.JsonReply, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, status.Error(codes.Internal, "identity service failed")
	}
	return &identity.JsonReply{BodyJson: data}, nil
}
