package web

import (
	"embed"
	"io/fs"
	"net/http"
)

// The web tree is one application with role workspaces. Shared code lives in
// shared/, never in a per-role copy.
//
//go:embed auth user rider merchant platform shared assets
var files embed.FS

func AssetsHandler() http.Handler {
	return staticHandler("assets", "/assets/")
}

// SharedAssetsHandler serves the API client, auth helpers, map helpers and
// common styles that every role workspace uses.
func SharedAssetsHandler() http.Handler {
	return staticHandler("shared", "/assets/shared/")
}

func UserCouponsAssetsHandler() http.Handler {
	return staticHandler("user/coupons/assets", "/user/coupons/assets/")
}

func staticHandler(dir, prefix string) http.Handler {
	sub, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return http.StripPrefix(prefix, http.FileServer(http.FS(sub)))
}

func ServePage(w http.ResponseWriter, name string) {
	content, err := files.ReadFile(name)
	if err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(content)
}
