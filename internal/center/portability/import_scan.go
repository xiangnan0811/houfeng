package portability

import (
	"encoding/json"
	"regexp"
	"strings"
)

var importedActiveURIPattern = regexp.MustCompile(`(?i)(?:javascript:|file://|data:[a-z0-9.+-]+/)`)

func scanImportedMember(entry ArchiveEntry) error {
	name := strings.ToLower(entry.Path + " " + entry.Classification)
	if strings.Contains(name, "checkpoint") {
		return ErrUntrustedImportContent
	}
	if entry.Classification == ArchiveClassMarkdown {
		if importedActiveURIPattern.Match(entry.Payload) {
			return ErrUntrustedImportContent
		}
		return nil
	}
	if entry.Classification == ArchiveClassAttachment {
		if len(entry.Payload) == 0 {
			return ErrUntrustedImportContent
		}
		return nil
	}
	var decoded any
	if err := json.Unmarshal(entry.Payload, &decoded); err != nil {
		return ErrUntrustedImportContent
	}
	return rejectUntrustedJSON(decoded)
}

func rejectUntrustedJSON(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for key := range object {
		switch strings.ToLower(key) {
		case "authorization", "role", "renderer", "sql", "password", "token", "path", "url":
			return ErrUntrustedImportContent
		}
	}
	return nil
}
