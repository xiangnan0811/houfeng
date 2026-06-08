package handlers

import (
	"context"
	"net/http"

	"houfeng/internal/center/ipquality"
)

type VPSIPQualityRepository interface {
	GetVPSIPQuality(context.Context, string) (ipquality.VPSReport, error)
}

func VPSIPQuality(repo VPSIPQualityRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "ip-quality")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		report, err := repo.GetVPSIPQuality(r.Context(), vpsID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
}
