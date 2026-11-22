package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/app/gateway/cmd/api/internal/svc"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	"cityflow/internal/platform/httpkit"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetReaction(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			TargetType string `json:"target_type"`
			TargetID   string `json:"target_id"`
			Kind       string `json:"kind"`
			Active     bool   `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid reaction body")
			return
		}
		if strings.EqualFold(request.TargetType, "MERCHANT") {
			if s.Discovery == nil {
				httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
				return
			}
			body, _ := json.Marshal(request)
			reply, err := s.Discovery.SetReaction(rpcContext(r), &discoveryclient.JsonBody{BodyJson: body})
			if err != nil {
				writeReactionError(w, err)
				return
			}
			writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
			return
		}
		if s.Reaction == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
			return
		}
		reply, err := s.Reaction.SetReaction(rpcContext(r), &reactionclient.SetReactionReq{
			TargetType: request.TargetType, TargetId: request.TargetID,
			Kind: request.Kind, Active: request.Active,
		})
		if err != nil {
			writeReactionError(w, err)
			return
		}
		writeReactionJSON(w, map[string]any{"active": reply.GetActive(), "count": reply.GetCount()})
	}
}

func GetReactionState(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.URL.Query().Get("target_ids"))
		if raw == "" {
			raw = strings.TrimSpace(r.URL.Query().Get("ids"))
		}
		ids := make([]string, 0)
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		targetType := r.URL.Query().Get("target_type")
		if strings.EqualFold(targetType, "MERCHANT") {
			if s.Discovery == nil {
				httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
				return
			}
			body, _ := json.Marshal(map[string]any{
				"target_type": targetType,
				"kind":        r.URL.Query().Get("kind"),
				"target_ids":  ids,
			})
			reply, err := s.Discovery.ReactionStates(rpcContext(r), &discoveryclient.JsonBody{BodyJson: body})
			if err != nil {
				writeReactionError(w, err)
				return
			}
			writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
			return
		}
		if s.Reaction == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
			return
		}
		reply, err := s.Reaction.GetReactionState(rpcContext(r), &reactionclient.ReactionStateReq{
			TargetType: targetType,
			Kind:       r.URL.Query().Get("kind"),
			TargetIds:  ids,
		})
		if err != nil {
			writeReactionError(w, err)
			return
		}
		items := make([]map[string]any, 0, len(reply.GetItems()))
		for _, item := range reply.GetItems() {
			items = append(items, map[string]any{
				"target_id": item.GetTargetId(), "count": item.GetCount(), "liked": item.GetLiked(),
			})
		}
		writeReactionJSON(w, map[string]any{"items": items})
	}
}

func ListMineReactions(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.URL.Query().Get("target_type"), "MERCHANT") {
			httpkit.WriteError(w, http.StatusBadRequest, "merchant likes are month-scoped; use the merchant hot list")
			return
		}
		if s.Reaction == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
			return
		}
		reply, err := s.Reaction.ListMine(rpcContext(r), &reactionclient.ListMineReq{
			Kind:       r.URL.Query().Get("kind"),
			TargetType: r.URL.Query().Get("target_type"),
			Cursor:     r.URL.Query().Get("cursor"),
			Limit:      int32(queryLimit(r)),
		})
		if err != nil {
			writeReactionError(w, err)
			return
		}
		items := make([]map[string]any, 0, len(reply.GetItems()))
		for _, item := range reply.GetItems() {
			items = append(items, map[string]any{
				"target_id": item.GetTargetId(), "created_at_ms": item.GetCreatedAtMs(),
			})
		}
		writeReactionJSON(w, map[string]any{"items": items, "next_cursor": reply.GetNextCursor()})
	}
}

func writeReactionJSON(w http.ResponseWriter, body any) {
	writeRawJSON(w, http.StatusOK, mustJSON(body))
}

func writeReactionError(w http.ResponseWriter, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		httpkit.WriteError(w, http.StatusBadRequest, status.Convert(err).Message())
	case codes.Unauthenticated:
		httpkit.WriteError(w, http.StatusUnauthorized, "authentication required")
	case codes.PermissionDenied:
		httpkit.WriteError(w, http.StatusForbidden, "operation is not allowed")
	case codes.NotFound:
		httpkit.WriteError(w, http.StatusNotFound, "reaction target not found")
	case codes.Unavailable, codes.DeadlineExceeded:
		httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
	default:
		httpkit.WriteError(w, http.StatusInternalServerError, "reaction service failed")
	}
}
