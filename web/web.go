package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed auth user rider merchant platform coupons assets
var files embed.FS

func AssetsHandler() http.Handler {
	assets, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/assets/", http.FileServer(http.FS(assets)))
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
