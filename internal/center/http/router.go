package http

import (
	stdhttp "net/http"

	"houfeng/internal/center/http/handlers"
)

type RouterOptions struct {
	Version    string
	WebDistDir string
}

func New(opts RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}
