package handler

import (
	"net/http"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/httpkit"
	"cityflow/web"
)

var staticPages = map[string]string{
	"/":                   "auth/login.html",
	"/login":              "auth/login.html",
	"/register":           "auth/login.html",
	"/forgot-password":    "auth/login.html",
	"/user":               "user/index.html",
	"/coupons":            "user/coupons/index.html",
	"/coupons/mine":       "user/coupons/mine.html",
	"/rider":              "rider/index.html",
	"/rider/register":     "rider/register.html",
	"/merchant":           "merchant/index.html",
	"/admin":              "platform/index.html",
	"/admin/redeem-codes": "platform/redeem-codes.html",
}

// Pages is the edge's non-API fallback. The web tree is embedded in the
// Gateway binary, so serving a page never depends on the retired Gin process.
// API paths are explicitly rejected and are never proxied anywhere.
func Pages(s *svc.ServiceContext) http.HandlerFunc {
	assets := web.AssetsHandler()
	shared := web.SharedAssetsHandler()
	couponAssets := web.UserCouponsAssetsHandler()
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
		case strings.HasPrefix(path, "/tasks/") && strings.TrimPrefix(path, "/tasks/") != "":
			web.ServePage(w, "user/task.html")
			return
		case strings.HasPrefix(path, "/riders/") && strings.TrimPrefix(path, "/riders/") != "":
			web.ServePage(w, "user/rider-profile.html")
			return
		case strings.HasPrefix(path, "/assets/shared/"):
			shared.ServeHTTP(w, r)
			return
		case strings.HasPrefix(path, "/user/coupons/assets/"):
			couponAssets.ServeHTTP(w, r)
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
