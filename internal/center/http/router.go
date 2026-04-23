package http

import (
	stdhttp "net/http"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/store"
)

type RouterOptions struct {
	Version    string
	WebDistDir string
	Nodes      store.NodeRepository
}

func New(opts RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))
	if opts.Nodes != nil {
		mux.Handle("/api/nodes", handlers.NodesCollection(opts.Nodes))
		mux.Handle("/api/nodes/", handlers.NodeItem(opts.Nodes))
	}
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}
