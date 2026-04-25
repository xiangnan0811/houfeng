package http

import (
	stdhttp "net/http"
	"strings"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

type RouterOptions struct {
	Version                   string
	WebDistDir                string
	DashboardHandler          stdhttp.Handler
	EventsHandler             stdhttp.Handler
	IncidentsHandler          stdhttp.Handler
	NodesCollectionHandler    stdhttp.Handler
	NodeItemHandler           stdhttp.Handler
	NodeRuntimeFactsHandler   stdhttp.Handler
	TargetsCollectionHandler  stdhttp.Handler
	TargetItemHandler         stdhttp.Handler
	TargetProbeItemsHandler   stdhttp.Handler
	TargetRuntimeFactsHandler stdhttp.Handler
	AgentEnrollHandler        stdhttp.Handler
	AgentSyncHandler          stdhttp.Handler
}

func New(opts RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()
	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))
	if opts.DashboardHandler != nil {
		mux.Handle("/api/dashboard", opts.DashboardHandler)
	}
	if opts.EventsHandler != nil {
		mux.Handle("/api/events", opts.EventsHandler)
	}
	if opts.IncidentsHandler != nil {
		mux.Handle("/api/incidents", opts.IncidentsHandler)
	}
	if opts.NodesCollectionHandler != nil {
		mux.Handle("/api/nodes", opts.NodesCollectionHandler)
	}
	if opts.NodeItemHandler != nil || opts.NodeRuntimeFactsHandler != nil {
		mux.Handle("/api/nodes/", stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			nodeID, subtree := nodeSubtreePath(r.URL.Path)
			if nodeID == "" {
				stdhttp.NotFound(w, r)
				return
			}

			switch subtree {
			case nodeSubtreeItem:
				if opts.NodeItemHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeItemHandler.ServeHTTP(w, r)
			case nodeSubtreeRuntimeFacts:
				if opts.NodeRuntimeFactsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeRuntimeFactsHandler.ServeHTTP(w, r)
			default:
				stdhttp.NotFound(w, r)
			}
		}))
	}
	if opts.TargetsCollectionHandler != nil {
		mux.Handle("/api/targets", opts.TargetsCollectionHandler)
	}
	if opts.AgentEnrollHandler != nil {
		mux.Handle(agentapi.EnrollPath, opts.AgentEnrollHandler)
	}
	if opts.AgentSyncHandler != nil {
		mux.Handle(agentapi.SyncPath, opts.AgentSyncHandler)
	}
	if opts.TargetItemHandler != nil || opts.TargetProbeItemsHandler != nil || opts.TargetRuntimeFactsHandler != nil {
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
			case targetSubtreeRuntimeFacts:
				if opts.TargetRuntimeFactsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.TargetRuntimeFactsHandler.ServeHTTP(w, r)
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

type nodeSubtree string

const (
	nodeSubtreeUnknown      nodeSubtree = ""
	nodeSubtreeItem         nodeSubtree = "item"
	nodeSubtreeRuntimeFacts nodeSubtree = "runtime-facts"
)

func nodeSubtreePath(path string) (nodeID string, subtree nodeSubtree) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/nodes/"), "/")
	if relative == "" {
		return "", nodeSubtreeUnknown
	}

	segments := strings.Split(relative, "/")
	if len(segments) == 0 || segments[0] == "" {
		return "", nodeSubtreeUnknown
	}
	if len(segments) == 1 {
		return segments[0], nodeSubtreeItem
	}
	if segments[1] == "runtime-facts" && len(segments) == 2 {
		return segments[0], nodeSubtreeRuntimeFacts
	}
	return segments[0], nodeSubtreeUnknown
}

type targetSubtree string

const (
	targetSubtreeUnknown      targetSubtree = ""
	targetSubtreeItem         targetSubtree = "item"
	targetSubtreeProbeItems   targetSubtree = "probe-items"
	targetSubtreeRuntimeFacts targetSubtree = "runtime-facts"
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
	if segments[1] == "runtime-facts" && len(segments) == 2 {
		return segments[0], targetSubtreeRuntimeFacts
	}
	return segments[0], targetSubtreeUnknown
}
