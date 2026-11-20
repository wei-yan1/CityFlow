package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cityflow/app/gateway/cmd/api/internal/svc"
	"cityflow/internal/platform/httpkit"

	"github.com/zeromicro/go-zero/rest/pathvar"
)

// UploadedFile serves merchant media written by Discovery. In production this
// should be replaced by object storage/CDN, but the edge is the only public
// origin in local development, so the Gateway serves the file instead of
// exposing the domain service's filesystem.
func UploadedFile(s *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveUpload(s, pathvar.Vars(r)["file"], w, r)
	}
}

func serveUpload(s *svc.ServiceContext, name string, w http.ResponseWriter, r *http.Request) {
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid upload name")
		return
	}
	root := strings.TrimSpace(s.Config.UploadDir)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("CITYFLOW_UPLOAD_DIR"))
	}
	if root == "" {
		root = "uploads"
	}
	path, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		httpkit.WriteError(w, http.StatusInternalServerError, "could not resolve upload")
		return
	}
	if _, err := os.Stat(path); err != nil {
		httpkit.WriteError(w, http.StatusNotFound, "upload not found")
		return
	}
	http.ServeFile(w, r, path)
}
