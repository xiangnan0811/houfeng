package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

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

func AgentEnroll(svc AgentEnrollService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAgentAPIError(w, http.StatusMethodNotAllowed, agentapi.ErrorCodeMethodNotAllowed, "method not allowed")
			return
		}

		var req agentapi.EnrollmentRequest
		if err := decodeJSON(r, &req); err != nil {
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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeAgentAPIError(w, http.StatusMethodNotAllowed, agentapi.ErrorCodeMethodNotAllowed, "method not allowed")
			return
		}

		var req agentapi.SyncRequest
		if err := decodeJSON(r, &req); err != nil {
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

func writeAgentAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, agentapi.ErrorResponse{Code: code, Message: message})
}

func isValidEnrollmentRequest(req agentapi.EnrollmentRequest) bool {
	return req.Token != "" && req.Fingerprint != ""
}

func isValidSyncRequest(req agentapi.SyncRequest) bool {
	if req.MonitoringInstanceID == "" || req.SyncToken == "" || len(req.Heartbeats) == 0 {
		return false
	}

	for _, heartbeat := range req.Heartbeats {
		if heartbeat.ObservedAt.IsZero() || heartbeat.AgentVersion == "" || heartbeat.Fingerprint == "" || heartbeat.SyncBatchID == "" {
			return false
		}
	}

	for _, sample := range req.HostSamples {
		if sample.ObservedAt.IsZero() || sample.AgentVersion == "" || sample.Fingerprint == "" || sample.SyncBatchID == "" {
			return false
		}
	}

	for _, observation := range req.ProbeObservations {
		if observation.TargetID == "" || observation.ProbeItemID == "" || observation.ProbeKind == "" ||
			observation.ObservedAt.IsZero() || observation.AgentVersion == "" || observation.Fingerprint == "" ||
			observation.SyncBatchID == "" || observation.ResultKind == "" {
			return false
		}
	}
	for _, report := range req.IPQualityReports {
		if report.ObservedAt.IsZero() || report.AgentVersion == "" || report.Fingerprint == "" || report.SyncBatchID == "" ||
			report.IPAddress == "" || report.IPVersion == 0 || report.Status == "" {
			return false
		}
		if !isValidIPQualityReportStatus(report.Status) {
			return false
		}
		for _, provider := range report.ProviderResults {
			if provider.Provider == "" {
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
			if unlock.Service == "" || unlock.Status == "" {
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

	return true
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
