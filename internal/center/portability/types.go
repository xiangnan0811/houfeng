package portability

import (
	"errors"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordmarkdown"
	"houfeng/internal/center/store"
)

var (
	ErrPortabilityDisabled    = errors.New("record portability is disabled")
	ErrInvalidExportRequest   = errors.New("invalid record export request")
	ErrExportUnauthorized     = errors.New("record export unauthorized")
	ErrExportInventoryDrift   = errors.New("record export inventory drifted")
	ErrExportLeaseRevoked     = errors.New("record export lease revoked")
	ErrExportNotFound         = errors.New("record export not found")
	ErrExportUnavailable      = errors.New("record export unavailable")
	ErrUnsupportedExportKind  = errors.New("record export kind is not supported")
	ErrInvalidArchive         = errors.New("invalid record archive")
	ErrUntrustedImportContent = errors.New("untrusted import content")
	ErrImportSchemaBlocked    = errors.New("import schema blocked")
	ErrImportCASConflict      = errors.New("import cas conflict")
	ErrOriginTombstoned       = errors.New("record origin is tombstoned")
	ErrImportOriginConflict   = errors.New("record origin already exists")
	ErrInvalidImportRequest   = errors.New("invalid record import request")
)

const (
	ExportKindMarkdown       = store.RecordExportKindMarkdown
	ExportKindComparisonJSON = store.RecordExportKindComparisonJSON
	ExportKindEvidenceJSON   = store.RecordExportKindEvidenceJSON
	ExportKindArchive        = store.RecordExportKindArchive
	ExportKindPDF            = store.RecordExportKindPDF
	ExportModeSafe           = store.RecordExportModeSafe
	ExportModeSensitiveTopo  = store.RecordExportModeSensitiveTopo
	maxExportPayloadBytes    = 1 << 20
	defaultPreviewTTL        = 15 * time.Minute
)

type PreviewRequest struct {
	Actor           recordauth.ActorScope
	IdempotencyKey  string
	RecordID        string
	RevisionID      string
	SnapshotID      string
	ExportKind      string
	ExportMode      string
	IncludeActivity bool
	ConfirmToken    string
}

type UnavailableMaterial struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type ExpectedFile struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	ByteSize  int64  `json:"byte_size"`
}

type Preview struct {
	PreviewID         string                              `json:"preview_id"`
	PreviewToken      string                              `json:"preview_token"`
	ExportKind        string                              `json:"export_kind"`
	ExportMode        string                              `json:"export_mode"`
	InventoryDigest   string                              `json:"inventory_digest"`
	ExpectedFiles     []ExpectedFile                      `json:"expected_files"`
	Unavailable       []UnavailableMaterial               `json:"unavailable"`
	RenderStatus      recordmarkdown.DocumentRenderStatus `json:"render_status,omitempty"`
	ComparisonSummary map[string]any                      `json:"comparison_summary,omitempty"`
	ConfirmToken      string                              `json:"confirm_token,omitempty"`
	ExpiresAt         time.Time                           `json:"expires_at"`
}

type CreateRequest struct {
	Actor           recordauth.ActorScope
	IdempotencyKey  string
	PreviewID       string
	PreviewToken    string
	InventoryDigest string
	ConfirmToken    string
}

type ExportView struct {
	ExportID   string    `json:"export_id"`
	JobState   string    `json:"job_state"`
	ExportKind string    `json:"export_kind"`
	MediaType  string    `json:"media_type,omitempty"`
	ByteSize   uint64    `json:"byte_size,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type Content struct {
	MediaType string
	Filename  string
	ByteSize  int64
	Body      interface {
		Read([]byte) (int, error)
		Close() error
	}
}
