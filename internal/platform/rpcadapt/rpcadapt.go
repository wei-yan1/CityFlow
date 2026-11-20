// Package rpcadapt implements the narrow read ports of the application
// services with zRPC calls to the owning service. A domain service reads a
// neighbour's facts through these adapters instead of touching its tables.
package rpcadapt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	discoverypb "cityflow/app/discovery/cmd/rpc/pb"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	identitypb "cityflow/app/identity/cmd/rpc/pb"
	paymentclient "cityflow/app/payment/cmd/rpc/paymentclient"
	paymentpb "cityflow/app/payment/cmd/rpc/pb"
	taskpb "cityflow/app/task/cmd/rpc/pb"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	trackingpb "cityflow/app/tracking/cmd/rpc/pb"
	trackingclient "cityflow/app/tracking/cmd/rpc/trackingclient"
	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/domain/payment"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

func decode(data []byte, target any) error {
	if len(data) == 0 {
		return domain.ErrNotFound
	}
	return json.Unmarshal(data, target)
}

// Accounts reads identity facts.
type Accounts struct{ Client identityclient.Identity }

func (a Accounts) GetUser(ctx context.Context, id string) (*user.User, error) {
	reply, err := a.Client.GetUserInternal(forward(ctx), &identitypb.IdRequest{Id: id})
	if err != nil {
		return nil, toDomain(err)
	}
	var out user.User
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a Accounts) IsActive(ctx context.Context, id string) (bool, error) {
	u, err := a.GetUser(ctx, id)
	if err != nil {
		return false, err
	}
	return u.Status == "ACTIVE", nil
}

// Tasks reads task facts.
type Tasks struct{ Client taskclient.Task }

func (a Tasks) Get(ctx context.Context, id string) (*task.Task, error) {
	reply, err := a.Client.GetTaskInternal(forward(ctx), &taskpb.IdRequest{Id: id})
	if err != nil {
		return nil, toDomain(err)
	}
	var out task.Task
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a Tasks) List(ctx context.Context, statusFilter string, page repository.Page) ([]*task.Task, error) {
	reply, err := a.Client.ListTasksInternal(forward(ctx), &taskpb.ListQuery{Status: statusFilter, Limit: int32(page.Limit), Offset: int32(page.Offset)})
	if err != nil {
		return nil, toDomain(err)
	}
	var out []*task.Task
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Riders reads rider performance profiles owned by Task.
type Riders struct{ Client taskclient.Task }

func (a Riders) RiderForUser(ctx context.Context, userID string) (*rider.Profile, error) {
	reply, err := a.Client.GetRiderByUser(forward(ctx), &taskpb.IdRequest{Id: userID})
	if err != nil {
		return nil, toDomain(err)
	}
	var out rider.Profile
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a Riders) GetRider(ctx context.Context, id string) (*rider.Profile, error) {
	reply, err := a.Client.GetRiderByID(forward(ctx), &taskpb.IdRequest{Id: id})
	if err != nil {
		return nil, toDomain(err)
	}
	var out rider.Profile
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Positions reads the latest rider position owned by Tracking.
type Positions struct{ Client trackingclient.Tracking }

func (a Positions) GetPosition(ctx context.Context, riderID string) (*location.Position, error) {
	reply, err := a.Client.GetPositionInternal(forward(ctx), &trackingpb.IdRequest{Id: riderID})
	if err != nil {
		return nil, toDomain(err)
	}
	var out location.Position
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Trusted resolves provider-verified places owned by Discovery.
type Trusted struct{ Client discoveryclient.Discovery }

func (a Trusted) FindTrustedPlaces(ctx context.Context, ids []string, now time.Time) (map[string]repository.TrustedPlace, error) {
	body, _ := json.Marshal(map[string]any{"ids": ids, "now": now})
	reply, err := a.Client.FindTrustedPlacesInternal(forward(ctx), &discoverypb.JsonBody{BodyJson: body})
	if err != nil {
		return nil, toDomain(err)
	}
	out := make(map[string]repository.TrustedPlace)
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a Trusted) SaveTrustedPlaces(ctx context.Context, places []repository.TrustedPlace) error {
	body, _ := json.Marshal(places)
	_, err := a.Client.SaveTrustedPlacesInternal(forward(ctx), &discoverypb.JsonBody{BodyJson: body})
	return toDomain(err)
}

// PaymentGate plugs Payment facts into TaskService without Task reading the
// payment tables.
type PaymentGate struct{ Client paymentclient.Payment }

type paymentState struct {
	Status    string `json:"status"`
	AmountFen int64  `json:"amount_fen"`
	Found     bool   `json:"found"`
}

func (a PaymentGate) state(ctx context.Context, taskID string) (*paymentState, error) {
	reply, err := a.Client.TaskPaymentStateInternal(forward(ctx), &paymentpb.IdRequest{Id: taskID})
	if err != nil {
		return nil, toDomain(err)
	}
	var out paymentState
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a PaymentGate) EnsureClaimable(ctx context.Context, item *task.Task) error {
	if item == nil || item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
		return nil
	}
	state, err := a.state(ctx, item.ID)
	if err != nil {
		return err
	}
	if !state.Found {
		return fmt.Errorf("%w: 请先完成支付后再接单", domain.ErrConflict)
	}
	if state.Status != payment.PaymentPaid {
		return fmt.Errorf("%w: 任务尚未收到付款，暂不能接单", domain.ErrConflict)
	}
	return nil
}

func (a PaymentGate) EnsureEditable(ctx context.Context, item *task.Task, nextAmount *int64) error {
	if item == nil || item.QuotedAmountFen == nil {
		return nil
	}
	state, err := a.state(ctx, item.ID)
	if err != nil {
		return err
	}
	if !state.Found {
		return nil
	}
	if state.Status != payment.PaymentPending && state.Status != payment.PaymentPaid {
		return nil
	}
	if nextAmount == nil || *nextAmount != state.AmountFen {
		return fmt.Errorf("%w: 已发起支付，任务金额不能修改", domain.ErrConflict)
	}
	return nil
}

func (a PaymentGate) AfterCancel(ctx context.Context, item *task.Task) {
	if item == nil {
		return
	}
	body, _ := json.Marshal(item)
	_, _ = a.Client.RefundForTaskInternal(forward(ctx), &paymentpb.JsonBody{BodyJson: body})
}

// Settlements triggers payout creation in Payment when a task completes.
type Settlements struct{ Client paymentclient.Payment }

func (a Settlements) EnsureSettlement(ctx context.Context, item *task.Task) (*payment.Settlement, error) {
	body, _ := json.Marshal(item)
	reply, err := a.Client.EnsureSettlementInternal(forward(ctx), &paymentpb.JsonBody{BodyJson: body})
	if err != nil {
		return nil, toDomain(err)
	}
	if len(reply.BodyJson) == 0 || string(reply.BodyJson) == "null" {
		return nil, nil
	}
	var out payment.Settlement
	if err := decode(reply.BodyJson, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
