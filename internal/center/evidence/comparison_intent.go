package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

type ComparisonIntent struct {
	Token     string
	KeyID     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type ComparisonIntentClaims struct {
	Purpose             string                 `json:"purpose"`
	KeyID               string                 `json:"key_id"`
	ActorHash           string                 `json:"actor_hash"`
	ProjectID           string                 `json:"project_id"`
	Items               []ComparisonIntentItem `json:"items"`
	BaselineIndex       int                    `json:"baseline_index"`
	Alignment           CoverageAlignment      `json:"alignment"`
	RequestedStart      string                 `json:"requested_from"`
	RequestedEnd        string                 `json:"requested_to"`
	ToleranceSeconds    int64                  `json:"tolerance_seconds"`
	BucketSeconds       *int64                 `json:"bucket_seconds,omitempty"`
	Digest              string                 `json:"digest"`
	RegistryVersion     string                 `json:"registry_version"`
	CalculationVersion  string                 `json:"calculation_version"`
	WarningsDigest      string                 `json:"warnings_digest"`
	DetailKind          string                 `json:"detail_kind,omitempty"`
	DetailSchemaVersion uint16                 `json:"detail_schema_version,omitempty"`
	DetailMetric        string                 `json:"detail_metric,omitempty"`
	IssuedAt            time.Time              `json:"-"`
	ExpiresAt           time.Time              `json:"-"`
	IssuedAtText        string                 `json:"issued_at"`
	ExpiresAtText       string                 `json:"expires_at"`
}

type ComparisonIntentItem struct {
	SnapshotID        string          `json:"snapshot_id,omitempty"`
	Hash              string          `json:"hash,omitempty"`
	Kind              string          `json:"kind,omitempty"`
	RevisionContext   RevisionContext `json:"revision_context"`
	RecordID          string          `json:"record_id,omitempty"`
	RevisionID        string          `json:"revision_id,omitempty"`
	ChosenSnapshotIDs []string        `json:"chosen_snapshot_ids,omitempty"`
	RecordType        string          `json:"record_type,omitempty"`
	BusinessStatus    string          `json:"business_status,omitempty"`
	StatusGroup       string          `json:"status_group,omitempty"`
	ImpactLevel       string          `json:"impact_level,omitempty"`
	OccurredAt        string          `json:"occurred_at,omitempty"`
}

type ComparisonIntentSigner interface {
	Sign(ComparisonIntentClaims) (ComparisonIntent, error)
	Verify(string, time.Time) (ComparisonIntentClaims, error)
}

type ComparisonIntentClaimsInput struct {
	Actor           ActorScope
	Items           []ResolvedComparisonItem
	BaselineIndex   int
	Alignment       CoverageAlignment
	RequestedWindow TimeWindow
	Tolerance       time.Duration
	BucketWidth     *time.Duration
	Digest          [sha256.Size]byte
	Review          []ComparabilityFinding
	Detail          *ComparisonDetail
	Now             time.Time
	KeyID           string
}

func BuildComparisonIntentClaims(input ComparisonIntentClaimsInput) ComparisonIntentClaims {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	window := normalizeWindow(input.RequestedWindow)
	items := make([]ComparisonIntentItem, 0, len(input.Items))
	for _, item := range input.Items {
		encoded := ComparisonIntentItem{
			SnapshotID:      item.SnapshotID,
			Hash:            hex.EncodeToString(item.Hash[:]),
			Kind:            item.Kind.String(),
			RevisionContext: item.RevisionContext,
			RecordID:        item.RecordID,
			RevisionID:      item.RevisionID,
		}
		if item.SnapshotID != "" && item.RevisionContext == RevisionContextBound {
			encoded.ChosenSnapshotIDs = []string{item.SnapshotID}
		}
		if item.Revision != nil && item.RevisionContext == RevisionContextBound {
			encoded.RecordType = item.Revision.RecordType
			encoded.BusinessStatus = item.Revision.BusinessStatus
			encoded.StatusGroup = item.Revision.StatusGroup
			encoded.ImpactLevel = item.Revision.ImpactLevel
			if item.Revision.HasOccurredAt {
				encoded.OccurredAt = item.Revision.OccurredAt.UTC().Format(time.RFC3339Nano)
			}
		}
		items = append(items, encoded)
	}
	var bucket *int64
	if input.BucketWidth != nil {
		seconds := int64(input.BucketWidth.Seconds())
		bucket = &seconds
	}
	actorHash := input.Actor.CanonicalHash()
	warnings := comparisonWarningsDigest(input.Review)
	claims := ComparisonIntentClaims{
		Purpose:            ComparisonIntentPurpose,
		KeyID:              input.KeyID,
		ActorHash:          hex.EncodeToString(actorHash[:]),
		ProjectID:          string(input.Actor.ProjectID),
		Items:              items,
		BaselineIndex:      input.BaselineIndex,
		Alignment:          input.Alignment,
		RequestedStart:     window.Start.UTC().Format(time.RFC3339Nano),
		RequestedEnd:       window.End.UTC().Format(time.RFC3339Nano),
		ToleranceSeconds:   int64(input.Tolerance / time.Second),
		BucketSeconds:      bucket,
		Digest:             hex.EncodeToString(input.Digest[:]),
		RegistryVersion:    "evidence-kinds/v1",
		CalculationVersion: ComparisonCalculationVersion,
		WarningsDigest:     hex.EncodeToString(warnings[:]),
		IssuedAt:           now,
		ExpiresAt:          now.Add(ComparisonIntentTTL),
		IssuedAtText:       now.Format(time.RFC3339Nano),
		ExpiresAtText:      now.Add(ComparisonIntentTTL).Format(time.RFC3339Nano),
	}
	if input.Detail != nil && input.Detail.Kind.Kind != "" {
		claims.DetailKind = string(input.Detail.Kind.Kind)
		claims.DetailSchemaVersion = uint16(input.Detail.Kind.SchemaVersion)
		claims.DetailMetric = input.Detail.Metric
	}
	return claims
}

func comparisonWarningsDigest(review []ComparabilityFinding) [sha256.Size]byte {
	digest, err := comparisonDigest(review)
	if err != nil {
		return sha256.Sum256(nil)
	}
	return digest
}
