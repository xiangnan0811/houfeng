package handlers

import (
	"context"
	"errors"
	"net/http"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/nodes"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

type AgentEnrollService interface {
	EnrollNode(ctx context.Context, input enrollment.EnrollInput) (enrollment.EnrollResult, error)
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

		result, err := svc.EnrollNode(r.Context(), enrollment.EnrollInput{
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
			NodeID:        result.NodeID,
			BindingStatus: result.BindingStatus,
			Status:        "accepted",
			SyncToken:     result.SyncToken,
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
			case errors.Is(err, nodes.ErrNodeNotFound):
				writeAgentAPIError(w, http.StatusNotFound, agentapi.ErrorCodeNodeNotFound, "node not found")
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
	if req.NodeID == "" || req.SyncToken == "" || len(req.Heartbeats) == 0 {
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

	return true
}

func observationBatchFromSyncRequest(req agentapi.SyncRequest) (observations.BatchWrite, bool) {
	if len(req.HostSamples) == 0 && len(req.ProbeObservations) == 0 {
		return observations.BatchWrite{}, false
	}

	batch := observations.BatchWrite{
		NodeID:            req.NodeID,
		HostSamples:       make([]observations.HostSampleWrite, 0, len(req.HostSamples)),
		ProbeObservations: make([]observations.ProbeObservationWrite, 0, len(req.ProbeObservations)),
	}

	for _, sample := range req.HostSamples {
		batch.HostSamples = append(batch.HostSamples, observations.HostSampleWrite{
			NodeID:               req.NodeID,
			ObservedAt:           sample.ObservedAt,
			AgentVersion:         sample.AgentVersion,
			Fingerprint:          sample.Fingerprint,
			CPUUsagePct:          sample.CPUUsagePct,
			Load1:                sample.Load1,
			Load5:                sample.Load5,
			Load15:               sample.Load15,
			MemUsedPct:           sample.MemUsedPct,
			MemAvailableBytes:    sample.MemAvailableBytes,
			SwapUsedPct:          sample.SwapUsedPct,
			DiskUsedPct:          sample.DiskUsedPct,
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
		})
	}

	for _, observation := range req.ProbeObservations {
		batch.ProbeObservations = append(batch.ProbeObservations, observations.ProbeObservationWrite{
			NodeID:             req.NodeID,
			TargetID:           observation.TargetID,
			ProbeItemID:        observation.ProbeItemID,
			ProbeKind:          observation.ProbeKind,
			ObservedAt:         observation.ObservedAt,
			AgentVersion:       observation.AgentVersion,
			Fingerprint:        observation.Fingerprint,
			ResultKind:         observation.ResultKind,
			LatencyMS:          observation.LatencyMS,
			HTTPStatus:         observation.HTTPStatus,
			TLSExpiryDays:      observation.TLSExpiryDays,
			ErrorCode:          observation.ErrorCode,
			ErrorSummary:       observation.ErrorSummary,
			MaintenanceContext: observation.MaintenanceContext,
			IsBackfilled:       observation.IsBackfilled,
			SyncBatchID:        observation.SyncBatchID,
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
		})
	}

	batch := syncing.Batch{
		NodeID:     req.NodeID,
		SyncToken:  req.SyncToken,
		Heartbeats: heartbeats,
	}
	if observationBatch, ok := observationBatchFromSyncRequest(req); ok {
		batch.Observations = observationBatch
	}

	return batch
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
	return &agentapi.SyncPlan{
		HostSampleFrequencyTier: plan.HostSampleFrequencyTier,
		ProbeAssignments:        assignments,
	}
}
