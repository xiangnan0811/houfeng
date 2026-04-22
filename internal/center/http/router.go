package centerhttp

import (
	"net/http"

	"houfeng/internal/center/http/handlers"
)

type RouterOptions struct {
	Version    string
	WebDistDir string
}

func New(opts RouterOptions) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))
	return mux
}
