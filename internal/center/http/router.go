package http

import (
	stdhttp "net/http"
	"strings"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

type RouterOptions struct {
	Version                         string
	WebDistDir                      string
	DashboardHandler                stdhttp.Handler
	EventsHandler                   stdhttp.Handler
	IncidentsHandler                stdhttp.Handler
	SettingsHandler                 stdhttp.Handler
	AssetServicesCollectionHandler  stdhttp.Handler
	ProvidersCollectionHandler      stdhttp.Handler
	ProviderItemHandler             stdhttp.Handler
	VPSCollectionHandler            stdhttp.Handler
	VPSItemHandler                  stdhttp.Handler
	VPSNodesHandler                 stdhttp.Handler
	VPSLinkNodeHandler              stdhttp.Handler
	VPSUnlinkNodeHandler            stdhttp.Handler
	VPSTimelineHandler              stdhttp.Handler
	VPSExperienceLogsHandler        stdhttp.Handler
	VPSServicesHandler              stdhttp.Handler
	SubscriptionsCollectionHandler  stdhttp.Handler
	SubscriptionItemHandler         stdhttp.Handler
	NodesCollectionHandler          stdhttp.Handler
	NodeItemHandler                 stdhttp.Handler
	NodeVPSHandler                  stdhttp.Handler
	NodeRuntimeFactsHandler         stdhttp.Handler
	NodeRuntimeControlHandler       stdhttp.Handler
	NodeLifecycleControlHandler     stdhttp.Handler
	NodeOnboardingHandler           stdhttp.Handler
	NodeEnrollmentTokenHandler      stdhttp.Handler
	NodeBindingConfirmRebindHandler stdhttp.Handler
	NodeBindingRejectPendingHandler stdhttp.Handler
	NodeBindingResetHandler         stdhttp.Handler
	NodeSparklinesHandler           stdhttp.Handler
	NodeActionsHandler              stdhttp.Handler
	NodeBatchHandler                stdhttp.Handler
	TargetsCollectionHandler        stdhttp.Handler
	TargetItemHandler               stdhttp.Handler
	TargetProbeItemsHandler         stdhttp.Handler
	TargetRuntimeFactsHandler       stdhttp.Handler
	TargetRuntimeControlHandler     stdhttp.Handler
	TargetSparklinesHandler         stdhttp.Handler
	AgentEnrollHandler              stdhttp.Handler
	AgentSyncHandler                stdhttp.Handler
	AuthLoginHandler                stdhttp.Handler
	AuthLogoutHandler               stdhttp.Handler
	AuthMeHandler                   stdhttp.Handler
	AuthChangePasswordHandler       stdhttp.Handler
	// AuthMiddleware wraps each protected /api/* handler. The four auth
	// routes, /api/healthz, /api/agent/*, and the SPA static handler at "/"
	// are intentionally NOT wrapped. Pass nil to disable wrapping (e.g. in
	// tests that don't exercise auth).
	AuthMiddleware func(stdhttp.Handler) stdhttp.Handler
}

func New(opts RouterOptions) stdhttp.Handler {
	mux := stdhttp.NewServeMux()

	protect := func(h stdhttp.Handler) stdhttp.Handler {
		if opts.AuthMiddleware == nil || h == nil {
			return h
		}
		return opts.AuthMiddleware(h)
	}

	mux.Handle("/api/healthz", handlers.Healthz(opts.Version))

	if opts.AuthLoginHandler != nil {
		mux.Handle("/api/auth/login", opts.AuthLoginHandler)
	}
	if opts.AuthLogoutHandler != nil {
		mux.Handle("/api/auth/logout", opts.AuthLogoutHandler)
	}
	if opts.AuthMeHandler != nil {
		mux.Handle("/api/auth/me", opts.AuthMeHandler)
	}
	if opts.AuthChangePasswordHandler != nil {
		mux.Handle("/api/auth/password", opts.AuthChangePasswordHandler)
	}

	if opts.DashboardHandler != nil {
		mux.Handle("/api/dashboard", protect(opts.DashboardHandler))
	}
	if opts.EventsHandler != nil {
		mux.Handle("/api/events", protect(opts.EventsHandler))
	}
	if opts.IncidentsHandler != nil {
		mux.Handle("/api/incidents", protect(opts.IncidentsHandler))
	}
	if opts.SettingsHandler != nil {
		mux.Handle("/api/settings", protect(opts.SettingsHandler))
	}
	if opts.AssetServicesCollectionHandler != nil {
		mux.Handle("/api/services", protect(opts.AssetServicesCollectionHandler))
	}
	if opts.ProvidersCollectionHandler != nil {
		mux.Handle("/api/providers", protect(opts.ProvidersCollectionHandler))
	}
	if opts.ProviderItemHandler != nil {
		mux.Handle("/api/providers/", protect(opts.ProviderItemHandler))
	}
	if opts.VPSCollectionHandler != nil {
		mux.Handle("/api/vps", protect(opts.VPSCollectionHandler))
	}
	if opts.VPSItemHandler != nil || opts.VPSNodesHandler != nil || opts.VPSLinkNodeHandler != nil || opts.VPSUnlinkNodeHandler != nil || opts.VPSTimelineHandler != nil || opts.VPSExperienceLogsHandler != nil || opts.VPSServicesHandler != nil {
		mux.Handle("/api/vps/", protect(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			vpsID, subtree := vpsSubtreePath(r.URL.Path)
			if vpsID == "" {
				stdhttp.NotFound(w, r)
				return
			}

			switch subtree {
			case vpsSubtreeItem:
				if opts.VPSItemHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSItemHandler.ServeHTTP(w, r)
			case vpsSubtreeNodes:
				if opts.VPSNodesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSNodesHandler.ServeHTTP(w, r)
			case vpsSubtreeLinkNode:
				if opts.VPSLinkNodeHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSLinkNodeHandler.ServeHTTP(w, r)
			case vpsSubtreeUnlinkNode:
				if opts.VPSUnlinkNodeHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSUnlinkNodeHandler.ServeHTTP(w, r)
			case vpsSubtreeTimeline:
				if opts.VPSTimelineHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSTimelineHandler.ServeHTTP(w, r)
			case vpsSubtreeExperienceLogs:
				if opts.VPSExperienceLogsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSExperienceLogsHandler.ServeHTTP(w, r)
			case vpsSubtreeServices:
				if opts.VPSServicesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSServicesHandler.ServeHTTP(w, r)
			default:
				stdhttp.NotFound(w, r)
			}
		})))
	}
	if opts.SubscriptionsCollectionHandler != nil {
		mux.Handle("/api/subscriptions", protect(opts.SubscriptionsCollectionHandler))
	}
	if opts.SubscriptionItemHandler != nil {
		mux.Handle("/api/subscriptions/", protect(opts.SubscriptionItemHandler))
	}
	if opts.NodesCollectionHandler != nil {
		mux.Handle("/api/nodes", protect(opts.NodesCollectionHandler))
	}
	if opts.NodeBatchHandler != nil {
		mux.Handle("/api/nodes/batch", protect(opts.NodeBatchHandler))
	}
	if opts.NodeItemHandler != nil || opts.NodeVPSHandler != nil || opts.NodeRuntimeFactsHandler != nil || opts.NodeRuntimeControlHandler != nil || opts.NodeLifecycleControlHandler != nil || opts.NodeOnboardingHandler != nil || opts.NodeEnrollmentTokenHandler != nil || opts.NodeBindingConfirmRebindHandler != nil || opts.NodeBindingRejectPendingHandler != nil || opts.NodeBindingResetHandler != nil || opts.NodeSparklinesHandler != nil || opts.NodeActionsHandler != nil {
		mux.Handle("/api/nodes/", protect(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			nodeID, subtree := nodeSubtreePath(r.URL.Path)
			if nodeID == "" && subtree != nodeSubtreeSparklines {
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
			case nodeSubtreeVPS:
				if opts.NodeVPSHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeVPSHandler.ServeHTTP(w, r)
			case nodeSubtreeRuntimeFacts:
				if opts.NodeRuntimeFactsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeRuntimeFactsHandler.ServeHTTP(w, r)
			case nodeSubtreeRuntimeControl:
				if opts.NodeRuntimeControlHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeRuntimeControlHandler.ServeHTTP(w, r)
			case nodeSubtreeLifecycleControl:
				if opts.NodeLifecycleControlHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeLifecycleControlHandler.ServeHTTP(w, r)
			case nodeSubtreeOnboarding:
				if opts.NodeOnboardingHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeOnboardingHandler.ServeHTTP(w, r)
			case nodeSubtreeEnrollmentToken:
				if opts.NodeEnrollmentTokenHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeEnrollmentTokenHandler.ServeHTTP(w, r)
			case nodeSubtreeBindingConfirmRebind:
				if opts.NodeBindingConfirmRebindHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeBindingConfirmRebindHandler.ServeHTTP(w, r)
			case nodeSubtreeBindingRejectPending:
				if opts.NodeBindingRejectPendingHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeBindingRejectPendingHandler.ServeHTTP(w, r)
			case nodeSubtreeBindingReset:
				if opts.NodeBindingResetHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeBindingResetHandler.ServeHTTP(w, r)
			case nodeSubtreeSparklines:
				if opts.NodeSparklinesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeSparklinesHandler.ServeHTTP(w, r)
			case nodeSubtreeActions:
				if opts.NodeActionsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.NodeActionsHandler.ServeHTTP(w, r)
			default:
				stdhttp.NotFound(w, r)
			}
		})))
	}
	if opts.TargetsCollectionHandler != nil {
		mux.Handle("/api/targets", protect(opts.TargetsCollectionHandler))
	}
	if opts.AgentEnrollHandler != nil {
		mux.Handle(agentapi.EnrollPath, opts.AgentEnrollHandler)
	}
	if opts.AgentSyncHandler != nil {
		mux.Handle(agentapi.SyncPath, opts.AgentSyncHandler)
	}
	if opts.TargetItemHandler != nil || opts.TargetProbeItemsHandler != nil || opts.TargetRuntimeFactsHandler != nil || opts.TargetRuntimeControlHandler != nil || opts.TargetSparklinesHandler != nil {
		mux.Handle("/api/targets/", protect(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			targetID, subtree := targetSubtreePath(r.URL.Path)
			if targetID == "" && subtree != targetSubtreeSparklines {
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
			case targetSubtreeRuntimeControl:
				if opts.TargetRuntimeControlHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.TargetRuntimeControlHandler.ServeHTTP(w, r)
			case targetSubtreeSparklines:
				if opts.TargetSparklinesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.TargetSparklinesHandler.ServeHTTP(w, r)
			default:
				stdhttp.NotFound(w, r)
			}
		})))
	}
	if opts.WebDistDir != "" {
		mux.Handle("/", handlers.SPA(opts.WebDistDir))
	}
	return mux
}

type vpsSubtree string

const (
	vpsSubtreeUnknown        vpsSubtree = ""
	vpsSubtreeItem           vpsSubtree = "item"
	vpsSubtreeNodes          vpsSubtree = "nodes"
	vpsSubtreeLinkNode       vpsSubtree = "link-node"
	vpsSubtreeUnlinkNode     vpsSubtree = "unlink-node"
	vpsSubtreeTimeline       vpsSubtree = "timeline"
	vpsSubtreeExperienceLogs vpsSubtree = "experience-logs"
	vpsSubtreeServices       vpsSubtree = "services"
)

func vpsSubtreePath(path string) (vpsID string, subtree vpsSubtree) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/vps/"), "/")
	if relative == "" {
		return "", vpsSubtreeUnknown
	}

	segments := strings.Split(relative, "/")
	if len(segments) == 0 || segments[0] == "" {
		return "", vpsSubtreeUnknown
	}
	if len(segments) == 1 {
		return segments[0], vpsSubtreeItem
	}
	if len(segments) != 2 {
		return segments[0], vpsSubtreeUnknown
	}
	switch segments[1] {
	case "nodes":
		return segments[0], vpsSubtreeNodes
	case "link-node":
		return segments[0], vpsSubtreeLinkNode
	case "unlink-node":
		return segments[0], vpsSubtreeUnlinkNode
	case "timeline":
		return segments[0], vpsSubtreeTimeline
	case "experience-logs":
		return segments[0], vpsSubtreeExperienceLogs
	case "services":
		return segments[0], vpsSubtreeServices
	default:
		return segments[0], vpsSubtreeUnknown
	}
}

type nodeSubtree string

const (
	nodeSubtreeUnknown              nodeSubtree = ""
	nodeSubtreeItem                 nodeSubtree = "item"
	nodeSubtreeVPS                  nodeSubtree = "vps"
	nodeSubtreeRuntimeFacts         nodeSubtree = "runtime-facts"
	nodeSubtreeRuntimeControl       nodeSubtree = "runtime-control"
	nodeSubtreeLifecycleControl     nodeSubtree = "lifecycle-control"
	nodeSubtreeOnboarding           nodeSubtree = "onboarding"
	nodeSubtreeEnrollmentToken      nodeSubtree = "enrollment-token"
	nodeSubtreeBindingConfirmRebind nodeSubtree = "binding-confirm-rebind"
	nodeSubtreeBindingRejectPending nodeSubtree = "binding-reject-pending"
	nodeSubtreeBindingReset         nodeSubtree = "binding-reset"
	nodeSubtreeSparklines           nodeSubtree = "sparklines"
	nodeSubtreeActions              nodeSubtree = "actions"
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
	if segments[0] == "sparklines" && len(segments) == 1 {
		return "", nodeSubtreeSparklines
	}
	if len(segments) == 1 {
		return segments[0], nodeSubtreeItem
	}
	if segments[1] == "vps" && len(segments) == 2 {
		return segments[0], nodeSubtreeVPS
	}
	if segments[1] == "runtime-facts" && len(segments) == 2 {
		return segments[0], nodeSubtreeRuntimeFacts
	}
	if segments[1] == "runtime" && len(segments) == 3 {
		return segments[0], nodeSubtreeRuntimeControl
	}
	if segments[1] == "lifecycle" && len(segments) == 3 {
		return segments[0], nodeSubtreeLifecycleControl
	}
	if segments[1] == "onboarding" && len(segments) == 2 {
		return segments[0], nodeSubtreeOnboarding
	}
	if segments[1] == "actions" && len(segments) == 2 {
		return segments[0], nodeSubtreeActions
	}
	if segments[1] == "enrollment-token" && len(segments) == 2 {
		return segments[0], nodeSubtreeEnrollmentToken
	}
	if segments[1] == "binding" && len(segments) == 3 {
		switch segments[2] {
		case "confirm-rebind":
			return segments[0], nodeSubtreeBindingConfirmRebind
		case "reject-pending":
			return segments[0], nodeSubtreeBindingRejectPending
		case "reset":
			return segments[0], nodeSubtreeBindingReset
		}
	}
	return segments[0], nodeSubtreeUnknown
}

type targetSubtree string

const (
	targetSubtreeUnknown        targetSubtree = ""
	targetSubtreeItem           targetSubtree = "item"
	targetSubtreeProbeItems     targetSubtree = "probe-items"
	targetSubtreeRuntimeFacts   targetSubtree = "runtime-facts"
	targetSubtreeRuntimeControl targetSubtree = "runtime-control"
	targetSubtreeSparklines     targetSubtree = "sparklines"
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
	if segments[0] == "sparklines" && len(segments) == 1 {
		return "", targetSubtreeSparklines
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
	if segments[1] == "runtime" && len(segments) == 3 {
		return segments[0], targetSubtreeRuntimeControl
	}
	return segments[0], targetSubtreeUnknown
}
