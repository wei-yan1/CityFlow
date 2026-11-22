package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"cityflow/app/gateway/cmd/api/internal/svc"
	paymentclient "cityflow/app/payment/cmd/rpc/paymentclient"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func CreateTaskPayment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Payment.CreateCharge(rpcContext(r), &paymentclient.IdBody{Id: pathvar.Vars(r)["id"], BodyJson: body})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, reply.GetBodyJson())
	}
}

func GetTaskPayment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		if _, err := s.Task.Get(rpcContext(r), &taskclient.GetRequest{Id: pathvar.Vars(r)["id"]}); err != nil {
			writeTaskError(w, err)
			return
		}
		reply, err := s.Payment.Summary(rpcContext(r), &paymentclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func PaymentNotify(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provider := pathvar.Vars(r)["provider"]
		if s.Payment == nil {
			writeNotifyFailure(w, provider, "payment service unavailable")
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeNotifyFailure(w, provider, "could not read the notification")
			return
		}
		headers, _ := json.Marshal(map[string]string{
			"wechatpay-timestamp": r.Header.Get("Wechatpay-Timestamp"),
			"wechatpay-nonce":     r.Header.Get("Wechatpay-Nonce"),
			"wechatpay-signature": r.Header.Get("Wechatpay-Signature"),
			"wechatpay-serial":    r.Header.Get("Wechatpay-Serial"),
		})
		if _, err := s.Payment.Notify(r.Context(), &paymentclient.NotifyRequest{
			Provider: provider, HeadersJson: headers, Body: body,
		}); err != nil {
			writeNotifyFailure(w, provider, paymentErrorMessage(err))
			return
		}
		switch provider {
		case "alipay":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("success"))
		case "wechat":
			writeRawJSON(w, http.StatusOK, []byte(`{"code":"SUCCESS","message":"成功"}`))
		default:
			writeRawJSON(w, http.StatusOK, []byte(`{"status":"ok"}`))
		}
	}
}

func writeNotifyFailure(w http.ResponseWriter, provider, message string) {
	switch provider {
	case "alipay":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("failure"))
	case "wechat":
		writeRawJSON(w, http.StatusBadRequest, mustJSON(map[string]string{"code": "FAIL", "message": message}))
	default:
		httpkit.WriteError(w, http.StatusBadRequest, message)
	}
}

func MockPay(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		reply, err := s.Payment.MockPay(rpcContext(r), &paymentclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func SyncPayment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		reply, err := s.Payment.Sync(rpcContext(r), &paymentclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func RefundPayment(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Payment.Refund(rpcContext(r), &paymentclient.IdBody{Id: pathvar.Vars(r)["id"], BodyJson: body})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func RiderSettlements(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		limit, offset := pageQuery(r)
		reply, err := s.Payment.Settlements(rpcContext(r), &paymentclient.ListQuery{
			Status: r.URL.Query().Get("status"), Limit: limit, Offset: offset,
		})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func GetPayoutAccount(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		reply, err := s.Payment.GetPayoutAccount(rpcContext(r), &paymentclient.Empty{})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func SavePayoutAccount(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Payment == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "payment service unavailable")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		reply, err := s.Payment.SavePayoutAccount(rpcContext(r), &paymentclient.JsonBody{BodyJson: body})
		if err != nil {
			writePaymentError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}

func writePaymentError(w http.ResponseWriter, err error) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "resource not found",
		Conflict:        "operation conflict",
		BadRequest:      "invalid request",
		Unavailable:     "payment service unavailable",
		Internal:        "could not process payment request",
	})
}

func paymentErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mustJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}
