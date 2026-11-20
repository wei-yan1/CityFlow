// Package logic holds Gateway-side response assembly that is independent of
// the HTTP transport. Endpoint-specific orchestration stays in handler until
// the .api contract is regenerated with goctl.
package logic

import (
	"cityflow/app/gateway/cmd/api/internal/types"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
)

// AuthResponse converts the Identity zRPC reply into the public REST shape.
func AuthResponse(response *identityclient.AuthReply) types.AuthResponse {
	return types.AuthResponse{
		AccessToken: response.GetAccessToken(),
		TokenType:   response.GetTokenType(),
		ExpiresAt:   response.GetExpiresAt(),
		User:        User(response.GetUser()),
	}
}

// User converts an Identity zRPC user into the public REST user shape.
func User(value *identityclient.User) *types.User {
	if value == nil {
		return nil
	}
	return &types.User{
		ID: value.GetId(), Email: value.GetEmail(), Name: value.GetName(),
		Phone: value.GetPhone(), AvatarURL: value.GetAvatarUrl(), Role: value.GetRole(),
		Status: value.GetStatus(), CreatedAt: value.GetCreatedAt(), UpdatedAt: value.GetUpdatedAt(),
	}
}
