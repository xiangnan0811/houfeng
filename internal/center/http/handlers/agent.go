package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/nodes"
	"houfeng/internal/contracts/agentapi"
)

type AgentEnrollmentService interface {
	EnrollNode(ctx context.Context, input enrollment.EnrollInput) (enrollment.EnrollResult, error)
	RecordHeartbeatSync(ctx context.Context, input enrollment.SyncInput) error
}

func AgentEnroll(svc AgentEnrollmentService) http.Handler {
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
		})
	})
}

func AgentSync(svc AgentEnrollmentService) http.Handler {
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

		heartbeats := make([]enrollment.HeartbeatPayload, 0, len(req.Heartbeats))
		for _, heartbeat := range req.Heartbeats {
			heartbeats = append(heartbeats, enrollment.HeartbeatPayload{
				ObservedAt:   heartbeat.ObservedAt,
				AgentVersion: heartbeat.AgentVersion,
				Fingerprint:  heartbeat.Fingerprint,
				SyncBatchID:  heartbeat.SyncBatchID,
			})
		}

		if err := svc.RecordHeartbeatSync(r.Context(), enrollment.SyncInput{
			NodeID:     req.NodeID,
			Heartbeats: heartbeats,
		}); err != nil {
			switch {
			case errors.Is(err, enrollment.ErrBindingNotAccepted):
				writeAgentAPIError(w, http.StatusConflict, agentapi.ErrorCodeBindingNotAccepted, "binding not accepted")
			case errors.Is(err, nodes.ErrNodeNotFound):
				writeAgentAPIError(w, http.StatusNotFound, agentapi.ErrorCodeNodeNotFound, "node not found")
			default:
				writeAgentAPIError(w, http.StatusInternalServerError, agentapi.ErrorCodeInternalError, "internal server error")
			}
			return
		}

		writeJSON(w, http.StatusOK, agentapi.SyncResponse{
			AcceptedAt: time.Now().UTC(),
			Status:     "accepted",
		})
	})
}

func writeAgentAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, agentapi.ErrorResponse{Code: code, Message: message})
}
