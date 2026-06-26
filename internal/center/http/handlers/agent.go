package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

type AgentEnrollService interface {
	EnrollMonitoringInstance(ctx context.Context, input enrollment.EnrollInput) (enrollment.EnrollResult, error)
}

type AgentSyncService interface {
	SyncBatch(ctx context.Context, batch syncing.Batch) (syncing.Result, error)
}

const (
	agentSyncMaxItems          = 256
	agentIdentityMaxBytes      = 256
	agentSecretMaxBytes        = 512
	agentErrorSummaryMaxBytes  = 2048
	agentCommandOutputMaxBytes = 128 << 10
	agentRawJSONMaxBytes       = 128 << 10
)

type AgentEndpointOptions struct {
	TrustedProxies []string
	RateLimit      AgentRateLimitOptions
	Now            func() time.Time
}

type AgentRateLimitOptions struct {
	MaxRequestsByIP   int
	MaxRequestsGlobal int
	MaxTrackedKeys    int
	Window            time.Duration
	SweepInterval     time.Duration
	MaxSyncInflight   int
}

type agentRequestLimiter struct {
	mu            sync.Mutex
	now           func() time.Time
	window        time.Duration
	sweepInterval time.Duration
	nextSweep     time.Time
	maxIP         int
	maxGlobal     int
	maxKeys       int
	byIP          map[string][]time.Time
	global        []time.Time
	resolver      trustedProxyResolver
}

func defaultAgentRateLimitOptions() AgentRateLimitOptions {
	return AgentRateLimitOptions{
		MaxRequestsByIP:   120,
		MaxRequestsGlobal: 1000,
		MaxTrackedKeys:    10000,
		Window:            time.Minute,
		SweepInterval:     time.Minute,
		MaxSyncInflight:   32,
	}
}

func newAgentRequestLimiter(opts AgentEndpointOptions) *agentRequestLimiter {
	if opts.RateLimit == (AgentRateLimitOptions{}) {
		opts.RateLimit = defaultAgentRateLimitOptions()
	}
	if opts.RateLimit.Window <= 0 {
		opts.RateLimit.Window = time.Minute
	}
	if opts.RateLimit.MaxTrackedKeys <= 0 {
		opts.RateLimit.MaxTrackedKeys = 10000
	}
	if opts.RateLimit.SweepInterval <= 0 {
		opts.RateLimit.SweepInterval = opts.RateLimit.Window
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &agentRequestLimiter{
		now:           now,
		window:        opts.RateLimit.Window,
		sweepInterval: opts.RateLimit.SweepInterval,
		maxIP:         opts.RateLimit.MaxRequestsByIP,
		maxGlobal:     opts.RateLimit.MaxRequestsGlobal,
		maxKeys:       opts.RateLimit.MaxTrackedKeys,
		byIP:          make(map[string][]time.Time),
		resolver:      newTrustedProxyResolver(opts.TrustedProxies),
	}
}

func (l *agentRequestLimiter) allow(r *http.Request) bool {
	if l == nil {
		return true
	}
	clientIP := l.resolver.clientIP(r)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	cutoff := now.Add(-l.window)
	l.sweepExpiredLocked(now, cutoff)
	ipEvents := l.eventsForKey(clientIP, cutoff)
	l.global = pruneTimes(l.global, cutoff)
	if l.maxIP > 0 && len(ipEvents) >= l.maxIP {
		return false
	}
	if l.maxGlobal > 0 && len(l.global) >= l.maxGlobal {
		return false
	}
	if events, ok := l.trackableEventsForKey(clientIP, cutoff); ok {
		l.byIP[clientIP] = append(events, now)
	}
	l.global = append(l.global, now)
	return true
}

func (l *agentRequestLimiter) eventsForKey(key string, cutoff time.Time) []time.Time {
	events, ok := l.byIP[key]
	if !ok {
		return nil
	}
	events = pruneTimes(events, cutoff)
	if len(events) == 0 {
		delete(l.byIP, key)
		return nil
	}
	l.byIP[key] = events
	return events
}

func (l *agentRequestLimiter) trackableEventsForKey(key string, cutoff time.Time) ([]time.Time, bool) {
	events := l.eventsForKey(key, cutoff)
	if events != nil {
		return events, true
	}
	if l.maxKeys > 0 && len(l.byIP) >= l.maxKeys {
		return nil, false
	}
	return nil, true
}

func (l *agentRequestLimiter) sweepExpiredLocked(now, cutoff time.Time) {
	if l.sweepInterval <= 0 {
		return
	}
	if !l.nextSweep.IsZero() && now.Before(l.nextSweep) {
		return
	}
	sweepTimeMap(l.byIP, cutoff)
	l.nextSweep = now.Add(l.sweepInterval)
}

func rejectAgentRateLimited(w http.ResponseWriter) {
	writeAgentAPIError(w, http.StatusTooManyRequests, agentapi.ErrorCodeInvalidRequest, "too many requests")
}

type agentSyncInflightGate chan struct{}

func newAgentSyncInflightGate(max int) agentSyncInflightGate {
	if max <= 0 {
		max = defaultAgentRateLimitOptions().MaxSyncInflight
	}
	return make(agentSyncInflightGate, max)
}

func (g agentSyncInflightGate) acquire() bool {
	select {
	case g <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g agentSyncInflightGate) release() {
	<-g
}

func rejectAgentUnavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	writeAgentAPIError(w, http.StatusServiceUnavailable, agentapi.ErrorCodeInvalidRequest, "service unavailable")
}

func AgentEnroll(svc AgentEnrollService) http.Handler {
	return AgentEnrollWithOptions(svc, AgentEndpointOptions{})
}

func AgentEnrollWithOptions(svc AgentEnrollService, opts AgentEndpointOptions) http.Handler {
	limiter := newAgentRequestLimiter(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAgentAPIError(w, http.StatusMethodNotAllowed, agentapi.ErrorCodeMethodNotAllowed, "method not allowed")
			return
		}
		if !limiter.allow(r) {
			rejectAgentRateLimited(w)
			return
		}

		var req agentapi.EnrollmentRequest
		if err := decodeJSONLimited(w, r, &req, AgentEnrollBodyLimit); err != nil {
			writeAgentAPIError(w, http.StatusBadRequest, agentapi.ErrorCodeInvalidJSON, "invalid json")
			return
		}
		if !isValidEnrollmentRequest(req) {
			writeAgentAPIError(w, http.StatusBadRequest, agentapi.ErrorCodeInvalidRequest, "invalid request")
			return
		}

		result, err := svc.EnrollMonitoringInstance(r.Context(), enrollment.EnrollInput{
			Token:       req.Token,
			Fingerprint: req.Fingerprint,
		})
		if err != nil {
			switch {
			case errors.Is(err, enrollment.ErrInvalidEnrollmentToken):
				writeAgentAPIError(w, http.StatusUnauthorized, agentapi.ErrorCodeInvalidEnrollmentToken, "invalid enrollment token")
			default:
				writeAgentAPIError(w, http.StatusInternalServerError, agentapi.ErrorCodeInternalError, "internal server error")
			}
			return
		}

		writeJSON(w, http.StatusOK, agentapi.EnrollmentResponse{
			MonitoringInstanceID: result.MonitoringInstanceID,
			BindingStatus:        result.BindingStatus,
			Status:               "accepted",
			SyncToken:            result.SyncToken,
		})
	})
}

func AgentSync(svc AgentSyncService) http.Handler {
	return AgentSyncWithOptions(svc, AgentEndpointOptions{})
}

func AgentSyncWithOptions(svc AgentSyncService, opts AgentEndpointOptions) http.Handler {
	limiter := newAgentRequestLimiter(opts)
	inflight := newAgentSyncInflightGate(opts.RateLimit.MaxSyncInflight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAgentAPIError(w, http.StatusMethodNotAllowed, agentapi.ErrorCodeMethodNotAllowed, "method not allowed")
			return
		}
		if !limiter.allow(r) {
			rejectAgentRateLimited(w)
			return
		}
		if !isValidOptionalSyncTokenHeader(r) {
			writeAgentAPIError(w, http.StatusBadRequest, agentapi.ErrorCodeInvalidRequest, "invalid request")
			return
		}
		if !inflight.acquire() {
			rejectAgentUnavailable(w)
			return
		}
		defer inflight.release()

		var req agentapi.SyncRequest
		if err := decodeJSONLimited(w, r, &req, AgentSyncBodyLimit); err != nil {
			writeAgentAPIError(w, http.StatusBadRequest, agentapi.ErrorCodeInvalidJSON, "invalid json")
			return
		}
		if !isValidSyncRequest(req) {
			writeAgentAPIError(w, http.StatusBadRequest, agentapi.ErrorCodeInvalidRequest, "invalid request")
			return
		}

		result, err := svc.SyncBatch(r.Context(), syncBatchFromRequest(req))
		if err != nil {
			switch {
			case errors.Is(err, syncing.ErrInvalidSyncToken):
				writeAgentAPIError(w, http.StatusUnauthorized, agentapi.ErrorCodeInvalidSyncToken, "invalid sync token")
			case errors.Is(err, syncing.ErrBindingNotAccepted):
				writeAgentAPIError(w, http.StatusConflict, agentapi.ErrorCodeBindingNotAccepted, "binding not accepted")
			case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
				writeAgentAPIError(w, http.StatusNotFound, agentapi.ErrorCodeMonitoringInstanceNotFound, "monitoring instance not found")
			case errors.Is(err, observations.ErrInvalidProbeObservation):
				writeAgentAPIError(w, http.StatusBadRequest, agentapi.ErrorCodeInvalidRequest, "invalid request")
			default:
				writeAgentAPIError(w, http.StatusInternalServerError, agentapi.ErrorCodeInternalError, "internal server error")
			}
			return
		}

		writeJSON(w, http.StatusOK, agentapi.SyncResponse{
			AcceptedAt: result.AcceptedAt,
			Status:     "accepted",
			Plan:       syncPlanToAPI(result.Plan),
		})
	})
}

func isValidOptionalSyncTokenHeader(r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("X-Houfeng-Agent-Token"))
	if authorization := strings.TrimSpace(r.Header.Get("Authorization")); authorization != "" {
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authorization, bearerPrefix) {
			return false
		}
		token = strings.TrimSpace(strings.TrimPrefix(authorization, bearerPrefix))
	}
	if token == "" {
		return true
	}
	if len(token) > agentSecretMaxBytes {
		return false
	}
	for _, r := range token {
		if r <= ' ' || r == '"' || r == '\'' || r == '\\' {
			return false
		}
	}
	return true
}

func writeAgentAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, agentapi.ErrorResponse{Code: code, Message: message})
}

func isValidEnrollmentRequest(req agentapi.EnrollmentRequest) bool {
	return requiredMax(req.Token, agentSecretMaxBytes) &&
		requiredMax(req.Fingerprint, agentIdentityMaxBytes)
}

func isValidSyncRequest(req agentapi.SyncRequest) bool {
	if !requiredMax(req.MonitoringInstanceID, agentIdentityMaxBytes) ||
		!requiredMax(req.SyncToken, agentSecretMaxBytes) ||
		len(req.Heartbeats) == 0 {
		return false
	}
	if exceedsAgentBatchLimit(len(req.Heartbeats), len(req.HostSamples), len(req.ProbeObservations), len(req.IPQualityReports), len(req.CommandResults)) {
		return false
	}

	for _, heartbeat := range req.Heartbeats {
		if !isValidAgentCarrier(heartbeat.ObservedAt, heartbeat.AgentVersion, heartbeat.Fingerprint, heartbeat.SyncBatchID) {
			return false
		}
	}

	for _, sample := range req.HostSamples {
		if !isValidAgentCarrier(sample.ObservedAt, sample.AgentVersion, sample.Fingerprint, sample.SyncBatchID) ||
			len(sample.Containers) > agentSyncMaxItems {
			return false
		}
		for _, container := range sample.Containers {
			if !optionalMax(container.ID, agentIdentityMaxBytes) ||
				!optionalMax(container.Name, agentIdentityMaxBytes) ||
				!optionalMax(container.Image, agentIdentityMaxBytes) ||
				!optionalMax(container.Status, agentIdentityMaxBytes) {
				return false
			}
		}
	}

	for _, observation := range req.ProbeObservations {
		if !requiredMax(observation.TargetID, agentIdentityMaxBytes) ||
			!requiredMax(observation.ProbeItemID, agentIdentityMaxBytes) ||
			!requiredMax(observation.ProbeKind, agentIdentityMaxBytes) ||
			!isValidAgentCarrier(observation.ObservedAt, observation.AgentVersion, observation.Fingerprint, observation.SyncBatchID) ||
			!requiredMax(observation.ResultKind, agentIdentityMaxBytes) ||
			!optionalMax(observation.ErrorCode, agentIdentityMaxBytes) ||
			!optionalMax(observation.ErrorSummary, agentErrorSummaryMaxBytes) {
			return false
		}
	}
	for _, report := range req.IPQualityReports {
		if !isValidAgentCarrier(report.ObservedAt, report.AgentVersion, report.Fingerprint, report.SyncBatchID) ||
			!requiredMax(report.IPAddress, agentIdentityMaxBytes) ||
			report.IPVersion == 0 ||
			!requiredMax(report.Status, agentIdentityMaxBytes) ||
			!optionalMax(report.ASN, agentIdentityMaxBytes) ||
			!optionalMax(report.Organization, agentErrorSummaryMaxBytes) ||
			!optionalMax(report.UseRegionCode, agentIdentityMaxBytes) ||
			!optionalMax(report.UseRegionName, agentIdentityMaxBytes) ||
			!optionalMax(report.RegisteredRegionCode, agentIdentityMaxBytes) ||
			!optionalMax(report.RegisteredRegionName, agentIdentityMaxBytes) ||
			!optionalMax(report.RiskLevel, agentIdentityMaxBytes) ||
			!optionalMax(report.ErrorCode, agentIdentityMaxBytes) ||
			!optionalMax(report.ErrorSummary, agentErrorSummaryMaxBytes) ||
			len(report.RawJSON) > agentRawJSONMaxBytes ||
			len(report.DiagnosticsJSON) > agentRawJSONMaxBytes ||
			len(report.ProviderResults) > agentSyncMaxItems ||
			len(report.ServiceUnlocks) > agentSyncMaxItems {
			return false
		}
		if !isValidIPQualityReportStatus(report.Status) {
			return false
		}
		for _, provider := range report.ProviderResults {
			if !requiredMax(provider.Provider, agentIdentityMaxBytes) ||
				!optionalMax(provider.Status, agentIdentityMaxBytes) ||
				!optionalMax(provider.SourceType, agentIdentityMaxBytes) ||
				!optionalMax(provider.UsageType, agentIdentityMaxBytes) ||
				!optionalMax(provider.CompanyType, agentIdentityMaxBytes) ||
				!optionalMax(provider.RiskLevel, agentIdentityMaxBytes) ||
				!optionalMax(provider.RiskScore, agentIdentityMaxBytes) ||
				!optionalMax(provider.RegionCode, agentIdentityMaxBytes) ||
				!optionalMax(provider.RegionName, agentIdentityMaxBytes) ||
				!optionalMax(provider.ErrorCode, agentIdentityMaxBytes) ||
				!optionalMax(provider.ErrorSummary, agentErrorSummaryMaxBytes) ||
				len(provider.ExtraJSON) > agentRawJSONMaxBytes {
				return false
			}
			if provider.Status != "" && !isValidIPQualitySourceStatus(provider.Status) {
				return false
			}
			if provider.SourceType != "" && !isValidIPQualitySourceType(provider.SourceType) {
				return false
			}
		}
		for _, unlock := range report.ServiceUnlocks {
			if !requiredMax(unlock.Service, agentIdentityMaxBytes) ||
				!requiredMax(unlock.Status, agentIdentityMaxBytes) ||
				!optionalMax(unlock.Source, agentIdentityMaxBytes) ||
				!optionalMax(unlock.ProbeStatus, agentIdentityMaxBytes) ||
				!optionalMax(unlock.Region, agentIdentityMaxBytes) ||
				!optionalMax(unlock.UnlockType, agentIdentityMaxBytes) ||
				!optionalMax(unlock.ErrorCode, agentIdentityMaxBytes) ||
				!optionalMax(unlock.ErrorSummary, agentErrorSummaryMaxBytes) ||
				len(unlock.ExtraJSON) > agentRawJSONMaxBytes {
				return false
			}
			if !isValidIPQualityServiceStatus(unlock.Status) {
				return false
			}
			if unlock.ProbeStatus != "" && !isValidIPQualitySourceStatus(unlock.ProbeStatus) {
				return false
			}
		}
	}
	for _, result := range req.CommandResults {
		if !requiredMax(result.ActionID, agentIdentityMaxBytes) ||
			!requiredMax(result.CommandID, agentIdentityMaxBytes) ||
			!optionalMax(result.Stdout, agentCommandOutputMaxBytes) ||
			!optionalMax(result.Stderr, agentCommandOutputMaxBytes) {
			return false
		}
	}

	return true
}

func exceedsAgentBatchLimit(lengths ...int) bool {
	for _, length := range lengths {
		if length > agentSyncMaxItems {
			return true
		}
	}
	return false
}

func isValidAgentCarrier(observedAt time.Time, agentVersion, fingerprint, syncBatchID string) bool {
	return !observedAt.IsZero() &&
		requiredMax(agentVersion, agentIdentityMaxBytes) &&
		requiredMax(fingerprint, agentIdentityMaxBytes) &&
		requiredMax(syncBatchID, agentIdentityMaxBytes)
}

func requiredMax(value string, maxBytes int) bool {
	return value != "" && optionalMax(value, maxBytes)
}

func optionalMax(value string, maxBytes int) bool {
	return len(value) <= maxBytes
}

func isValidIPQualityReportStatus(value string) bool {
	switch value {
	case agentapi.IPQualityStatusSuccess, agentapi.IPQualityStatusPartial, agentapi.IPQualityStatusFailure:
		return true
	default:
		return false
	}
}

func isValidIPQualitySourceStatus(value string) bool {
	switch value {
	case "success", "failure", "skipped", "not_configured":
		return true
	default:
		return false
	}
}

func isValidIPQualitySourceType(value string) bool {
	switch value {
	case "default", "optional", "custom":
		return true
	default:
		return false
	}
}

func isValidIPQualityServiceStatus(value string) bool {
	switch value {
	case "unlocked", "blocked", "partial", "unknown":
		return true
	default:
		return false
	}
}

func observationBatchFromSyncRequest(req agentapi.SyncRequest) (observations.BatchWrite, bool) {
	if len(req.HostSamples) == 0 && len(req.ProbeObservations) == 0 {
		return observations.BatchWrite{}, false
	}

	batch := observations.BatchWrite{
		MonitoringInstanceID: req.MonitoringInstanceID,
		HostSamples:          make([]observations.HostSampleWrite, 0, len(req.HostSamples)),
		ProbeObservations:    make([]observations.ProbeObservationWrite, 0, len(req.ProbeObservations)),
	}

	for _, sample := range req.HostSamples {
		batch.HostSamples = append(batch.HostSamples, observations.HostSampleWrite{
			MonitoringInstanceID: req.MonitoringInstanceID,
			ObservedAt:           sample.ObservedAt,
			AgentVersion:         sample.AgentVersion,
			Fingerprint:          sample.Fingerprint,
			CPUUsagePct:          sample.CPUUsagePct,
			Load1:                sample.Load1,
			Load5:                sample.Load5,
			Load15:               sample.Load15,
			MemUsedPct:           sample.MemUsedPct,
			MemAvailableBytes:    sample.MemAvailableBytes,
			MemTotalBytes:        sample.MemTotalBytes,
			SwapUsedPct:          sample.SwapUsedPct,
			DiskUsedPct:          sample.DiskUsedPct,
			DiskTotalBytes:       sample.DiskTotalBytes,
			InodeUsedPct:         sample.InodeUsedPct,
			NetInBytesPerSec:     sample.NetInBytesPerSec,
			NetOutBytesPerSec:    sample.NetOutBytesPerSec,
			CPUIOWaitPct:         sample.CPUIOWaitPct,
			CPUStealPct:          sample.CPUStealPct,
			DiskReadBytesPerSec:  sample.DiskReadBytesPerSec,
			DiskWriteBytesPerSec: sample.DiskWriteBytesPerSec,
			DiskBusyPct:          sample.DiskBusyPct,
			UptimeSeconds:        sample.UptimeSeconds,
			MaintenanceContext:   sample.MaintenanceContext,
			IsBackfilled:         sample.IsBackfilled,
			SyncBatchID:          sample.SyncBatchID,
			Containers:           sample.Containers,
		})
	}

	for _, observation := range req.ProbeObservations {
		batch.ProbeObservations = append(batch.ProbeObservations, observations.ProbeObservationWrite{
			MonitoringInstanceID: req.MonitoringInstanceID,
			TargetID:             observation.TargetID,
			ProbeItemID:          observation.ProbeItemID,
			ProbeKind:            observation.ProbeKind,
			ObservedAt:           observation.ObservedAt,
			AgentVersion:         observation.AgentVersion,
			Fingerprint:          observation.Fingerprint,
			ResultKind:           observation.ResultKind,
			LatencyMS:            observation.LatencyMS,
			HTTPStatus:           observation.HTTPStatus,
			TLSExpiryDays:        observation.TLSExpiryDays,
			ErrorCode:            observation.ErrorCode,
			ErrorSummary:         observation.ErrorSummary,
			MaintenanceContext:   observation.MaintenanceContext,
			IsBackfilled:         observation.IsBackfilled,
			SyncBatchID:          observation.SyncBatchID,
		})
	}

	return batch, true
}

func syncBatchFromRequest(req agentapi.SyncRequest) syncing.Batch {
	heartbeats := make([]syncing.HeartbeatPayload, 0, len(req.Heartbeats))
	for _, heartbeat := range req.Heartbeats {
		heartbeats = append(heartbeats, syncing.HeartbeatPayload{
			ObservedAt:   heartbeat.ObservedAt,
			AgentVersion: heartbeat.AgentVersion,
			Fingerprint:  heartbeat.Fingerprint,
			SyncBatchID:  heartbeat.SyncBatchID,
			IsBackfilled: heartbeat.IsBackfilled,
		})
	}

	batch := syncing.Batch{
		MonitoringInstanceID: req.MonitoringInstanceID,
		SyncToken:            req.SyncToken,
		Heartbeats:           heartbeats,
	}
	if observationBatch, ok := observationBatchFromSyncRequest(req); ok {
		batch.Observations = observationBatch
	}
	if len(req.IPQualityReports) > 0 {
		batch.IPQualityReports = ipQualityReportsFromRequest(req)
	}

	if len(req.CommandResults) > 0 {
		results := make([]syncing.CommandResult, 0, len(req.CommandResults))
		for _, cr := range req.CommandResults {
			results = append(results, syncing.CommandResult{
				ActionID:  cr.ActionID,
				CommandID: cr.CommandID,
				Stdout:    cr.Stdout,
				Stderr:    cr.Stderr,
				ExitCode:  cr.ExitCode,
			})
		}
		batch.CommandResults = results
	}

	return batch
}

func ipQualityReportsFromRequest(req agentapi.SyncRequest) []ipquality.ReportWrite {
	reports := make([]ipquality.ReportWrite, 0, len(req.IPQualityReports))
	for _, report := range req.IPQualityReports {
		write := ipquality.ReportWrite{
			MonitoringInstanceID: req.MonitoringInstanceID,
			ObservedAt:           report.ObservedAt,
			AgentVersion:         report.AgentVersion,
			Fingerprint:          report.Fingerprint,
			SyncBatchID:          report.SyncBatchID,
			IPAddress:            report.IPAddress,
			IPVersion:            report.IPVersion,
			Status:               report.Status,
			ASN:                  report.ASN,
			Organization:         report.Organization,
			Latitude:             report.Latitude,
			Longitude:            report.Longitude,
			UseRegionCode:        report.UseRegionCode,
			UseRegionName:        report.UseRegionName,
			RegisteredRegionCode: report.RegisteredRegionCode,
			RegisteredRegionName: report.RegisteredRegionName,
			RiskLevel:            report.RiskLevel,
			ErrorCode:            report.ErrorCode,
			ErrorSummary:         report.ErrorSummary,
			IsBackfilled:         report.IsBackfilled,
			RawJSON:              ipquality.SanitizeRawJSON(report.RawJSON),
			CoverageJSON:         coveragePayloadJSON(report.Coverage),
			DiagnosticsJSON:      ipquality.SanitizeExtraJSON(report.DiagnosticsJSON),
			ProviderResults:      make([]ipquality.ProviderResultWrite, 0, len(report.ProviderResults)),
			ServiceUnlocks:       make([]ipquality.ServiceUnlockWrite, 0, len(report.ServiceUnlocks)),
		}
		for _, provider := range report.ProviderResults {
			write.ProviderResults = append(write.ProviderResults, ipquality.ProviderResultWrite{
				Provider:     provider.Provider,
				Status:       provider.Status,
				SourceType:   provider.SourceType,
				LatencyMS:    provider.LatencyMS,
				UsageType:    provider.UsageType,
				CompanyType:  provider.CompanyType,
				RiskLevel:    provider.RiskLevel,
				RiskScore:    provider.RiskScore,
				RegionCode:   provider.RegionCode,
				RegionName:   provider.RegionName,
				IsProxy:      provider.IsProxy,
				IsTor:        provider.IsTor,
				IsVPN:        provider.IsVPN,
				IsServer:     provider.IsServer,
				IsAbuser:     provider.IsAbuser,
				IsRobot:      provider.IsRobot,
				ErrorCode:    provider.ErrorCode,
				ErrorSummary: provider.ErrorSummary,
				ExtraJSON:    ipquality.SanitizeExtraJSON(provider.ExtraJSON),
			})
		}
		for _, unlock := range report.ServiceUnlocks {
			write.ServiceUnlocks = append(write.ServiceUnlocks, ipquality.ServiceUnlockWrite{
				Service:      unlock.Service,
				Source:       unlock.Source,
				Status:       unlock.Status,
				ProbeStatus:  unlock.ProbeStatus,
				LatencyMS:    unlock.LatencyMS,
				Region:       unlock.Region,
				UnlockType:   unlock.UnlockType,
				ErrorCode:    unlock.ErrorCode,
				ErrorSummary: unlock.ErrorSummary,
				ExtraJSON:    ipquality.SanitizeExtraJSON(unlock.ExtraJSON),
			})
		}
		reports = append(reports, write)
	}
	return reports
}

func coveragePayloadJSON(coverage *agentapi.IPQualityCoveragePayload) json.RawMessage {
	if coverage == nil {
		return nil
	}
	payload, err := json.Marshal(coverage)
	if err != nil {
		return nil
	}
	return ipquality.SanitizeExtraJSON(payload)
}

func syncPlanToAPI(plan agentplan.SyncPlan) *agentapi.SyncPlan {
	assignments := make([]agentapi.ProbeAssignment, 0, len(plan.ProbeAssignments))
	for _, assignment := range plan.ProbeAssignments {
		assignments = append(assignments, agentapi.ProbeAssignment{
			TargetID:           assignment.TargetID,
			TargetHost:         assignment.TargetHost,
			TargetBasePort:     assignment.TargetBasePort,
			MaintenanceContext: assignment.MaintenanceContext,
			ProbeItemID:        assignment.ProbeItemID,
			ProbeKind:          assignment.ProbeKind,
			FrequencyTier:      assignment.FrequencyTier,
			TimeoutSeconds:     assignment.TimeoutSeconds,
			Config:             append([]byte(nil), assignment.Config...),
		})
	}

	apiPlan := &agentapi.SyncPlan{
		HostSampleFrequencyTier:      plan.HostSampleFrequencyTier,
		HostSampleMaintenanceContext: plan.HostSampleMaintenanceContext,
		ProbeAssignments:             assignments,
	}

	if plan.PendingAction != nil {
		apiPlan.PendingAction = &agentapi.PendingAction{
			CommandID: plan.PendingAction.CommandID,
			ActionID:  plan.PendingAction.ActionID,
		}
	}
	if plan.IPQualityPlan != nil {
		apiPlan.IPQualityPlan = &agentapi.IPQualityPlan{
			Enabled:          plan.IPQualityPlan.Enabled,
			FrequencySeconds: plan.IPQualityPlan.FrequencySeconds,
			TimeoutSeconds:   plan.IPQualityPlan.TimeoutSeconds,
			Services:         append([]string(nil), plan.IPQualityPlan.Services...),
		}
	}

	return apiPlan
}
