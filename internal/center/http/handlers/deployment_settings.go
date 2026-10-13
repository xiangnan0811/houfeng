package handlers

import (
	"net/http"

	"houfeng/internal/center/accessadmin"
	centerconfig "houfeng/internal/center/config"
)

// DeploymentSettings exposes the immutable startup deployment snapshot. The
// endpoint intentionally performs authorization on every request: the frontend
// capability is only a convenience and is never the security boundary.
func DeploymentSettings(service *accessadmin.Service, snapshot centerconfig.DeploymentSettingsSnapshot) http.Handler {
	immutableSnapshot := snapshot.Clone()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if r.Method != http.MethodGet {
			writeCodedError(w, http.StatusMethodNotAllowed, "method not allowed", "method_not_allowed")
			return
		}
		if _, ok := requireAccessAdminSupervisorWithUnavailableStatus(w, r, service, http.StatusInternalServerError); !ok {
			return
		}
		// Keep response ownership separate from the handler's captured snapshot.
		// This prevents a caller that retained the input value from mutating a
		// later response through the Items slice or Value pointers.
		writeJSON(w, http.StatusOK, immutableSnapshot.Clone())
	})
}
