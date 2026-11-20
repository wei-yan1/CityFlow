package handler

import (
	"net/http"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/httpkit"
	"cityflow/web"
)

var staticPages = map[string]string{
	"/":                "auth/login.html",
	"/login":           "auth/login.html",
	"/register":        "auth/login.html",
	"/forgot-password": "auth/login.html",
	"/user":            "user/index.html",
	"/coupons":         "coupons/index.html",
	"/coupons/mine":    "coupons/mine.html",
	"/rider":           "rider/index.html",
	"/rider/register":  "rider/register.html",
	"/merchant":        "merchant/index.html",
	"/admin":           "platform/index.html",
}

// Pages is the edge's non-API fallback. The web tree is embedded in the
// Gateway binary, so serving a page never depends on the retired Gin process.
// API paths are explicitly rejected and are never proxied anywhere.
func Pages(s *svc.ServiceContext) http.HandlerFunc {
	assets := web.AssetsHandler()
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api" || strings.HasPrefix(path, "/api/") {
			httpkit.WriteError(w, http.StatusNotFound, "api route not found")
			return
		}
		if page, ok := staticPages[path]; ok {
			web.ServePage(w, page)
			return
		}
		switch {
		case strings.HasPrefix(path, "/orders/") && strings.TrimPrefix(path, "/orders/") != "":
			web.ServePage(w, "user/order.html")
			return
		case strings.HasPrefix(path, "/riders/") && strings.TrimPrefix(path, "/riders/") != "":
			web.ServePage(w, "user/rider-profile.html")
			return
		case strings.HasPrefix(path, "/assets/"):
			assets.ServeHTTP(w, r)
			return
		case strings.HasPrefix(path, "/uploads/"):
			serveUpload(s, strings.TrimPrefix(path, "/uploads/"), w, r)
			return
		}
		httpkit.WriteError(w, http.StatusNotFound, "page not found")
	}
}
