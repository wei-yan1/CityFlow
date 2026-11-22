package svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	paymentservice "cityflow/app/payment/application/payment"
	promotionclient "cityflow/app/promotion/cmd/rpc/promotionclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/rpcctx"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type promotionCouponGate struct {
	tasks     taskclient.Task
	promotion promotionclient.Promotion
}

type taskSnapshot struct {
	Type            string `json:"type"`
	QuotedAmountFen *int64 `json:"quoted_amount_fen"`
}

func (g promotionCouponGate) QuoteForTask(ctx context.Context, userID, claimID, taskID string) (*paymentservice.CouponQuote, error) {
	return g.quote(ctx, userID, claimID, taskID, false)
}

func (g promotionCouponGate) LockForTask(ctx context.Context, userID, claimID, taskID string) (*paymentservice.CouponQuote, error) {
	return g.quote(ctx, userID, claimID, taskID, true)
}

func (g promotionCouponGate) quote(ctx context.Context, userID, claimID, taskID string, lock bool) (*paymentservice.CouponQuote, error) {
	if g.tasks == nil || g.promotion == nil {
		return nil, fmt.Errorf("promotion coupon gate is not configured")
	}
	reply, err := g.tasks.GetTaskInternal(forwardRPC(ctx), &taskclient.IdRequest{Id: taskID})
	if err != nil {
		return nil, err
	}
	var snapshot taskSnapshot
	if err := json.Unmarshal([]byte(reply.GetBodyJson()), &snapshot); err != nil {
		return nil, fmt.Errorf("decode task %s: %w", taskID, err)
	}
	if snapshot.QuotedAmountFen == nil || *snapshot.QuotedAmountFen <= 0 {
		return nil, errors.New("task has no quoted amount")
	}
	quoteReply, err := g.promotion.Quote(forwardRPC(ctx), &promotionclient.QuoteTaskReq{
		UserId: userID, UserCouponId: claimID, TaskId: taskID,
		AmountFen: *snapshot.QuotedAmountFen, TaskType: snapshot.Type, Lock: lock,
	})
	if err != nil {
		return nil, promotionError(err)
	}
	quote := quoteReply.GetQuote()
	if quote == nil {
		return nil, errors.New("promotion returned an empty quote")
	}
	return &paymentservice.CouponQuote{
		ClaimID: quote.GetUserCouponId(), CouponID: quote.GetCouponId(), CouponTitle: quote.GetCouponTitle(),
		OriginalAmountFen: quote.GetOriginalAmountFen(), DiscountFen: quote.GetDiscountFen(),
		PayableAmountFen: quote.GetPayableAmountFen(),
	}, nil
}

func (g promotionCouponGate) MarkUsed(ctx context.Context, userID, claimID, taskID string, discountFen int64) error {
	if g.promotion == nil {
		return errors.New("promotion coupon gate is not configured")
	}
	_, err := g.promotion.MarkUsed(forwardRPC(ctx), &promotionclient.MarkUsedReq{
		UserCouponId: claimID, TaskId: taskID, DiscountFen: discountFen,
	})
	return promotionError(err)
}

func (g promotionCouponGate) Release(ctx context.Context, claimID, taskID string) {
	if g.promotion == nil {
		return
	}
	_, _ = g.promotion.Release(forwardRPC(ctx), &promotionclient.ReleaseCouponReq{UserCouponId: claimID, TaskId: taskID})
}

func forwardRPC(ctx context.Context) context.Context {
	if token, ok := authn.BearerFromMetadata(ctx); ok {
		return rpcctx.WithBearer(ctx, token)
	}
	return ctx
}

func promotionError(err error) error {
	if err == nil {
		return nil
	}
	switch status.Code(err) {
	case codes.NotFound:
		return fmt.Errorf("promotion coupon not found: %w", err)
	case codes.ResourceExhausted:
		return err
	case codes.Unavailable:
		return err
	default:
		message := status.Convert(err).Message()
		if strings.Contains(strings.ToLower(message), "not found") {
			return err
		}
		return err
	}
}
