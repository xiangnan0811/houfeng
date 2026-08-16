// Package evidence owns the transport-neutral contracts for immutable record
// evidence kinds and canonical payloads.
package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"houfeng/internal/center/recordauth"
)

const (
	CanonicalizationVersionV1 uint16 = 1
	ForbiddenCorpusVersionV1  uint16 = 1

	MaxCanonicalPayloadBytes uint64 = 5 * 1024 * 1024
	MaxSnapshotDataPoints    uint64 = 50_000
	MaxMetricBucketCount     uint64 = 2_000
	MaxPeakCount             uint64 = 20
	CaptureIntentTTL                = 15 * time.Minute
)

var (
	ErrInvalidKindDescriptor    = errors.New("invalid evidence kind descriptor")
	ErrInvalidKindRegistry      = errors.New("invalid evidence kind registry")
	ErrKindNotRegistered        = errors.New("evidence kind not registered")
	ErrUnknownKindVersion       = errors.New("unknown evidence kind version")
	ErrFieldNotAllowed          = errors.New("evidence field not allowed")
	ErrForbiddenField           = errors.New("forbidden evidence field")
	ErrInvalidCanonicalPayload  = errors.New("invalid canonical evidence payload")
	ErrCanonicalPayloadTooLarge = errors.New("canonical evidence payload too large")
	ErrInvalidSnapshotEnvelope  = errors.New("invalid evidence snapshot envelope")
	ErrKindConformance          = errors.New("evidence kind conformance failed")
)

type ActorScope = recordauth.ActorScope
type AuthorizationScope = recordauth.SourceAuthorization

type KindName string
type SchemaVersion uint16

const (
	KindIPQualityReport  KindName = "ip_quality.report"
	KindMonitoringHost   KindName = "monitoring.host"
	KindMonitoringProbe  KindName = "monitoring.probe"
	KindMonitoringEvent  KindName = "monitoring.event"
	KindSubscriptionCost KindName = "subscription.cost"
	KindCommandAudit     KindName = "command.audit"
)

type KindKey struct {
	Kind          KindName
	SchemaVersion SchemaVersion
}

func IPQualityReportV1Key() KindKey {
	return KindKey{Kind: KindIPQualityReport, SchemaVersion: 1}
}

func MonitoringHostV1Key() KindKey {
	return KindKey{Kind: KindMonitoringHost, SchemaVersion: 1}
}

func MonitoringProbeV2Key() KindKey {
	return KindKey{Kind: KindMonitoringProbe, SchemaVersion: 2}
}

func MonitoringEventV2Key() KindKey {
	return KindKey{Kind: KindMonitoringEvent, SchemaVersion: 2}
}

func SubscriptionCostV1Key() KindKey {
	return KindKey{Kind: KindSubscriptionCost, SchemaVersion: 1}
}

func CommandAuditV1Key() KindKey {
	return KindKey{Kind: KindCommandAudit, SchemaVersion: 1}
}

type Sensitivity string

const (
	SensitivityNormal            Sensitivity = "normal"
	SensitivitySensitiveTopology Sensitivity = "sensitive_topology"
	SensitivityForbidden         Sensitivity = "forbidden"
)

type FieldFormat string

const (
	FieldFormatText FieldFormat = ""
	FieldFormatURL  FieldFormat = "url"
)

type FieldDefinition struct {
	Path        string
	Sensitivity Sensitivity
	Format      FieldFormat
}

type ConformanceMetadata struct {
	CanonicalizationVersion uint16
	ForbiddenCorpusVersion  uint16
	RendererVersion         string
	MaxCanonicalBytes       uint64
}

type Descriptor struct {
	Key         KindKey
	Fields      []FieldDefinition
	Conformance ConformanceMetadata
}

type IdentitySnapshot struct {
	Type   string
	ID     string
	Fields map[string]string
}

type TimeWindow struct {
	Start time.Time
	End   time.Time
}

type QualityStatus string

const (
	QualityComplete QualityStatus = "complete"
	QualityPartial  QualityStatus = "partial"
	QualityDegraded QualityStatus = "degraded"
	QualityUnknown  QualityStatus = "unknown"
)

type Quality struct {
	Status           QualityStatus
	SampleCount      uint64
	GapCount         uint64
	MaintenanceCount uint64
	BackfilledCount  uint64
	BucketCount      uint64
	DataPointCount   uint64
	PeakCount        uint64
	Truncated        bool
	Partial          bool
}

type DurationSemantics struct {
	Applicable bool
	Value      time.Duration
	Reason     string
}

type UnitsStatus string

const (
	UnitsApplicable    UnitsStatus = "applicable"
	UnitsNotApplicable UnitsStatus = "not_applicable"
)

type UnitsSemantics struct {
	Status UnitsStatus
	Values map[string]string
	Reason string
}

type QuotaStatus string

const (
	QuotaAllowed     QuotaStatus = "allowed"
	QuotaWarning     QuotaStatus = "warning"
	QuotaExceeded    QuotaStatus = "exceeded"
	QuotaUnavailable QuotaStatus = "unavailable"
)

type QuotaOutcome struct {
	Status QuotaStatus
	Reason string
}

type RetentionScope string

const RetentionScopeRecordRevision RetentionScope = "record_revision"

type SourceDeletionSemantics string

const SourceDeletionSnapshotRetained SourceDeletionSemantics = "snapshot_retained_source_unavailable"

type RetentionSemantics struct {
	Immutable      bool
	Scope          RetentionScope
	SourceDeletion SourceDeletionSemantics
}

type Selection struct {
	Key                     KindKey
	SourceType              string
	SourceID                string
	RequestedWindow         TimeWindow
	Metrics                 []string
	Precision               time.Duration
	SensitiveTopologyFields []string
}

type Intent struct {
	ID            string
	Key           KindKey
	Selection     Selection
	PreviewDigest [sha256.Size]byte
	ValidUntil    time.Time
}

type Preview struct {
	IntentID                string
	Key                     KindKey
	Selection               Selection
	Subject                 IdentitySnapshot
	Source                  IdentitySnapshot
	RequestedWindow         TimeWindow
	ActualWindow            TimeWindow
	ObservedAt              time.Time
	SourceRevision          string
	SourceWatermark         string
	ProducerVersion         string
	CalculationVersion      string
	Units                   UnitsSemantics
	Quality                 Quality
	Sensitivity             Sensitivity
	ActualPrecision         DurationSemantics
	BucketWidth             DurationSemantics
	QuotaOutcome            QuotaOutcome
	Retention               RetentionSemantics
	Redaction               []FieldDecision
	EstimatedCanonicalBytes uint64
	SourceDigest            [sha256.Size]byte
	RendererVersion         string
	PreviewedAt             time.Time
	ValidUntil              time.Time
}

type SnapshotEnvelope struct {
	Key                KindKey
	Subject            IdentitySnapshot
	Source             IdentitySnapshot
	Authorization      AuthorizationScope
	RequestedWindow    TimeWindow
	ActualWindow       TimeWindow
	ObservedAt         time.Time
	CapturedAt         time.Time
	ReferencedAt       time.Time
	SourceRevision     string
	SourceWatermark    string
	SourceDigest       [sha256.Size]byte
	ProducerVersion    string
	CalculationVersion string
	Units              UnitsSemantics
	Quality            Quality
	Sensitivity        Sensitivity
	ActualPrecision    DurationSemantics
	BucketWidth        DurationSemantics
	QuotaOutcome       QuotaOutcome
	Retention          RetentionSemantics
	Redaction          []FieldDecision
	CanonicalHash      [sha256.Size]byte
	CanonicalSize      uint64
}

type CanonicalPayload struct {
	bytes []byte
	hash  [sha256.Size]byte
}

func (payload CanonicalPayload) Bytes() []byte {
	return append([]byte(nil), payload.bytes...)
}

func (payload CanonicalPayload) Hash() [sha256.Size]byte {
	return payload.hash
}

func (payload CanonicalPayload) Size() uint64 {
	return uint64(len(payload.bytes))
}

type CanonicalSnapshot struct {
	envelope SnapshotEnvelope
	payload  CanonicalPayload
}

func (snapshot CanonicalSnapshot) Envelope() SnapshotEnvelope {
	return cloneSnapshotEnvelope(snapshot.envelope)
}

func (snapshot CanonicalSnapshot) Bytes() []byte {
	return snapshot.payload.Bytes()
}

func (snapshot CanonicalSnapshot) Hash() [sha256.Size]byte {
	return snapshot.payload.Hash()
}

func (snapshot CanonicalSnapshot) Size() uint64 {
	return snapshot.payload.Size()
}

type Summary struct {
	Key             KindKey
	RendererVersion string
	Title           string
	SearchText      string
	ReadModel       map[string]any
}

type AlignmentMode string

const AlignmentExact AlignmentMode = "exact"

type Alignment struct {
	Mode AlignmentMode
}

type Comparison struct {
	Key        KindKey
	Compatible bool
	Reason     string
	Values     map[string]any
}

type ExportMode string

const (
	ExportModeSafe              ExportMode = "safe"
	ExportModeSensitiveTopology ExportMode = "sensitive_topology"
)

type ExportMaterial struct {
	Key       KindKey
	MediaType string
	Filename  string
	Bytes     []byte
}

// Kind implementations are registered once and may be called concurrently by
// request and worker paths. Implementations must therefore be concurrency-safe
// and must not mutate values received from the registry or callers.
type Kind interface {
	Descriptor() Descriptor
	ValidateSelection(context.Context, ActorScope, Selection) error
	PreviewCapture(context.Context, ActorScope, Selection) (Preview, error)
	Capture(context.Context, ActorScope, Intent) (CanonicalSnapshot, error)
	Authorize(context.Context, ActorScope, Selection) (AuthorizationScope, error)
	Summarize(CanonicalSnapshot) Summary
	Compare(CanonicalSnapshot, CanonicalSnapshot, Alignment) Comparison
	Export(CanonicalSnapshot, ExportMode) ExportMaterial
}
