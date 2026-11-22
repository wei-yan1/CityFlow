package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cityflow/app/gateway/cmd/api/internal/svc"
	promotionclient "cityflow/app/promotion/cmd/rpc/promotionclient"
	"cityflow/internal/common/auth"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

type redeemCodeView struct {
	ID          string `json:"id"`
	CouponID    string `json:"coupon_id"`
	BatchNo     string `json:"batch_no"`
	Code        string `json:"code"`
	Seq         uint64 `json:"seq"`
	MaxRedeem   int    `json:"max_redeem"`
	UsedCount   int    `json:"used_count"`
	Status      string `json:"status"`
	CreatedBy   string `json:"created_by"`
	Note        string `json:"note,omitempty"`
	StartsAtMs  int64  `json:"starts_at_ms,omitempty"`
	EndsAtMs    int64  `json:"ends_at_ms,omitempty"`
	CreatedAtMs int64  `json:"created_at_ms,omitempty"`
	UpdatedAtMs int64  `json:"updated_at_ms,omitempty"`
}

func GenerateRedeemCodes(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := requireAdmin(w, r)
		if userID == "" {
			return
		}
		couponID := pathvar.Vars(r)["id"]
		if couponID == "" {
			httpkit.WriteError(w, http.StatusBadRequest, "coupon id is required")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		var request struct {
			Count     int    `json:"count"`
			MaxRedeem int    `json:"max_redeem"`
			BatchNo   string `json:"batch_no"`
			Note      string `json:"note"`
			StartsAt  string `json:"starts_at"`
			EndsAt    string `json:"ends_at"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid redeem code request")
			return
		}
		startsAt, err := parseAdminTime(request.StartsAt)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid starts_at")
			return
		}
		endsAt, err := parseAdminTime(request.EndsAt)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid ends_at")
			return
		}
		reply, err := s.Promotion.GenerateRedeemCodes(rpcContext(r), &promotionclient.GenerateRedeemCodesReq{
			CouponId: couponID, Count: int32(request.Count), MaxRedeem: int32(request.MaxRedeem),
			BatchNo: request.BatchNo, CreatedBy: userID, Note: request.Note,
			StartsAtMs: millisPtr(startsAt), EndsAtMs: millisPtr(endsAt),
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, mustJSON(map[string]any{
			"data": redeemCodeViews(reply.GetCodes()), "batch_no": reply.GetBatchNo(),
		}))
	}
}

func ListRedeemCodes(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if requireAdmin(w, r) == "" {
			return
		}
		limit, offset := pageQuery(r)
		reply, err := s.Promotion.ListRedeemCodes(rpcContext(r), &promotionclient.ListRedeemCodesReq{
			CouponId: redeemCouponID(r), BatchNo: r.URL.Query().Get("batch_no"),
			Status: r.URL.Query().Get("status"), Limit: limit, Offset: offset,
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{
			"data": redeemCodeViews(reply.GetCodes()), "limit": reply.GetLimit(), "offset": reply.GetOffset(),
		}))
	}
}

func ListAllRedeemCodes(s *svc.ServiceContext) http.HandlerFunc {
	return ListRedeemCodes(s)
}

func ExportRedeemCodes(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if requireAdmin(w, r) == "" {
			return
		}
		reply, err := s.Promotion.ExportRedeemCodes(rpcContext(r), &promotionclient.ExportRedeemCodesReq{
			CouponId: redeemCouponID(r), BatchNo: r.URL.Query().Get("batch_no"),
			Status: r.URL.Query().Get("status"),
		})
		if err != nil {
			writePromotionError(w, err)
			return
		}
		couponID := redeemCouponID(r)
		if couponID == "" {
			couponID = "all"
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="redeem-codes-%s.csv"`, couponID))
		_, _ = w.Write([]byte("\ufeff"))
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"券ID", "批次号", "兑换码", "序列号", "可兑换人数", "已兑换", "状态", "创建人", "备注", "开始时间（UTC+8）", "结束时间（UTC+8）", "创建时间（UTC+8）"})
		for _, item := range reply.GetCodes() {
			_ = writer.Write([]string{
				item.GetCouponId(), item.GetBatchNo(), item.GetCode(), strconv.FormatUint(item.GetSeq(), 10),
				strconv.Itoa(int(item.GetMaxRedeem())), strconv.Itoa(int(item.GetUsedCount())), item.GetStatus(),
				item.GetCreatedBy(), item.GetNote(), formatMillis(item.GetStartsAtMs()), formatMillis(item.GetEndsAtMs()),
				formatMillis(item.GetCreatedAtMs()),
			})
		}
		writer.Flush()
	}
}

func ExportAllRedeemCodes(s *svc.ServiceContext) http.HandlerFunc {
	return ExportRedeemCodes(s)
}

func DisableRedeemCode(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if requireAdmin(w, r) == "" {
			return
		}
		id := pathvar.Vars(r)["id"]
		if id == "" {
			httpkit.WriteError(w, http.StatusBadRequest, "redeem code id is required")
			return
		}
		if _, err := s.Promotion.DisableRedeemCode(rpcContext(r), &promotionclient.DisableRedeemCodeReq{Id: id}); err != nil {
			writePromotionError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func requireAdmin(w http.ResponseWriter, r *http.Request) string {
	userID, role, ok := requestUser(r)
	if !ok {
		httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
		return ""
	}
	if role != auth.RoleAdmin {
		httpkit.WriteError(w, http.StatusForbidden, "operation is not allowed")
		return ""
	}
	return userID
}

func redeemCouponID(r *http.Request) string {
	if id := pathvar.Vars(r)["id"]; id != "" {
		return id
	}
	return strings.TrimSpace(r.URL.Query().Get("coupon_id"))
}

func redeemCodeViews(items []*promotionclient.RedeemCode) []redeemCodeView {
	result := make([]redeemCodeView, 0, len(items))
	for _, item := range items {
		result = append(result, redeemCodeView{
			ID: item.GetId(), CouponID: item.GetCouponId(), BatchNo: item.GetBatchNo(),
			Code: item.GetCode(), Seq: item.GetSeq(), MaxRedeem: int(item.GetMaxRedeem()),
			UsedCount: int(item.GetUsedCount()), Status: item.GetStatus(), CreatedBy: item.GetCreatedBy(),
			Note: item.GetNote(), StartsAtMs: item.GetStartsAtMs(), EndsAtMs: item.GetEndsAtMs(),
			CreatedAtMs: item.GetCreatedAtMs(), UpdatedAtMs: item.GetUpdatedAtMs(),
		})
	}
	return result
}

var adminTimeZone = time.FixedZone("UTC+8", 8*60*60)

func parseAdminTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		var (
			parsed time.Time
			err    error
		)
		if layout == time.RFC3339 {
			parsed, err = time.Parse(layout, value)
		} else {
			parsed, err = time.ParseInLocation(layout, value, adminTimeZone)
		}
		if err == nil {
			utc := parsed.UTC()
			return &utc, nil
		}
	}
	return nil, fmt.Errorf("invalid time %q", value)
}

func millisPtr(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.UnixMilli()
}

func formatMillis(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.UnixMilli(value).In(adminTimeZone).Format("2006-01-02 15:04:05")
}
