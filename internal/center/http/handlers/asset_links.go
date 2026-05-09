package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/assetlinks"
)

func VPSNodes(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "nodes")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		records, err := repo.ListNodesForVPS(r.Context(), vpsID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func VPSLinkNode(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "link-node")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlinks.LinkInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlinks.NormalizeLinkInput(input)
		if err := assetlinks.ValidateLinkInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		record, err := repo.LinkNode(r.Context(), vpsID, input)
		if errors.Is(err, assetlinks.ErrInvalidVPSNodeLinkInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if errors.Is(err, assetlinks.ErrVPSNodeLinkNotFound) {
			writeError(w, http.StatusNotFound, "vps or node not found")
			return
		}
		if errors.Is(err, assetlinks.ErrVPSNodeLinkConflict) {
			writeError(w, http.StatusConflict, "vps node link conflict")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusCreated, record)
	})
}

func VPSUnlinkNode(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "unlink-node")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlinks.UnlinkInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlinks.NormalizeUnlinkInput(input)
		if err := assetlinks.ValidateUnlinkInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		record, err := repo.UnlinkNode(r.Context(), vpsID, input)
		if errors.Is(err, assetlinks.ErrInvalidVPSNodeLinkInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if errors.Is(err, assetlinks.ErrVPSNodeLinkNotFound) {
			writeError(w, http.StatusNotFound, "vps node link not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func NodeVPS(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeID, ok := parseNodeSubresourcePath(r.URL.Path, "vps")
		if !ok {
			writeError(w, http.StatusNotFound, "node not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		records, err := repo.ListVPSForNode(r.Context(), nodeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func parseVPSSubresourcePath(path, wantSubresource string) (string, bool) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/vps/"), "/")
	segments := strings.Split(relative, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != wantSubresource {
		return "", false
	}
	return segments[0], true
}

func parseNodeSubresourcePath(path, wantSubresource string) (string, bool) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/nodes/"), "/")
	segments := strings.Split(relative, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != wantSubresource {
		return "", false
	}
	return segments[0], true
}
