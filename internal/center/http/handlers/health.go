package handlers

import (
	"encoding/json"
	"net/http"

	"houfeng/internal/contracts/agentapi"
)

type HealthResponse struct {
	Name                      string `json:"name"`
	Version                   string `json:"version"`
	Status                    string `json:"status"`
	AgentSyncProtocolRevision int    `json:"agent_sync_protocol_revision"`
	AgentSyncReplayWindow     int64  `json:"agent_sync_replay_window"`
}

func Healthz(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Name:                      "houfeng-center",
			Version:                   version,
			Status:                    "ok",
			AgentSyncProtocolRevision: agentapi.SyncProtocolRevision,
			AgentSyncReplayWindow:     agentapi.SyncReplayWindow,
		})
	}
}
