package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	taskclient "cityflow/app/task/cmd/rpc/taskclient"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

func RiderMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.GetMe(rpcContext(r), &taskclient.IdRequest{})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func UpdateRiderMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.UpdateMe(rpcContext(r), &taskclient.IdBody{BodyJson: body})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func UpdateRiderStatus(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.SetMyStatus(rpcContext(r), &taskclient.IdBody{BodyJson: body})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ArchiveRiderMe(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.ArchiveMe(rpcContext(r), &taskclient.IdRequest{})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		_ = response
		w.WriteHeader(http.StatusNoContent)
	}
}

func TaskRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.TaskRider(rpcContext(r), &taskclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func PublicRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.PublicRider(rpcContext(r), &taskclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func CreateRiderReview(s *svc.ServiceContext) http.HandlerFunc {
	return createRiderFeedback(s, "REVIEW")
}

func CreateRiderTip(s *svc.ServiceContext) http.HandlerFunc {
	return createRiderFeedback(s, "TIP")
}

func CreateRiderReport(s *svc.ServiceContext) http.HandlerFunc {
	return createRiderFeedback(s, "REPORT")
}

func createRiderFeedback(s *svc.ServiceContext, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBodyLimit(w, r, 12<<20)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid feedback body")
			return
		}
		payload["kind"] = json.RawMessage(strconv.Quote(kind))
		body, err = json.Marshal(payload)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid feedback body")
			return
		}
		response, err := s.Task.CreateRiderFeedback(rpcContext(r), &taskclient.IdBody{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func CallRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.CallRider(rpcContext(r), &taskclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func CreateTaskIncident(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBodyLimit(w, r, 12<<20)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.CreateIncident(rpcContext(r), &taskclient.IdBody{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func ResolveTaskIncident(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.ResolveIncident(rpcContext(r), &taskclient.ResolveIncidentRequest{
			Id: pathvar.Vars(r)["id"], IncidentId: pathvar.Vars(r)["incidentId"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ListTaskIncidents(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpkit.Page(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.ListIncidents(rpcContext(r), &taskclient.ListQuery{
			Status: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  int32(page.Limit), Offset: int32(page.Offset),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ListRiders(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpkit.Page(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.ListRiders(rpcContext(r), &taskclient.ListQuery{
			Status: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  int32(page.Limit), Offset: int32(page.Offset),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func CreateRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid rider body")
			return
		}
		rawUserID, ok := payload["user_id"]
		if !ok {
			httpkit.WriteError(w, http.StatusBadRequest, "user_id is required")
			return
		}
		var userID string
		if err := json.Unmarshal(rawUserID, &userID); err != nil || strings.TrimSpace(userID) == "" {
			httpkit.WriteError(w, http.StatusBadRequest, "user_id is required")
			return
		}
		delete(payload, "user_id")
		updateBody, err := json.Marshal(payload)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid rider body")
			return
		}
		response, err := s.Task.CreateRider(rpcContext(r), &taskclient.IdBody{
			Id: userID, BodyJson: updateBody,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func GetRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.GetRiderAdmin(rpcContext(r), &taskclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func UpdateRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.UpdateRider(rpcContext(r), &taskclient.IdBody{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ArchiveRider(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Task.ArchiveRider(rpcContext(r), &taskclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		_ = response
		w.WriteHeader(http.StatusNoContent)
	}
}

func ListRiderReports(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpkit.Page(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.ListRiderReports(rpcContext(r), &taskclient.ListQuery{
			Status: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  int32(page.Limit), Offset: int32(page.Offset),
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ResolveRiderReport(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Task.ResolveRiderReport(rpcContext(r), &taskclient.IdBody{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeTaskError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func readBodyLimit(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return io.ReadAll(r.Body)
}
