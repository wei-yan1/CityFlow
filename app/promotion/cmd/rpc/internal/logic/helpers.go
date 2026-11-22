package logic

import (
	"context"
	"errors"
	"time"

	"cityflow/app/promotion/cmd/rpc/pb"
	"cityflow/app/promotion/internal/application/promotionapp"
	"cityflow/app/promotion/internal/domain/coupon"
	"cityflow/internal/common/auth"
	"cityflow/internal/platform/rpckit"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func couponPage(limit, offset int32) promotionapp.Page {
	return promotionapp.Page{Limit: int(limit), Offset: int(offset)}
}

// requireAdmin guards redeem-code management. Gateway also checks the role,
// but the RPC listener must not rely on that single layer.
func requireAdmin(ctx context.Context) error {
	claims, err := rpckit.Claims(ctx)
	if err != nil {
		return err
	}
	return rpckit.RequireRole(auth.RoleAdmin, claims.Role)
}

func toProtoCoupon(item *coupon.Coupon) *pb.Coupon {
	if item == nil {
		return nil
	}
	return &pb.Coupon{
		Id: item.ID, Code: item.Code, Title: item.Title, Subtitle: item.Subtitle,
		Description: item.Description, Type: item.Type,
		ApplicableTaskTypes: append([]string(nil), item.ApplicableTaskTypes...),
		ValueFen:            item.ValueFen, DiscountPercent: int32(item.DiscountPercent),
		MaxDiscountFen: item.MaxDiscountFen, MinSpendFen: item.MinSpendFen,
		TotalQuantity: int32(item.TotalQuantity), ClaimedCount: int32(item.ClaimedCount),
		PerUserLimit: int32(item.PerUserLimit), Status: item.Status, Remaining: int32(item.Remaining),
		UserClaimCount: int32(item.UserClaimCount), UserCouponId: item.UserCouponID,
		UserCouponStatus: item.UserCouponStatus,
		StartsAtMs:       timeMillis(item.StartsAt), EndsAtMs: timeMillis(item.EndsAt),
		CreatedAtMs: item.CreatedAt.UnixMilli(), UpdatedAtMs: item.UpdatedAt.UnixMilli(),
	}
}

func toProtoUserCoupon(item *coupon.UserCoupon) *pb.UserCoupon {
	if item == nil {
		return nil
	}
	return &pb.UserCoupon{
		Coupon: toProtoCoupon(item.Coupon), Id: item.ID, CouponId: item.CouponID,
		UserId: item.UserID, Status: item.Status, TaskId: item.TaskID,
		PaymentOrderId: item.PaymentOrder, ClaimNo: int32(item.ClaimNo),
		DiscountFen: item.DiscountFen, ClaimedAtMs: item.ClaimedAt.UnixMilli(),
		UsedAtMs: timeMillis(item.UsedAt),
	}
}

func toProtoQuote(item *coupon.Quote) *pb.Quote {
	if item == nil {
		return nil
	}
	quote := &pb.Quote{
		OriginalAmountFen: item.OriginalAmountFen, DiscountFen: item.DiscountFen,
		PayableAmountFen: item.PayableAmountFen,
	}
	if item.UserCoupon != nil {
		quote.UserCouponId = item.UserCoupon.ID
		quote.Status = item.UserCoupon.Status
	}
	if item.Coupon != nil {
		quote.CouponId = item.Coupon.ID
		quote.CouponTitle = item.Coupon.Title
	}
	return quote
}

func toProtoRedeemCode(item *coupon.RedeemCode) *pb.RedeemCode {
	if item == nil {
		return nil
	}
	return &pb.RedeemCode{
		Id: item.ID, CouponId: item.CouponID, BatchNo: item.BatchNo, Code: item.Code,
		Seq: item.Seq, MaxRedeem: int32(item.MaxRedeem), UsedCount: int32(item.UsedCount),
		Status: item.Status, CreatedBy: item.CreatedBy, Note: item.Note,
		StartsAtMs: timeMillis(item.StartsAt), EndsAtMs: timeMillis(item.EndsAt),
		CreatedAtMs: item.CreatedAt.UnixMilli(), UpdatedAtMs: item.UpdatedAt.UnixMilli(),
	}
}

func millisTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	result := time.UnixMilli(value).UTC()
	return &result
}

func timeMillis(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UnixMilli()
}

func promotionError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, coupon.ErrInvalidRequest):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, coupon.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, coupon.ErrConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, coupon.ErrPending):
		return status.Error(codes.Aborted, "领取处理中，请使用同一 Idempotency-Key 重试")
	case errors.Is(err, coupon.ErrUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
