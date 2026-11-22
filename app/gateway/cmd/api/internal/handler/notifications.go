package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	notificationclient "cityflow/app/notification/cmd/rpc/notificationclient"
	"cityflow/internal/platform/httpkit"
)

func ListNotifications(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		limit, offset := pageQuery(r)
		reply, err := s.Notification.List(rpcContext(r), &notificationclient.ListReq{
			UserId: userID, UnreadOnly: r.URL.Query().Get("unread") == "true", Limit: limit, Offset: offset,
		})
		if err != nil {
			writeNotificationError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

func UnreadNotifications(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		reply, err := s.Notification.Unread(rpcContext(r), &notificationclient.UnreadReq{UserId: userID})
		if err != nil {
			writeNotificationError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

func MarkNotificationsRead(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, _, ok := requestUser(r)
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		var body struct {
			IDs []string `json:"ids"`
			All bool     `json:"all"`
		}
		raw, err := readBody(w, r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(strings.TrimSpace(string(raw))) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				httpkit.WriteError(w, http.StatusBadRequest, "invalid notification payload")
				return
			}
		}
		reply, err := s.Notification.MarkRead(rpcContext(r), &notificationclient.MarkReadReq{
			UserId: userID, Ids: body.IDs, All: body.All,
		})
		if err != nil {
			writeNotificationError(w, err)
			return
		}
		writeProtoJSON(w, http.StatusOK, reply)
	}
}

func writeNotificationError(w http.ResponseWriter, err error) {
	httpkit.WriteGRPCError(w, err, httpkit.GRPCErrorMessages{
		Unauthenticated: "authentication required",
		Forbidden:       "operation is not allowed",
		NotFound:        "notification not found",
		BadRequest:      "invalid notification request",
		Unavailable:     "notification service unavailable",
		Internal:        "could not process notification request",
	})
}
