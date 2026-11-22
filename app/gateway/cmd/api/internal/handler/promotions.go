package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	promotionclient "cityflow/app/promotion/cmd/rpc/promotionclient"
	"cityflow/internal/platform/authctx"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func ListPromotionCoupons(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := ""
		if claims, ok := authctx.Claims(r.Context()); ok {
			userID = claims.Subject
		}
		limit, offset := pageQuery(r)
		reply, err := s.Promotion.ListAvailable(rpcContext(r), &promotionclient.ListCouponsReq{
			UserId: userID, Limit: limit, Offset: offset,
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

func ListMyPromotionCoupons(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		limit, offset := pageQuery(r)
		reply, err := s.Promotion.ListMine(rpcContext(r), &promotionclient.ListMineReq{
			UserId: userID, Status: r.URL.Query().Get("status"), Limit: limit, Offset: offset,
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

func ClaimPromotionCoupon(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		reply, err := s.Promotion.Claim(rpcContext(r), &promotionclient.ClaimCouponReq{
			UserId: userID, CouponId: pathvar.Vars(r)["id"],
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

func RedeemPromotionCoupon(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		var request struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(body, &request); err != nil || strings.TrimSpace(request.Code) == "" {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid redeem code request")
			return
		}
		reply, err := s.Promotion.RedeemCode(rpcContext(r), &promotionclient.RedeemCodeReq{
			UserId: userID, Code: strings.TrimSpace(request.Code),
		})
		if err != nil {
			writeRedeemError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

// writeRedeemError keeps redeem-code failures user-facing: the learner should
// never see the raw RPC text "coupon not found" or an internal state conflict.
func writeRedeemError(w http.ResponseWriter, err error) {
	code := grpcstatus.Code(err)
	message := promotionReason(err)
	switch code {
	case codes.NotFound:
		httpkit.WriteJSON(w, http.StatusNotFound, map[string]string{"message": "兑换码不存在或已失效"})
		return
	case codes.FailedPrecondition:
		if strings.Contains(message, "领取上限") || strings.Contains(message, "已领取") {
			httpkit.WriteJSON(w, http.StatusConflict, map[string]string{"message": "你已领取过该券，不能重复兑换"})
			return
		}
		httpkit.WriteJSON(w, http.StatusConflict, map[string]string{"message": message})
		return
	case codes.InvalidArgument:
		httpkit.WriteJSON(w, http.StatusBadRequest, map[string]string{"message": message})
		return
	}
	writePromotionError(w, err)
}

func writePromotionError(w http.ResponseWriter, err error) {
	code := grpcstatus.Code(err)
	message := promotionReason(err)
	// 预扣成功但落库还没完成：返回 202，客户端必须复用同一个 Idempotency-Key 重试。
	if code == codes.Aborted && strings.Contains(message, "领取处理中") {
		httpkit.WriteJSON(w, http.StatusAccepted, map[string]string{
			"status":  "processing",
			"message": "领取处理中，请使用同一 Idempotency-Key 重试",
		})
		return
	}
	if code == codes.Unavailable && strings.Contains(message, "领取人数过多") {
		httpkit.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status":  "busy",
			"message": "领取人数过多，请稍后重试",
		})
		return
	}
	switch code {
	case codes.InvalidArgument:
		httpkit.WriteJSON(w, http.StatusBadRequest, map[string]string{"message": message})
		return
	case codes.NotFound:
		httpkit.WriteJSON(w, http.StatusNotFound, map[string]string{"message": "优惠券不存在"})
		return
	case codes.FailedPrecondition:
		httpkit.WriteJSON(w, http.StatusConflict, map[string]string{"message": message})
		return
	}
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "coupon not found",
		Conflict:        "coupon state conflict",
		BadRequest:      "invalid coupon request",
		Unavailable:     "promotion service unavailable",
		Internal:        "could not process coupon request",
	})
}

func promotionReason(err error) string {
	message := strings.TrimSpace(grpcstatus.Convert(err).Message())
	if index := strings.LastIndex(message, ": "); index >= 0 && index+2 < len(message) {
		candidate := strings.TrimSpace(message[index+2:])
		if candidate != "" {
			return candidate
		}
	}
	return message
}
