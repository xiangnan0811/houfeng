package handlers

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func SPA(webDistDir string) http.Handler {
	indexPath := filepath.Join(webDistDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		if r.URL.Path == "/" {
			http.ServeFile(w, r, indexPath)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		relPath := strings.TrimPrefix(cleanPath, "/")
		filePath := filepath.Join(webDistDir, filepath.FromSlash(relPath))

		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, filePath)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}
