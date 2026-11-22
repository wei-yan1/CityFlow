// Package paymentrpc adapts Payment RPC to Task's payment-policy ports.
// It is Task-specific because the payloads are Task aggregates and status
// policy; the shared rpcadapt package must not import Task's domain.
package paymentrpc

import (
	"context"
	"encoding/json"
	"fmt"

	paymentclient "cityflow/app/payment/cmd/rpc/paymentclient"
	paymentpb "cityflow/app/payment/cmd/rpc/pb"
	"cityflow/app/task/domain/task"
	"cityflow/internal/common/domain"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	paymentPending = "PENDING"
	paymentPaid    = "PAID"
)

type Gate struct{ Client paymentclient.Payment }

type paymentState struct {
	Status    string `json:"status"`
	AmountFen int64  `json:"amount_fen"`
	Found     bool   `json:"found"`
}

func (a Gate) state(ctx context.Context, taskID string) (*paymentState, error) {
	reply, err := a.Client.TaskPaymentStateInternal(forward(ctx), &paymentpb.IdRequest{Id: taskID})
	if err != nil {
		return nil, toDomain(err)
	}
	var out paymentState
	if err := json.Unmarshal(reply.GetBodyJson(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a Gate) EnsureClaimable(ctx context.Context, item *task.Task) error {
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
	if state.Status != paymentPaid {
		return fmt.Errorf("%w: 任务尚未收到付款，暂不能接单", domain.ErrConflict)
	}
	return nil
}

func (a Gate) EnsureEditable(ctx context.Context, item *task.Task, nextAmount *int64) error {
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
	if state.Status != paymentPending && state.Status != paymentPaid {
		return nil
	}
	if nextAmount == nil || *nextAmount != state.AmountFen {
		return fmt.Errorf("%w: 已发起支付，任务金额不能修改", domain.ErrConflict)
	}
	return nil
}

func (a Gate) AfterCancel(ctx context.Context, item *task.Task) {
	if item == nil {
		return
	}
	body, _ := json.Marshal(item)
	_, _ = a.Client.RefundForTaskInternal(forward(ctx), &paymentpb.JsonBody{BodyJson: body})
}

// Settlements triggers payout creation in Payment when a task completes.
type Settlements struct{ Client paymentclient.Payment }

func (a Settlements) EnsureSettlement(ctx context.Context, item *task.Task) error {
	body, _ := json.Marshal(item)
	_, err := a.Client.EnsureSettlementInternal(forward(ctx), &paymentpb.JsonBody{BodyJson: body})
	if err != nil {
		return toDomain(err)
	}
	return nil
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
