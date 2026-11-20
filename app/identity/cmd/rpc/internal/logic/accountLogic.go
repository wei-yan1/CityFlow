package logic

import (
	"context"
	"encoding/json"

	"cityflow/app/identity/cmd/rpc/internal/svc"
	"cityflow/app/identity/cmd/rpc/pb"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/user"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"
	"cityflow/internal/platform/rpckit"

	"github.com/zeromicro/go-zero/core/logx"
)

type baseLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func newBase(ctx context.Context, svcCtx *svc.ServiceContext) baseLogic {
	return baseLogic{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx)}
}

func (b baseLogic) admin() (*auth.Claims, error) {
	claims, err := rpckit.Claims(b.ctx)
	if err != nil {
		return nil, err
	}
	if err := rpckit.RequireRole(auth.RoleAdmin, claims.Role); err != nil {
		return nil, err
	}
	return claims, nil
}

type RegisterLogic struct{ baseLogic }

func NewRegisterLogic(ctx context.Context, s *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{newBase(ctx, s)}
}
func (l *RegisterLogic) Register(in *identity.JsonBody) (*identity.JsonReply, error) {
	if l.svcCtx.Users == nil {
		return nil, mapError(domain.ErrUnavailable, "")
	}
	var req user.RegisterRequest
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Users.Register(l.ctx, req)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type RequestPasswordResetLogic struct{ baseLogic }

func NewRequestPasswordResetLogic(ctx context.Context, s *svc.ServiceContext) *RequestPasswordResetLogic {
	return &RequestPasswordResetLogic{newBase(ctx, s)}
}
func (l *RequestPasswordResetLogic) RequestPasswordReset(in *identity.JsonBody) (*identity.JsonReply, error) {
	var req struct {
		Email string `json:"email"`
	}
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Users.RequestPasswordReset(l.ctx, req.Email)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type ResetPasswordLogic struct{ baseLogic }

func NewResetPasswordLogic(ctx context.Context, s *svc.ServiceContext) *ResetPasswordLogic {
	return &ResetPasswordLogic{newBase(ctx, s)}
}
func (l *ResetPasswordLogic) ResetPassword(in *identity.JsonBody) (*identity.JsonReply, error) {
	var req user.PasswordResetConfirm
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	if err := l.svcCtx.Users.ResetPassword(l.ctx, req); err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(map[string]bool{"ok": true})
}

type UpdateMeLogic struct{ baseLogic }

func NewUpdateMeLogic(ctx context.Context, s *svc.ServiceContext) *UpdateMeLogic {
	return &UpdateMeLogic{newBase(ctx, s)}
}
func (l *UpdateMeLogic) UpdateMe(in *identity.JsonBody) (*identity.JsonReply, error) {
	claims, err := rpckit.Claims(l.ctx)
	if err != nil {
		return nil, err
	}
	var req user.UpdateRequest
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Users.UpdateOwnProfile(l.ctx, claims.Subject, req)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type ListUsersLogic struct{ baseLogic }

func NewListUsersLogic(ctx context.Context, s *svc.ServiceContext) *ListUsersLogic {
	return &ListUsersLogic{newBase(ctx, s)}
}
func (l *ListUsersLogic) ListUsers(in *identity.ListQuery) (*identity.JsonReply, error) {
	if _, err := l.admin(); err != nil {
		return nil, err
	}
	page, err := rpckit.Page(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Users.ListAccounts(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(map[string]any{"data": out, "limit": page.Limit, "offset": page.Offset})
}

type CreateUserLogic struct{ baseLogic }

func NewCreateUserLogic(ctx context.Context, s *svc.ServiceContext) *CreateUserLogic {
	return &CreateUserLogic{newBase(ctx, s)}
}
func (l *CreateUserLogic) CreateUser(in *identity.JsonBody) (*identity.JsonReply, error) {
	if _, err := l.admin(); err != nil {
		return nil, err
	}
	var req user.CreateRequest
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Users.CreateAccount(l.ctx, req)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type GetUserAdminLogic struct{ baseLogic }

func NewGetUserAdminLogic(ctx context.Context, s *svc.ServiceContext) *GetUserAdminLogic {
	return &GetUserAdminLogic{newBase(ctx, s)}
}
func (l *GetUserAdminLogic) GetUserAdmin(in *identity.IdRequest) (*identity.JsonReply, error) {
	if _, err := l.admin(); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Accounts.GetUser(l.ctx, in.GetId())
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type UpdateUserLogic struct{ baseLogic }

func NewUpdateUserLogic(ctx context.Context, s *svc.ServiceContext) *UpdateUserLogic {
	return &UpdateUserLogic{newBase(ctx, s)}
}
func (l *UpdateUserLogic) UpdateUser(in *identity.IdBody) (*identity.JsonReply, error) {
	if _, err := l.admin(); err != nil {
		return nil, err
	}
	var req user.UpdateRequest
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Users.UpdateAccount(l.ctx, in.GetId(), req)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type DisableUserLogic struct{ baseLogic }

func NewDisableUserLogic(ctx context.Context, s *svc.ServiceContext) *DisableUserLogic {
	return &DisableUserLogic{newBase(ctx, s)}
}
func (l *DisableUserLogic) DisableUser(in *identity.IdRequest) (*identity.JsonReply, error) {
	if _, err := l.admin(); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Users.DisableAccount(l.ctx, in.GetId())
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type SubmitRiderRegistrationLogic struct{ baseLogic }

func NewSubmitRiderRegistrationLogic(ctx context.Context, s *svc.ServiceContext) *SubmitRiderRegistrationLogic {
	return &SubmitRiderRegistrationLogic{newBase(ctx, s)}
}
func (l *SubmitRiderRegistrationLogic) SubmitRiderRegistration(in *identity.JsonBody) (*identity.JsonReply, error) {
	if l.svcCtx.Riders == nil {
		return nil, mapError(domain.ErrUnavailable, "")
	}
	var req rider.RegistrationRequest
	if err := rpckit.DecodeBody(in.GetBodyJson(), &req); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Riders.Submit(l.ctx, req)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type ListRiderRegistrationsLogic struct{ baseLogic }

func NewListRiderRegistrationsLogic(ctx context.Context, s *svc.ServiceContext) *ListRiderRegistrationsLogic {
	return &ListRiderRegistrationsLogic{newBase(ctx, s)}
}
func (l *ListRiderRegistrationsLogic) ListRiderRegistrations(in *identity.ListQuery) (*identity.JsonReply, error) {
	if _, err := l.admin(); err != nil {
		return nil, err
	}
	page, err := rpckit.Page(in.GetLimit(), in.GetOffset())
	if err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Riders.List(l.ctx, in.GetStatus(), page)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(map[string]any{"data": out, "limit": page.Limit, "offset": page.Offset})
}

// sagaStore is the Identity-owned half of rider approval; the rider performance
// profile is provisioned in Task over RPC.
type sagaStore interface {
	ApproveRiderRegistrationAccount(ctx context.Context, id, adminID, note string) (*rider.Registration, error)
	RevertRiderRegistration(ctx context.Context, id, userID string) error
}

type ApproveRiderRegistrationLogic struct{ baseLogic }

func NewApproveRiderRegistrationLogic(ctx context.Context, s *svc.ServiceContext) *ApproveRiderRegistrationLogic {
	return &ApproveRiderRegistrationLogic{newBase(ctx, s)}
}
func (l *ApproveRiderRegistrationLogic) ApproveRiderRegistration(in *identity.IdBody) (*identity.JsonReply, error) {
	claims, err := l.admin()
	if err != nil {
		return nil, err
	}
	var body struct {
		Note string `json:"note"`
	}
	if len(in.GetBodyJson()) > 0 {
		if err := json.Unmarshal(in.GetBodyJson(), &body); err != nil {
			return nil, mapError(err, "")
		}
	}
	store, ok := l.svcCtx.Accounts.(sagaStore)
	if !ok {
		return nil, mapError(domain.ErrUnavailable, "")
	}
	if !l.svcCtx.EventsEnabled && l.svcCtx.Task == nil {
		return nil, mapError(domain.ErrUnavailable, "")
	}
	reg, err := store.ApproveRiderRegistrationAccount(l.ctx, in.GetId(), claims.Subject, body.Note)
	if err != nil {
		return nil, mapError(err, "")
	}
	// With the outbox enabled, approval is already durable and Task provisions
	// the rider profile after consuming rider.approved.v1. The synchronous RPC
	// remains only as a fallback for local deployments without a broker.
	if l.svcCtx.EventsEnabled {
		return jsonReply(reg)
	}
	profile := rider.UpdateRequest{
		DisplayName: reg.DisplayName, VehicleType: reg.VehicleType, Skills: reg.Skills,
		ServiceArea: reg.ServiceArea, MaxDistanceMeters: reg.MaxDistanceMeters, MaxConcurrentTasks: reg.MaxConcurrentTasks,
	}
	data, _ := json.Marshal(profile)
	taskCtx := l.ctx
	if token, ok := authn.BearerFromMetadata(l.ctx); ok {
		taskCtx = rpcctx.WithBearer(l.ctx, token)
	}
	if _, err := l.svcCtx.Task.ProvisionRider(taskCtx, &taskpb.IdBody{Id: reg.UserID, BodyJson: data}); err != nil {
		_ = store.RevertRiderRegistration(l.ctx, reg.ID, reg.UserID)
		// ProvisionRider may have inserted the Task profile before failing (for
		// example while loading it back). Archive it best-effort so compensation
		// does not leave an orphaned profile pointing at a deleted account.
		_, _ = l.svcCtx.Task.ArchiveRider(taskCtx, &taskpb.IdRequest{Id: reg.UserID})
		if l.svcCtx.Users != nil {
			_, _ = l.svcCtx.Users.DisableAccount(l.ctx, reg.UserID)
		}
		return nil, mapError(err, "")
	}
	return jsonReply(reg)
}

type RejectRiderRegistrationLogic struct{ baseLogic }

func NewRejectRiderRegistrationLogic(ctx context.Context, s *svc.ServiceContext) *RejectRiderRegistrationLogic {
	return &RejectRiderRegistrationLogic{newBase(ctx, s)}
}
func (l *RejectRiderRegistrationLogic) RejectRiderRegistration(in *identity.IdBody) (*identity.JsonReply, error) {
	claims, err := l.admin()
	if err != nil {
		return nil, err
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := rpckit.DecodeBody(in.GetBodyJson(), &body); err != nil {
		return nil, mapError(err, "")
	}
	out, err := l.svcCtx.Riders.Reject(l.ctx, in.GetId(), claims.Subject, body.Note)
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}

type GetUserInternalLogic struct{ baseLogic }

func NewGetUserInternalLogic(ctx context.Context, s *svc.ServiceContext) *GetUserInternalLogic {
	return &GetUserInternalLogic{newBase(ctx, s)}
}
func (l *GetUserInternalLogic) GetUserInternal(in *identity.IdRequest) (*identity.JsonReply, error) {
	if _, err := rpckit.Claims(l.ctx); err != nil {
		return nil, err
	}
	out, err := l.svcCtx.Accounts.GetUser(l.ctx, in.GetId())
	if err != nil {
		return nil, mapError(err, "")
	}
	return jsonReply(out)
}
