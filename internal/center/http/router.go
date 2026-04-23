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
			targetID, subtree := targetSubtreePath(r.URL.Path)
			if targetID == "" {
				stdhttp.NotFound(w, r)
				return
			}

			switch subtree {
			case targetSubtreeItem:
				if opts.TargetItemHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.TargetItemHandler.ServeHTTP(w, r)
			case targetSubtreeProbeItems:
				if opts.TargetProbeItemsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.TargetProbeItemsHandler.ServeHTTP(w, r)
			default:
				stdhttp.NotFound(w, r)
			}
		}))
	}
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}

type targetSubtree string

const (
	targetSubtreeUnknown    targetSubtree = ""
	targetSubtreeItem       targetSubtree = "item"
	targetSubtreeProbeItems targetSubtree = "probe-items"
)

func targetSubtreePath(path string) (targetID string, subtree targetSubtree) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/targets/"), "/")
	if relative == "" {
		return "", targetSubtreeUnknown
	}

	segments := strings.Split(relative, "/")
	if len(segments) == 0 || segments[0] == "" {
		return "", targetSubtreeUnknown
	}
	if len(segments) == 1 {
		return segments[0], targetSubtreeItem
	}
	if segments[1] == "probe-items" {
		return segments[0], targetSubtreeProbeItems
	}
	return segments[0], targetSubtreeUnknown
}
