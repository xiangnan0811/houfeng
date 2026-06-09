package handlers

import (
	"context"
	"net/http"
	"strings"

	"houfeng/internal/center/ipquality"
)

type VPSIPQualityRepository interface {
	GetVPSIPQuality(context.Context, string) (ipquality.VPSReport, error)
	GetVPSIPQualityReportDetail(context.Context, string, string) (ipquality.VPSReport, error)
}

func VPSIPQuality(repo VPSIPQualityRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, reportID, ok := parseVPSIPQualityPath(r.URL.Path)
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var (
			report ipquality.VPSReport
			err    error
		)
		if reportID != "" {
			report, err = repo.GetVPSIPQualityReportDetail(r.Context(), vpsID, reportID)
		} else {
			report, err = repo.GetVPSIPQuality(r.Context(), vpsID)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, report)
	})
}

func parseVPSIPQualityPath(path string) (vpsID, reportID string, ok bool) {
	if id, matched := parseVPSSubresourcePath(path, "ip-quality"); matched {
		return id, "", true
	}
	prefix := "/api/vps/"
	relative := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if relative == path || relative == "" {
		return "", "", false
	}
	segments := strings.Split(relative, "/")
	if len(segments) != 4 || segments[1] != "ip-quality" || segments[2] != "reports" || segments[0] == "" || segments[3] == "" {
		return "", "", false
	}
	return segments[0], segments[3], true
}
