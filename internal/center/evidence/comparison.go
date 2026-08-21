package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidComparisonSelection    = errors.New("invalid comparison selection")
	ErrComparisonSelectionIncomplete = errors.New("incomplete comparison selection")
	ErrComparisonSelectionNotFound   = errors.New("comparison selection not found")
	ErrComparisonResultTooLarge      = errors.New("comparison result too large")
	ErrComparisonCapacityExhausted   = errors.New("comparison capacity exhausted")
	ErrComparisonRequestMemoryLimit  = errors.New("comparison request exceeds memory limit")
	ErrComparisonIntentUnavailable   = errors.New("comparison intent unavailable")
	ErrComparisonIntentInvalid       = errors.New("invalid comparison intent")
	ErrComparisonIntentExpired       = errors.New("comparison intent expired")
	ErrComparisonIntentStale         = errors.New("comparison intent stale")
)

const (
	ComparisonIntentPurpose       = "comparison-save/v1"
	ComparisonIntentTTL           = 15 * time.Minute
	ComparisonAdmissionTokenBytes = int64(8 << 20)
	ComparisonAdmissionWait       = 2 * time.Second
	ComparisonAdmissionMaxQueue   = 16
	ComparisonCalculationVersion  = "comparison/v1"
)

type ComparisonReason string

const (
	ReasonMetadataOnly             ComparisonReason = "metadata_only"
	ReasonKindMissing              ComparisonReason = "kind_missing"
	ReasonMetricMissing            ComparisonReason = "metric_missing"
	ReasonCoveragePartial          ComparisonReason = "coverage_partial"
	ReasonCoverageTruncated        ComparisonReason = "coverage_truncated"
	ReasonCommonOverlapUnsupported ComparisonReason = "common_overlap_unsupported"
	ReasonCommonOverlapEmpty       ComparisonReason = "common_overlap_empty"
	ReasonSchemaIncompatible       ComparisonReason = "schema_incompatible"
	ReasonUnitIncompatible         ComparisonReason = "unit_incompatible"
	ReasonPrecisionIncompatible    ComparisonReason = "precision_incompatible"
	ReasonSourceTombstoned         ComparisonReason = "source_tombstoned"
	ReasonSourceUnavailable        ComparisonReason = "source_unavailable"
	ReasonSnapshotUnreadable       ComparisonReason = "snapshot_unreadable"
)

type CoverageAlignment string

const (
	CoverageActual        CoverageAlignment = "actual_coverage"
	CoverageCommonOverlap CoverageAlignment = "common_overlap"
)

type RevisionContext string

const (
	RevisionContextBound         RevisionContext = "bound"
	RevisionContextNotApplicable RevisionContext = "not_applicable"
)

type RevisionMetadataSnapshot struct {
	RecordType     string
	BusinessStatus string
	StatusGroup    string
	ImpactLevel    string
	OccurredAt     time.Time
	HasOccurredAt  bool
}

type ComparisonItemInput struct {
	SnapshotID      string
	Hash            [sha256.Size]byte
	Kind            KindKey
	RevisionContext RevisionContext
	Revision        *RevisionMetadataSnapshot
	RecordID        string
	RevisionID      string
	SubjectKind     string
	SubjectID       string
	Snapshot        CanonicalSnapshot
	Reasons         []ComparisonReason
}

type ComparisonDetail struct {
	Kind   KindKey
	Metric string
}

type ComparisonEvaluateInput struct {
	Items           []ComparisonItemInput
	BaselineIndex   int
	Alignment       CoverageAlignment
	RequestedWindow TimeWindow
	Tolerance       time.Duration
	Detail          *ComparisonDetail
}

type ResolvedComparisonItem struct {
	SnapshotID      string
	Hash            [sha256.Size]byte
	Kind            KindKey
	RevisionContext RevisionContext
	Revision        *RevisionMetadataSnapshot
	RecordID        string
	RevisionID      string
	SubjectKind     string
	SubjectID       string
}

type ComparabilityFinding struct {
	ItemIndex int
	Kind      KindKey
	Reason    ComparisonReason
}

type SeriesPoint struct {
	Start time.Time
	End   time.Time
	Value float64
}

type Series struct {
	ItemIndex int
	MetricID  string
	Segments  [][]SeriesPoint
	Unit      string
}

type ComparisonEvaluateResult struct {
	Digest   [sha256.Size]byte
	Items    []ResolvedComparisonItem
	Review   []ComparabilityFinding
	Pairwise []Comparison
	Series   []Series
}

func SupportsMonitoringSeries(key KindKey) bool {
	return key == MonitoringHostV1Key() || key == MonitoringProbeV2Key()
}

func EvaluateComparison(registry Registry, input ComparisonEvaluateInput) (ComparisonEvaluateResult, error) {
	if len(input.Items) < 2 || len(input.Items) > 6 ||
		input.BaselineIndex < 0 || input.BaselineIndex >= len(input.Items) ||
		(input.Alignment != CoverageActual && input.Alignment != CoverageCommonOverlap) {
		return ComparisonEvaluateResult{}, fmt.Errorf("%w: item count or baseline", ErrInvalidComparisonSelection)
	}

	items := make([]ResolvedComparisonItem, 0, len(input.Items))
	review := make([]ComparabilityFinding, 0)
	for index, item := range input.Items {
		resolved := ResolvedComparisonItem{
			SnapshotID:      item.SnapshotID,
			Hash:            item.Hash,
			Kind:            item.Kind,
			RevisionContext: item.RevisionContext,
			RecordID:        item.RecordID,
			RevisionID:      item.RevisionID,
			SubjectKind:     item.SubjectKind,
			SubjectID:       item.SubjectID,
		}
		if item.RevisionContext == RevisionContextBound {
			resolved.Revision = cloneRevisionMetadataSnapshot(item.Revision)
		}
		items = append(items, resolved)
		for _, reason := range item.Reasons {
			review = append(review, ComparabilityFinding{ItemIndex: index, Kind: item.Kind, Reason: reason})
		}
	}

	if input.Alignment == CoverageCommonOverlap {
		review = append(review, ComparabilityFinding{Reason: ReasonCommonOverlapUnsupported})
	}

	digest, err := comparisonDigest(comparisonDigestBody{
		Items:           items,
		BaselineIndex:   input.BaselineIndex,
		Alignment:       input.Alignment,
		RequestedWindow: input.RequestedWindow,
		Tolerance:       input.Tolerance,
		Detail:          input.Detail,
	})
	if err != nil {
		return ComparisonEvaluateResult{}, err
	}

	result := ComparisonEvaluateResult{Digest: digest, Items: items, Review: review}
	if input.Detail == nil || input.Alignment == CoverageCommonOverlap {
		return result, nil
	}

	if SupportsMonitoringSeries(input.Detail.Kind) {
		series, pairwise, findings, seriesErr := evaluateMonitoringSeries(input)
		if seriesErr != nil {
			return ComparisonEvaluateResult{}, seriesErr
		}
		result.Series = series
		result.Pairwise = pairwise
		result.Review = append(result.Review, findings...)
		return result, nil
	}

	kind, lookupErr := registry.LookupKey(input.Detail.Kind)
	if lookupErr != nil {
		result.Review = append(result.Review, ComparabilityFinding{Kind: input.Detail.Kind, Reason: ReasonKindMissing})
		return result, nil
	}
	baseline := input.Items[input.BaselineIndex]
	if skipComparisonItemPayload(baseline.Reasons) {
		return result, nil
	}
	for index, item := range input.Items {
		if index == input.BaselineIndex || skipComparisonItemPayload(item.Reasons) {
			continue
		}
		if item.Kind != input.Detail.Kind || baseline.Kind != input.Detail.Kind {
			result.Review = append(result.Review, ComparabilityFinding{ItemIndex: index, Kind: item.Kind, Reason: ReasonSchemaIncompatible})
			continue
		}
		compared := kind.Compare(baseline.Snapshot, item.Snapshot, Alignment{Mode: AlignmentExact})
		compared.ItemIndex = index
		if compared.Values == nil {
			compared.Values = map[string]any{}
		}
		compared.Values["item_index"] = index
		result.Pairwise = append(result.Pairwise, compared)
	}
	return result, nil
}

func evaluateMonitoringSeries(input ComparisonEvaluateInput) ([]Series, []Comparison, []ComparabilityFinding, error) {
	metric := ""
	if input.Detail != nil {
		metric = input.Detail.Metric
	}
	if metric == "" {
		return nil, nil, []ComparabilityFinding{{
			Kind: input.Detail.Kind, Reason: ReasonMetricMissing,
		}}, nil
	}
	grouped := make([][]MonitoringBucketPoint, 0, len(input.Items))
	for _, item := range input.Items {
		if skipComparisonItemPayload(item.Reasons) || item.Kind != input.Detail.Kind {
			grouped = append(grouped, nil)
			continue
		}
		payload, err := decodeSnapshotPayload(item.Snapshot.Bytes())
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%w: snapshot payload", ErrInvalidCanonicalPayload)
		}
		points, err := ExtractMonitoringSeriesPoints(payload, metric, item.Hash)
		if err != nil {
			return nil, nil, nil, err
		}
		grouped = append(grouped, points)
	}
	series, matches, err := AlignActualCoverage(grouped, input.BaselineIndex, input.Tolerance, metric)
	if err != nil {
		return nil, nil, nil, err
	}
	baselineSkip := skipComparisonItemPayload(input.Items[input.BaselineIndex].Reasons) ||
		input.Items[input.BaselineIndex].Kind != input.Detail.Kind
	pairwise := make([]Comparison, 0, len(matches))
	findings := make([]ComparabilityFinding, 0)
	for _, match := range matches {
		item := input.Items[match.ItemIndex]
		if baselineSkip || skipComparisonItemPayload(item.Reasons) || item.Kind != input.Detail.Kind {
			continue
		}
		if match.UnmatchedBaseline > 0 || match.UnmatchedItem > 0 {
			findings = append(findings, ComparabilityFinding{
				ItemIndex: match.ItemIndex, Kind: input.Detail.Kind, Reason: ReasonCoveragePartial,
			})
		}
		pairwise = append(pairwise, coverageMatchComparison(input.Detail.Kind, match))
	}
	return series, pairwise, findings, nil
}

func decodeSnapshotPayload(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: empty payload", ErrInvalidCanonicalPayload)
	}
	var document canonicalDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCanonicalPayload, err)
	}
	if document.Payload == nil {
		return nil, fmt.Errorf("%w: missing payload", ErrInvalidCanonicalPayload)
	}
	return document.Payload, nil
}

func skipComparisonItemPayload(reasons []ComparisonReason) bool {
	return hasReason(reasons, ReasonMetadataOnly) || hasReason(reasons, ReasonSnapshotUnreadable)
}

func hasReason(reasons []ComparisonReason, want ComparisonReason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func cloneRevisionMetadataSnapshot(value *RevisionMetadataSnapshot) *RevisionMetadataSnapshot {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

type comparisonDigestBody struct {
	Items           []ResolvedComparisonItem
	BaselineIndex   int
	Alignment       CoverageAlignment
	RequestedWindow TimeWindow
	Tolerance       time.Duration
	Detail          *ComparisonDetail
}

func comparisonDigest(value any) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}

func (item ResolvedComparisonItem) MarshalJSON() ([]byte, error) {
	var revision *revisionMetadataJSON
	if item.Revision != nil {
		revision = &revisionMetadataJSON{
			RecordType:     item.Revision.RecordType,
			BusinessStatus: item.Revision.BusinessStatus,
			StatusGroup:    item.Revision.StatusGroup,
			ImpactLevel:    item.Revision.ImpactLevel,
		}
		if item.Revision.HasOccurredAt {
			revision.OccurredAt = item.Revision.OccurredAt.UTC().Format(time.RFC3339Nano)
		}
	}
	return json.Marshal(struct {
		SnapshotID      string                `json:"snapshot_id"`
		Hash            string                `json:"hash"`
		Kind            string                `json:"kind"`
		RevisionContext RevisionContext       `json:"revision_context"`
		Revision        *revisionMetadataJSON `json:"revision"`
	}{
		SnapshotID:      item.SnapshotID,
		Hash:            hex.EncodeToString(item.Hash[:]),
		Kind:            item.Kind.String(),
		RevisionContext: item.RevisionContext,
		Revision:        revision,
	})
}

type revisionMetadataJSON struct {
	RecordType     string `json:"record_type"`
	BusinessStatus string `json:"business_status"`
	StatusGroup    string `json:"status_group"`
	ImpactLevel    string `json:"impact_level"`
	OccurredAt     string `json:"occurred_at,omitempty"`
}
