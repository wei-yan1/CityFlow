package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	reactionclient "cityflow/app/reaction/cmd/rpc/reactionclient"
	"cityflow/internal/platform/httpkit"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetReaction(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Reaction == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
			return
		}
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
		reply, err := s.Reaction.SetReaction(rpcContext(r), &reactionclient.SetReactionReq{
			TargetType: request.TargetType, TargetId: request.TargetID,
			Kind: request.Kind, Active: request.Active,
		})
		if err != nil {
			writeReactionError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{
			"target_id": request.TargetID, "active": reply.GetActive(), "count": reply.GetCount(),
		}))
	}
}

func GetReactionState(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Reaction == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
			return
		}
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
		reply, err := s.Reaction.GetReactionState(rpcContext(r), &reactionclient.ReactionStateReq{
			TargetType: r.URL.Query().Get("target_type"),
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
		writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{"items": items}))
	}
}

func ListMineReactions(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Reaction == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "reaction service unavailable")
			return
		}
		reply, err := s.Reaction.ListMine(rpcContext(r), &reactionclient.ListMineReq{
			Kind: r.URL.Query().Get("kind"), TargetType: r.URL.Query().Get("target_type"),
			Cursor: r.URL.Query().Get("cursor"), Limit: queryLimit(r),
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
		writeRawJSON(w, http.StatusOK, mustJSON(map[string]any{
			"items": items, "next_cursor": reply.GetNextCursor(),
		}))
	}
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
