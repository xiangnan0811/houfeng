package http

import (
	stdhttp "net/http"
	"strings"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

type RouterOptions struct {
	Version                                       string
	WebDistDir                                    string
	DashboardHandler                              stdhttp.Handler
	EventsHandler                                 stdhttp.Handler
	IncidentsHandler                              stdhttp.Handler
	SettingsHandler                               stdhttp.Handler
	AssetDomainsCollectionHandler                 stdhttp.Handler
	AssetServicesCollectionHandler                stdhttp.Handler
	ProvidersCollectionHandler                    stdhttp.Handler
	ProviderItemHandler                           stdhttp.Handler
	VPSCollectionHandler                          stdhttp.Handler
	VPSItemHandler                                stdhttp.Handler
	VPSMonitoringInstancesHandler                 stdhttp.Handler
	VPSSubscriptionsHandler                       stdhttp.Handler
	VPSLinkMonitoringInstanceHandler              stdhttp.Handler
	VPSUnlinkMonitoringInstanceHandler            stdhttp.Handler
	VPSTimelineHandler                            stdhttp.Handler
	VPSExperienceLogsHandler                      stdhttp.Handler
	VPSDomainsHandler                             stdhttp.Handler
	VPSServicesHandler                            stdhttp.Handler
	VPSCancellationPreviewHandler                 stdhttp.Handler
	VPSCancellationHandler                        stdhttp.Handler
	AssetContextMonitoringInstancesHandler        stdhttp.Handler
	AssetContextTargetsHandler                    stdhttp.Handler
	SubscriptionsCollectionHandler                stdhttp.Handler
	SubscriptionItemHandler                       stdhttp.Handler
	MonitoringInstancesCollectionHandler          stdhttp.Handler
	MonitoringInstanceItemHandler                 stdhttp.Handler
	MonitoringInstanceVPSHandler                  stdhttp.Handler
	MonitoringInstanceRuntimeFactsHandler         stdhttp.Handler
	MonitoringInstanceRuntimeControlHandler       stdhttp.Handler
	MonitoringInstanceOnboardingHandler           stdhttp.Handler
	MonitoringInstanceEnrollmentTokenHandler      stdhttp.Handler
	MonitoringInstanceInstallCommandHandler       stdhttp.Handler
	MonitoringInstanceBindingConfirmRebindHandler stdhttp.Handler
	MonitoringInstanceBindingRejectPendingHandler stdhttp.Handler
	MonitoringInstanceBindingResetHandler         stdhttp.Handler
	MonitoringInstanceSparklinesHandler           stdhttp.Handler
	MonitoringInstanceActionsHandler              stdhttp.Handler
	MonitoringInstanceBatchHandler                stdhttp.Handler
	TargetsCollectionHandler                      stdhttp.Handler
	TargetItemHandler                             stdhttp.Handler
	TargetProbeItemsHandler                       stdhttp.Handler
	TargetRuntimeFactsHandler                     stdhttp.Handler
	TargetRuntimeControlHandler                   stdhttp.Handler
	TargetSparklinesHandler                       stdhttp.Handler
	AgentEnrollHandler                            stdhttp.Handler
	AgentSyncHandler                              stdhttp.Handler
	InstallerScriptHandler                        stdhttp.Handler
	AuthLoginHandler                              stdhttp.Handler
	AuthLogoutHandler                             stdhttp.Handler
	AuthMeHandler                                 stdhttp.Handler
	AuthChangePasswordHandler                     stdhttp.Handler
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
	if opts.AssetDomainsCollectionHandler != nil {
		mux.Handle("/api/domains", protect(opts.AssetDomainsCollectionHandler))
	}
	if opts.AssetServicesCollectionHandler != nil {
		mux.Handle("/api/services", protect(opts.AssetServicesCollectionHandler))
	}
	if opts.AssetContextMonitoringInstancesHandler != nil {
		mux.Handle("/api/asset-context/monitoring-instances", protect(opts.AssetContextMonitoringInstancesHandler))
	}
	if opts.AssetContextTargetsHandler != nil {
		mux.Handle("/api/asset-context/targets", protect(opts.AssetContextTargetsHandler))
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
	if opts.VPSItemHandler != nil || opts.VPSMonitoringInstancesHandler != nil || opts.VPSSubscriptionsHandler != nil || opts.VPSLinkMonitoringInstanceHandler != nil || opts.VPSUnlinkMonitoringInstanceHandler != nil || opts.VPSTimelineHandler != nil || opts.VPSExperienceLogsHandler != nil || opts.VPSDomainsHandler != nil || opts.VPSServicesHandler != nil || opts.VPSCancellationPreviewHandler != nil || opts.VPSCancellationHandler != nil {
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
			case vpsSubtreeMonitoringInstances:
				if opts.VPSMonitoringInstancesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSMonitoringInstancesHandler.ServeHTTP(w, r)
			case vpsSubtreeSubscriptions:
				if opts.VPSSubscriptionsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSSubscriptionsHandler.ServeHTTP(w, r)
			case vpsSubtreeLinkMonitoringInstance:
				if opts.VPSLinkMonitoringInstanceHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSLinkMonitoringInstanceHandler.ServeHTTP(w, r)
			case vpsSubtreeUnlinkMonitoringInstance:
				if opts.VPSUnlinkMonitoringInstanceHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSUnlinkMonitoringInstanceHandler.ServeHTTP(w, r)
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
			case vpsSubtreeDomains:
				if opts.VPSDomainsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSDomainsHandler.ServeHTTP(w, r)
			case vpsSubtreeServices:
				if opts.VPSServicesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSServicesHandler.ServeHTTP(w, r)
			case vpsSubtreeCancellationPreview:
				if opts.VPSCancellationPreviewHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSCancellationPreviewHandler.ServeHTTP(w, r)
			case vpsSubtreeCancellation:
				if opts.VPSCancellationHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.VPSCancellationHandler.ServeHTTP(w, r)
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
	if opts.MonitoringInstancesCollectionHandler != nil {
		mux.Handle("/api/monitoring-instances", protect(opts.MonitoringInstancesCollectionHandler))
	}
	if opts.MonitoringInstanceBatchHandler != nil {
		mux.Handle("/api/monitoring-instances/batch", protect(opts.MonitoringInstanceBatchHandler))
	}
	if opts.MonitoringInstanceItemHandler != nil || opts.MonitoringInstanceVPSHandler != nil || opts.MonitoringInstanceRuntimeFactsHandler != nil || opts.MonitoringInstanceRuntimeControlHandler != nil || opts.MonitoringInstanceOnboardingHandler != nil || opts.MonitoringInstanceEnrollmentTokenHandler != nil || opts.MonitoringInstanceInstallCommandHandler != nil || opts.MonitoringInstanceBindingConfirmRebindHandler != nil || opts.MonitoringInstanceBindingRejectPendingHandler != nil || opts.MonitoringInstanceBindingResetHandler != nil || opts.MonitoringInstanceSparklinesHandler != nil || opts.MonitoringInstanceActionsHandler != nil {
		mux.Handle("/api/monitoring-instances/", protect(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			monitoringInstanceID, subtree := monitoringInstanceSubtreePath(r.URL.Path)
			if monitoringInstanceID == "" && subtree != monitoringInstanceSubtreeSparklines {
				stdhttp.NotFound(w, r)
				return
			}

			switch subtree {
			case monitoringInstanceSubtreeItem:
				if opts.MonitoringInstanceItemHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceItemHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeVPS:
				if opts.MonitoringInstanceVPSHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceVPSHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeRuntimeFacts:
				if opts.MonitoringInstanceRuntimeFactsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceRuntimeFactsHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeRuntimeControl:
				if opts.MonitoringInstanceRuntimeControlHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceRuntimeControlHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeOnboarding:
				if opts.MonitoringInstanceOnboardingHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceOnboardingHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeEnrollmentToken:
				if opts.MonitoringInstanceEnrollmentTokenHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceEnrollmentTokenHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeInstallCommand:
				if opts.MonitoringInstanceInstallCommandHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceInstallCommandHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeBindingConfirmRebind:
				if opts.MonitoringInstanceBindingConfirmRebindHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceBindingConfirmRebindHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeBindingRejectPending:
				if opts.MonitoringInstanceBindingRejectPendingHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceBindingRejectPendingHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeBindingReset:
				if opts.MonitoringInstanceBindingResetHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceBindingResetHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeSparklines:
				if opts.MonitoringInstanceSparklinesHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceSparklinesHandler.ServeHTTP(w, r)
			case monitoringInstanceSubtreeActions:
				if opts.MonitoringInstanceActionsHandler == nil {
					stdhttp.NotFound(w, r)
					return
				}
				opts.MonitoringInstanceActionsHandler.ServeHTTP(w, r)
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
	if opts.InstallerScriptHandler != nil {
		mux.Handle(agentapi.InstallScriptPath, opts.InstallerScriptHandler)
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
	vpsSubtreeUnknown                  vpsSubtree = ""
	vpsSubtreeItem                     vpsSubtree = "item"
	vpsSubtreeMonitoringInstances      vpsSubtree = "monitoring-instances"
	vpsSubtreeSubscriptions            vpsSubtree = "subscriptions"
	vpsSubtreeLinkMonitoringInstance   vpsSubtree = "link-monitoring-instance"
	vpsSubtreeUnlinkMonitoringInstance vpsSubtree = "unlink-monitoring-instance"
	vpsSubtreeTimeline                 vpsSubtree = "timeline"
	vpsSubtreeExperienceLogs           vpsSubtree = "experience-logs"
	vpsSubtreeDomains                  vpsSubtree = "domains"
	vpsSubtreeServices                 vpsSubtree = "services"
	vpsSubtreeCancellationPreview      vpsSubtree = "cancellation-preview"
	vpsSubtreeCancellation             vpsSubtree = "cancellation"
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
	case "monitoring-instances":
		return segments[0], vpsSubtreeMonitoringInstances
	case "subscriptions":
		return segments[0], vpsSubtreeSubscriptions
	case "link-monitoring-instance":
		return segments[0], vpsSubtreeLinkMonitoringInstance
	case "unlink-monitoring-instance":
		return segments[0], vpsSubtreeUnlinkMonitoringInstance
	case "timeline":
		return segments[0], vpsSubtreeTimeline
	case "experience-logs":
		return segments[0], vpsSubtreeExperienceLogs
	case "domains":
		return segments[0], vpsSubtreeDomains
	case "services":
		return segments[0], vpsSubtreeServices
	case "cancellation-preview":
		return segments[0], vpsSubtreeCancellationPreview
	case "cancellation":
		return segments[0], vpsSubtreeCancellation
	default:
		return segments[0], vpsSubtreeUnknown
	}
}

type monitoringInstanceSubtree string

const (
	monitoringInstanceSubtreeUnknown              monitoringInstanceSubtree = ""
	monitoringInstanceSubtreeItem                 monitoringInstanceSubtree = "item"
	monitoringInstanceSubtreeVPS                  monitoringInstanceSubtree = "vps"
	monitoringInstanceSubtreeRuntimeFacts         monitoringInstanceSubtree = "runtime-facts"
	monitoringInstanceSubtreeRuntimeControl       monitoringInstanceSubtree = "runtime-control"
	monitoringInstanceSubtreeOnboarding           monitoringInstanceSubtree = "onboarding"
	monitoringInstanceSubtreeEnrollmentToken      monitoringInstanceSubtree = "enrollment-token"
	monitoringInstanceSubtreeInstallCommand       monitoringInstanceSubtree = "install-command"
	monitoringInstanceSubtreeBindingConfirmRebind monitoringInstanceSubtree = "binding-confirm-rebind"
	monitoringInstanceSubtreeBindingRejectPending monitoringInstanceSubtree = "binding-reject-pending"
	monitoringInstanceSubtreeBindingReset         monitoringInstanceSubtree = "binding-reset"
	monitoringInstanceSubtreeSparklines           monitoringInstanceSubtree = "sparklines"
	monitoringInstanceSubtreeActions              monitoringInstanceSubtree = "actions"
)

func monitoringInstanceSubtreePath(path string) (monitoringInstanceID string, subtree monitoringInstanceSubtree) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/")
	if relative == "" {
		return "", monitoringInstanceSubtreeUnknown
	}

	segments := strings.Split(relative, "/")
	if len(segments) == 0 || segments[0] == "" {
		return "", monitoringInstanceSubtreeUnknown
	}
	if segments[0] == "sparklines" && len(segments) == 1 {
		return "", monitoringInstanceSubtreeSparklines
	}
	if len(segments) == 1 {
		return segments[0], monitoringInstanceSubtreeItem
	}
	if segments[1] == "vps" && len(segments) == 2 {
		return segments[0], monitoringInstanceSubtreeVPS
	}
	if segments[1] == "runtime-facts" && len(segments) == 2 {
		return segments[0], monitoringInstanceSubtreeRuntimeFacts
	}
	if segments[1] == "runtime" && len(segments) == 3 {
		return segments[0], monitoringInstanceSubtreeRuntimeControl
	}
	if segments[1] == "onboarding" && len(segments) == 2 {
		return segments[0], monitoringInstanceSubtreeOnboarding
	}
	if segments[1] == "actions" && len(segments) == 2 {
		return segments[0], monitoringInstanceSubtreeActions
	}
	if segments[1] == "enrollment-token" && len(segments) == 2 {
		return segments[0], monitoringInstanceSubtreeEnrollmentToken
	}
	if segments[1] == "install-command" && len(segments) == 2 {
		return segments[0], monitoringInstanceSubtreeInstallCommand
	}
	if segments[1] == "binding" && len(segments) == 3 {
		switch segments[2] {
		case "confirm-rebind":
			return segments[0], monitoringInstanceSubtreeBindingConfirmRebind
		case "reject-pending":
			return segments[0], monitoringInstanceSubtreeBindingRejectPending
		case "reset":
			return segments[0], monitoringInstanceSubtreeBindingReset
		}
	}
	return segments[0], monitoringInstanceSubtreeUnknown
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
