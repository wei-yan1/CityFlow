package handler

import (
	"encoding/json"
	"net/http"

	discoveryclient "cityflow/app/discovery/cmd/rpc/discoveryclient"
	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

// SetMerchantLike is the resource-shaped HTTP entry for the month-scoped
// merchant like. Discovery still owns the fact table and Redis projection;
// Gateway only maps the request onto the existing MERCHANT reaction command.
func SetMerchantLike(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Discovery == nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "discovery service unavailable")
			return
		}
		var request struct {
			Active *bool `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid merchant like body")
			return
		}
		if request.Active == nil {
			httpkit.WriteError(w, http.StatusBadRequest, "merchant like body requires active")
			return
		}
		body, _ := json.Marshal(map[string]any{
			"target_type": "MERCHANT",
			"target_id":   pathvar.Vars(r)["id"],
			"kind":        "LIKE",
			"active":      *request.Active,
		})
		reply, err := s.Discovery.SetReaction(rpcContext(r), &discoveryclient.JsonBody{BodyJson: body})
		if err != nil {
			writeReactionError(w, err)
			return
		}
		writeRawJSON(w, http.StatusOK, reply.GetBodyJson())
	}
}
