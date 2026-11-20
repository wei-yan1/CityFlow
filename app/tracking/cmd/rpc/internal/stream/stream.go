// Package stream exposes the Tracking-owned WebSocket location stream as an
// internal HTTP endpoint. Gateway proxies the browser upgrade here; the bearer
// travels in the Sec-WebSocket-Protocol subprotocol because browsers cannot set
// Authorization on a WebSocket handshake.
package stream

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"cityflow/app/tracking/cmd/rpc/internal/svc"
	"cityflow/internal/domain"
	"cityflow/internal/domain/location"
	"cityflow/internal/platform/rpcctx"
	"cityflow/internal/security"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{Subprotocols: []string{"cityflow"}, HandshakeTimeout: 5 * time.Second}

// Handler returns the internal WSS handler for /internal/tasks/{id}/location/stream.
func Handler(s *svc.ServiceContext) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := taskIDFromPath(r.URL.Path)
		if id == "" {
			http.Error(w, "missing task id", http.StatusBadRequest)
			return
		}
		token := bearerFromProtocol(r.Header.Get("Sec-WebSocket-Protocol"))
		if token == "" {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		claims, err := security.VerifyJWT(token, s.Secret, time.Now())
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		ctx := rpcctx.WithBearer(r.Context(), token)
		if active, err := s.Status.IsActive(ctx, claims.Subject, token); err != nil || !active {
			http.Error(w, "account inactive", http.StatusUnauthorized)
			return
		}
		riderID, err := s.Tracking.CanTrack(ctx, id, *claims)
		if err != nil {
			writeStreamError(w, err)
			return
		}
		updates, cancel := s.Tracking.Subscribe(riderID)
		defer cancel()
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(1024)
		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		send := func(kind string, p *location.Position) bool {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			return conn.WriteJSON(map[string]any{"type": kind, "task_id": id, "position": p}) == nil
		}
		snapshot := func() bool {
			if time.Now().Unix() >= claims.Expires {
				return false
			}
			if active, err := s.Status.IsActive(ctx, claims.Subject, token); err != nil || !active {
				return false
			}
			p, err := s.Tracking.Snapshot(ctx, id, *claims)
			if err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					if _, allowedErr := s.Tracking.CanTrack(ctx, id, *claims); allowedErr != nil {
						return false
					}
					return send("rider.location_unavailable", nil)
				}
				return false
			}
			return send("rider.location_changed", p)
		}
		if !snapshot() {
			return
		}
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case _, ok := <-updates:
				if !ok || !snapshot() {
					return
				}
			case <-ticker.C:
				if !snapshot() {
					return
				}
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			}
		}
	})
}

func taskIDFromPath(p string) string {
	const prefix = "/internal/tasks/"
	const suffix = "/location/stream"
	if !strings.HasPrefix(p, prefix) || !strings.HasSuffix(p, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(p, prefix), suffix)
}

func bearerFromProtocol(header string) string {
	for _, protocol := range strings.Split(header, ",") {
		protocol = strings.TrimSpace(protocol)
		if strings.HasPrefix(protocol, "bearer.") {
			return strings.TrimPrefix(protocol, "bearer.")
		}
	}
	return ""
}

func writeStreamError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "location unavailable", http.StatusServiceUnavailable)
	}
}
