package http

import (
	stdhttp "net/http"
	"strings"

	"houfeng/internal/center/http/handlers"
)

type RouterOptions struct {
	Version                  string
	WebDistDir               string
	NodesCollectionHandler   stdhttp.Handler
	NodeItemHandler          stdhttp.Handler
	TargetsCollectionHandler stdhttp.Handler
	TargetItemHandler        stdhttp.Handler
	TargetProbeItemsHandler  stdhttp.Handler
}

func New(opts RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))
	if opts.NodesCollectionHandler != nil {
		mux.Handle("/api/nodes", opts.NodesCollectionHandler)
	}
	if opts.NodeItemHandler != nil {
		mux.Handle("/api/nodes/", opts.NodeItemHandler)
	}
	if opts.TargetsCollectionHandler != nil {
		mux.Handle("/api/targets", opts.TargetsCollectionHandler)
	}
	if opts.TargetItemHandler != nil || opts.TargetProbeItemsHandler != nil {
		mux.Handle("/api/targets/", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			trimmedPath := strings.TrimSuffix(r.URL.Path, "/")
			if strings.HasSuffix(trimmedPath, "/probe-items") {
				if opts.TargetProbeItemsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.TargetProbeItemsHandler.ServeHTTP(w, r)
				return
			}
			if opts.TargetItemHandler == nil {
				stdhttp.NotFound(w, r)
				return
			}
			opts.TargetItemHandler.ServeHTTP(w, r)
		}))
	}
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}
