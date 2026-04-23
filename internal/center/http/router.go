package http

import (
	stdhttp "net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/http/handlers"
)

type RouterOptions struct {
	Version    string
	WebDistDir string
	DB         *pgxpool.Pool
}

func New(opts RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}
