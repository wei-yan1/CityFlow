package handler

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/platform/authn"
	"cityflow/internal/platform/httpkit"
	"cityflow/internal/platform/rpcctx"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func ListTasks(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpkit.Page(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.List(rpcContext(r), &taskclient.ListRequest{
			Status: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  int64(page.Limit),
			Offset: int64(page.Offset),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func CreateTask(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.Create(rpcContext(r), &taskclient.CreateRequest{
			BodyJson: body, IdempotencyKey: r.Header.Get("Idempotency-Key"),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func GetTask(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.Get(rpcContext(r), &taskclient.GetRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func UpdateTask(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.Update(rpcContext(r), &taskclient.UpdateRequest{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func CancelTask(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.Cancel(rpcContext(r), &taskclient.CancelRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ClaimTask(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.Claim(rpcContext(r), &taskclient.ClaimRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func UpdateStatus(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.UpdateStatus(rpcContext(r), &taskclient.StatusRequest{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func AdvancePickup(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(pathvar.Vars(r)["index"])
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid pickup index")
			return
		}
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.AdvancePickup(rpcContext(r), &taskclient.PickupRequest{
			Id: pathvar.Vars(r)["id"], Index: int32(index), BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	return io.ReadAll(r.Body)
}

func rpcContext(r *http.Request) context.Context {
	token, _ := authn.BearerToken(r)
	return rpcctx.WithBearer(r.Context(), token)
}

func writeRawJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

func writeTaskError(w http.ResponseWriter, err error) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "resource not found",
		Conflict:        "operation conflict",
		BadRequest:      "invalid request",
		Unavailable:     "task service unavailable",
		Internal:        "could not process task",
	})
}
