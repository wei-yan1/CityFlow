package handler

import (
	"net/http"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/logic"
	"cityflow/app/gateway/cmd/api/internal/svc"
	identityclient "cityflow/app/identity/cmd/rpc/identityclient"
	"cityflow/internal/platform/authctx"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

// Register is the public account-creation endpoint. Identity owns validation,
// uniqueness, password hashing and persistence; Gateway only forwards JSON.
func Register(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.Register(r.Context(), &identityclient.JsonBody{BodyJson: body})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func RequestPasswordReset(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.RequestPasswordReset(r.Context(), &identityclient.JsonBody{BodyJson: body})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ResetPassword(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := s.Identity.ResetPassword(r.Context(), &identityclient.JsonBody{BodyJson: body}); err != nil {
			writeIdentityError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func UserProfile(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Identity.Me(rpcContext(r), &identityclient.MeRequest{})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		if response.GetUser() == nil {
			httpkit.WriteError(w, http.StatusNotFound, "user not found")
			return
		}
		httpkit.WriteJSON(w, http.StatusOK, logic.User(response.GetUser()))
	}
}

func UpdateUserProfile(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.UpdateMe(rpcContext(r), &identityclient.JsonBody{BodyJson: body})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ListUsers(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpkit.Page(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.ListUsers(rpcContext(r), &identityclient.ListQuery{
			Status: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  int32(page.Limit), Offset: int32(page.Offset),
		})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func CreateUser(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.CreateUser(rpcContext(r), &identityclient.JsonBody{BodyJson: body})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func GetUser(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := s.Identity.GetUserAdmin(rpcContext(r), &identityclient.IdRequest{Id: pathvar.Vars(r)["id"]})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func UpdateUser(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.UpdateUser(rpcContext(r), &identityclient.IdBody{
			Id: pathvar.Vars(r)["id"], BodyJson: body,
		})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func DisableUser(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := pathvar.Vars(r)["id"]
		if claims, ok := authctx.Claims(r.Context()); ok && claims.Subject == id {
			httpkit.WriteError(w, http.StatusConflict, "you cannot disable your own account")
			return
		}
		response, err := s.Identity.DisableUser(rpcContext(r), &identityclient.IdRequest{Id: id})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func SubmitRiderRegistration(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.SubmitRiderRegistration(r.Context(), &identityclient.JsonBody{BodyJson: body})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusCreated, response.GetBodyJson())
	}
}

func ListRiderRegistrations(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := httpkit.Page(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := s.Identity.ListRiderRegistrations(rpcContext(r), &identityclient.ListQuery{
			Status: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
			Limit:  int32(page.Limit), Offset: int32(page.Offset),
		})
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func ApproveRiderRegistration(s *svc.ServiceContext) http.HandlerFunc {
	return reviewRiderRegistration(s, true)
}

func RejectRiderRegistration(s *svc.ServiceContext) http.HandlerFunc {
	return reviewRiderRegistration(s, false)
}

func reviewRiderRegistration(s *svc.ServiceContext, approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(body) == 0 {
			body = []byte("{}")
		}
		request := &identityclient.IdBody{Id: pathvar.Vars(r)["id"], BodyJson: body}
		var response *identityclient.JsonReply
		if approve {
			response, err = s.Identity.ApproveRiderRegistration(rpcContext(r), request)
		} else {
			response, err = s.Identity.RejectRiderRegistration(rpcContext(r), request)
		}
		if err != nil {
			writeIdentityError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, response.GetBodyJson())
	}
}

func writeIdentityError(w http.ResponseWriter, err error) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "account resource not found",
		Conflict:        "account or registration conflict",
		BadRequest:      "invalid account request",
		Unavailable:     "identity service unavailable",
		Internal:        "could not process account request",
	})
}
