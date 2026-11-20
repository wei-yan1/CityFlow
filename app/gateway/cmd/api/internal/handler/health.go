package handler

import (
	"context"
	"net/http"
	"time"

	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/httpkit"
)

func Health(w http.ResponseWriter, _ *http.Request) {
	httpkit.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func Ready(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.Ready(ctx); err != nil {
			httpkit.WriteError(w, http.StatusServiceUnavailable, "gateway dependencies unavailable")
			return
		}
		httpkit.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
