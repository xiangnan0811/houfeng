package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxMetadataLabelCount = 20
	maxMetadataLabelRunes = 64
	maxMetadataNoteRunes  = 2000
)

type updateMetadataRequest struct {
	Labels *[]string `json:"labels"`
	Note   *string   `json:"note"`
}

func decodeUpdateMetadataRequest(r *http.Request) ([]string, string, bool, error) {
	var request updateMetadataRequest
	if err := decodeJSON(r, &request); err != nil {
		return nil, "", false, err
	}
	if request.Labels == nil || request.Note == nil {
		return nil, "", false, nil
	}
	return *request.Labels, *request.Note, true, nil
}

func parseMetadataPrecondition(value string) (*time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) && len(value) >= 2 {
		value = value[1 : len(value)-1]
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, false
	}
	return &updatedAt, true
}

func normalizeMetadata(labels []string, note string) ([]string, string) {
	normalizedLabels := make([]string, 0, len(labels))
	seen := make(map[string]struct{}, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		normalizedLabels = append(normalizedLabels, label)
	}
	return normalizedLabels, strings.TrimSpace(note)
}

func isValidMetadata(labels []string, note string) bool {
	if len(labels) > maxMetadataLabelCount {
		return false
	}
	for _, label := range labels {
		if utf8.RuneCountInString(label) > maxMetadataLabelRunes {
			return false
		}
	}
	return utf8.RuneCountInString(note) <= maxMetadataNoteRunes
}
