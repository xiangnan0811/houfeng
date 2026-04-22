package handlers

import (
	"encoding/json"
	"net/http"
)

type HealthResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Status  string `json:"status"`
}

func Healthz(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Name:    "houfeng-center",
			Version: version,
			Status:  "ok",
		})
	}
}
