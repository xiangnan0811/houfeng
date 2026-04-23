package http

import (
	stdhttp "net/http"

	"houfeng/internal/center/http/handlers"
)

type RouterOptions struct {
	Version                string
	WebDistDir             string
	NodesCollectionHandler stdhttp.Handler
	NodeItemHandler        stdhttp.Handler
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
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}
