package handler

import (
	"encoding/json"
	"net/http"

	"cityflow/app/gateway/cmd/api/internal/svc"
	promotionclient "cityflow/app/promotion/cmd/rpc/promotionclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/common/auth"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

type taskCouponRef struct {
	ID       string `json:"id"`
	CouponID string `json:"coupon_id"`
	Status   string `json:"status,omitempty"`
}

type taskCouponInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type taskCouponQuote struct {
	UserCoupon        taskCouponRef  `json:"user_coupon"`
	Coupon            taskCouponInfo `json:"coupon"`
	OriginalAmountFen int64          `json:"original_amount_fen"`
	DiscountFen       int64          `json:"discount_fen"`
	PayableAmountFen  int64          `json:"payable_amount_fen"`
}

type taskCouponTask struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	CreatedBy       string `json:"created_by"`
	QuotedAmountFen *int64 `json:"quoted_amount_fen"`
}

// ListTaskCoupons quotes every unused coupon against the task's quoted amount.
// A task has no separate order row to read from, so the task itself is the
// authorization and pricing source.
func ListTaskCoupons(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, role, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		taskID := pathvar.Vars(r)["taskId"]
		reply, err := s.Task.Get(rpcContext(r), &taskclient.GetRequest{Id: taskID})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		var item taskCouponTask
		if err := json.Unmarshal([]byte(reply.GetBodyJson()), &item); err != nil {
			httpkit.WriteError(w, http.StatusInternalServerError, "could not read task")
			return
		}
		if role != auth.RoleAdmin && item.CreatedBy != userID {
			httpkit.WriteError(w, http.StatusNotFound, "task not found")
			return
		}
		if item.QuotedAmountFen == nil || *item.QuotedAmountFen <= 0 {
			writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{"items": []taskCouponQuote{}}))
			return
		}

		mine, err := s.Promotion.ListMine(rpcContext(r), &promotionclient.ListMineReq{
			UserId: item.CreatedBy, Status: "UNUSED", Limit: 100,
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		quotes := make([]taskCouponQuote, 0, len(mine.GetCoupons()))
		for _, claim := range mine.GetCoupons() {
			quote, err := s.Promotion.Quote(rpcContext(r), &promotionclient.QuoteTaskReq{
				UserId: item.CreatedBy, TaskId: item.ID, UserCouponId: claim.GetId(),
				AmountFen: *item.QuotedAmountFen, TaskType: item.Type,
			})
			if err != nil || quote.GetQuote() == nil {
				continue
			}
			result := quote.GetQuote()
			quotes = append(quotes, taskCouponQuote{
				UserCoupon:        taskCouponRef{ID: claim.GetId(), CouponID: claim.GetCouponId()},
				Coupon:            taskCouponInfo{ID: result.GetCouponId(), Title: result.GetCouponTitle()},
				OriginalAmountFen: result.GetOriginalAmountFen(),
				DiscountFen:       result.GetDiscountFen(),
				PayableAmountFen:  result.GetPayableAmountFen(),
			})
		}
		writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{"items": quotes}))
	}
}
