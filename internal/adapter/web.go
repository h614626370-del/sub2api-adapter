package adapter

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed web/dist
var adminDist embed.FS

func serveAdminAsset(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(adminDist, "web/dist")
	if err != nil {
		http.Error(w, "admin assets unavailable", http.StatusInternalServerError)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/admin/")
	if path == "" || r.URL.Path == "/admin" || strings.HasPrefix(path, "api/") {
		path = "index.html"
	}
	if _, err := fs.Stat(sub, path); err != nil {
		path = "index.html"
	}
	raw, err := fs.ReadFile(sub, path)
	if err != nil {
		http.Error(w, "admin asset not found", http.StatusNotFound)
		return
	}
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	_, _ = w.Write(raw)
}
