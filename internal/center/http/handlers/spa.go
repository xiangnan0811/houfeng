package handlers

import (
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func SPA(webDistDir string) http.Handler {
	indexPath := filepath.Join(webDistDir, "index.html")
	webDistRoot, err := filepath.Abs(webDistDir)
	if err != nil {
		webDistRoot = filepath.Clean(webDistDir)
	}
	if resolvedRoot, err := filepath.EvalSymlinks(webDistRoot); err == nil {
		webDistRoot = resolvedRoot
	}

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

		filePath, ok := spaFilePath(webDistRoot, r.URL.EscapedPath())
		if !ok {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(filePath); err == nil && info.Mode().IsRegular() {
			http.ServeFile(w, r, filePath)
			return
		}

		http.ServeFile(w, r, indexPath)
	})
}

func spaFilePath(webDistRoot, escapedPath string) (string, bool) {
	if escapedPath == "" {
		return "", false
	}
	unescapedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return "", false
	}
	if hasParentSegment(unescapedPath) {
		return "", false
	}
	cleanPath := path.Clean("/" + unescapedPath)
	relPath := strings.TrimPrefix(cleanPath, "/")
	if relPath == "" {
		return "", false
	}

	candidate := filepath.Join(webDistRoot, filepath.FromSlash(relPath))
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return candidate, true
	}
	if !isPathWithinRoot(webDistRoot, resolvedCandidate) {
		return "", false
	}
	return resolvedCandidate, true
}

func hasParentSegment(requestPath string) bool {
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func isPathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
