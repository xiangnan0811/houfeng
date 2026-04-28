package handlers

import (
	"strings"
	"unicode/utf8"
)

const (
	maxMetadataLabelCount = 20
	maxMetadataLabelRunes = 64
	maxMetadataNoteRunes  = 2000
)

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
