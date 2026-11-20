package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/httpkit"
)

// TrackingStream proxies the browser WebSocket upgrade to the Tracking-owned
// stream. Tracking validates the bearer carried in Sec-WebSocket-Protocol and
// decides whether the caller may observe the task; Gateway only routes.
func TrackingStream(s *svc.ServiceContext) http.HandlerFunc {
	target, err := url.Parse(s.Config.TrackingStreamURL)
	if err != nil {
		panic(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = strings.Replace(req.URL.Path, "/api/v1/tasks/", "/internal/tasks/", 1)
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if r.Context().Err() != nil {
			return
		}
		httpkit.WriteError(w, http.StatusBadGateway, "tracking stream unavailable")
	}
	return proxy.ServeHTTP
}
