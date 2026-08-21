package portability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

const (
	ArchiveFormatV1            = "houfeng-record-archive/v1"
	ArchiveManifestName        = "manifest.json"
	ArchiveClassMarkdown       = "markdown"
	ArchiveClassComparisonJSON = "comparison_json"
	ArchiveClassEvidenceJSON   = "evidence_json"
	ArchiveClassAttachment     = "attachment"
)

type ArchiveEntry struct {
	Path           string
	Classification string
	Payload        []byte
}

type ArchiveManifest struct {
	Format string                `json:"format"`
	Files  []ArchiveManifestFile `json:"files"`
}

type ArchiveManifestFile struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	Size           uint64 `json:"size"`
	Classification string `json:"classification"`
}

func newArchiveManifest(entries []ArchiveEntry) (ArchiveManifest, error) {
	files := make([]ArchiveManifestFile, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		normalized, err := normalizeArchivePath(entry.Path)
		if err != nil {
			return ArchiveManifest{}, err
		}
		if normalized == ArchiveManifestName {
			return ArchiveManifest{}, fmt.Errorf("%w: reserved path", ErrInvalidArchive)
		}
		if _, exists := seen[normalized]; exists {
			return ArchiveManifest{}, fmt.Errorf("%w: path collision", ErrInvalidArchive)
		}
		if !validArchiveClassification(entry.Classification) {
			return ArchiveManifest{}, fmt.Errorf("%w: classification", ErrInvalidArchive)
		}
		if err := validateArchivePayload(entry.Payload); err != nil {
			return ArchiveManifest{}, err
		}
		seen[normalized] = struct{}{}
		sum := sha256.Sum256(entry.Payload)
		files = append(files, ArchiveManifestFile{
			Path:           normalized,
			SHA256:         hex.EncodeToString(sum[:]),
			Size:           uint64(len(entry.Payload)),
			Classification: entry.Classification,
		})
	}
	sort.Slice(files, func(left, right int) bool {
		return files[left].Path < files[right].Path
	})
	return ArchiveManifest{Format: ArchiveFormatV1, Files: files}, nil
}

func (manifest ArchiveManifest) validate() error {
	if manifest.Format != ArchiveFormatV1 || manifest.Files == nil {
		return fmt.Errorf("%w: manifest", ErrInvalidArchive)
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	var previous string
	for index, file := range manifest.Files {
		normalized, err := normalizeArchivePath(file.Path)
		if err != nil || normalized != file.Path || normalized == ArchiveManifestName {
			return fmt.Errorf("%w: manifest path", ErrInvalidArchive)
		}
		if _, exists := seen[normalized]; exists {
			return fmt.Errorf("%w: manifest collision", ErrInvalidArchive)
		}
		if index > 0 && previous >= normalized {
			return fmt.Errorf("%w: manifest order", ErrInvalidArchive)
		}
		if !validArchiveClassification(file.Classification) || len(file.SHA256) != 64 {
			return fmt.Errorf("%w: manifest file", ErrInvalidArchive)
		}
		if _, err := hex.DecodeString(file.SHA256); err != nil {
			return fmt.Errorf("%w: manifest digest", ErrInvalidArchive)
		}
		seen[normalized] = struct{}{}
		previous = normalized
	}
	return nil
}

func validArchiveClassification(value string) bool {
	switch value {
	case ArchiveClassMarkdown, ArchiveClassComparisonJSON, ArchiveClassEvidenceJSON, ArchiveClassAttachment:
		return true
	default:
		return false
	}
}
