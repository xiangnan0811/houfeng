package evidence

import (
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"houfeng/internal/center/recordidentity"
)

// ComparisonDependencyItem describes an unresolved canonical reference. RecordID
// is revision context, not proof that an embedded foreign ID belongs locally.
// Resolution must bind this exact item to trusted native or import provenance.
type ComparisonDependencyItem struct {
	OriginalSnapshotID string
	CopiedSnapshotID   string
	RecordID           string
	RevisionID         string
	RevisionContext    RevisionContext
	Hash               string
	Kind               string
}

// ComparisonDependency retains source identity even after that source is gone.
// Only the result snapshot has a referential lifetime dependency in storage.
type ComparisonDependency struct {
	SourceRecordID   string
	SourceRevisionID string
	SourceSnapshotID string
}

// ComparisonDependencyIndexBatchResult is a bounded lexical scan, not a global
// readiness certificate. An empty continuation starts the next complete sweep.
type ComparisonDependencyIndexBatchResult struct {
	AfterSnapshotID string
	Examined        uint64
	Incomplete      uint64
}

// ExtractComparisonDependencyItems validates the actual canonical bytes before
// interpreting references. It never resolves embedded IDs by local existence.
func ExtractComparisonDependencyItems(snapshot CanonicalSnapshot) ([]ComparisonDependencyItem, error) {
	if err := snapshot.Validate(comparisonResultDescriptor()); err != nil {
		return nil, err
	}
	payload, err := decodeSnapshotPayload(snapshot.payload.bytes)
	if err != nil {
		return nil, err
	}
	return extractComparisonDependencyItems(payload)
}

func extractComparisonDependencyItems(payload map[string]any) ([]ComparisonDependencyItem, error) {
	if payload["version"] != "comparison_result/v1" {
		return nil, fmt.Errorf("%w: comparison dependency version", ErrInvalidCanonicalPayload)
	}
	rawItems, ok := payload["items"].([]any)
	if !ok || len(rawItems) < 2 || len(rawItems) > 6 {
		return nil, fmt.Errorf("%w: comparison dependency items", ErrInvalidCanonicalPayload)
	}
	items := make([]ComparisonDependencyItem, 0, len(rawItems))
	for _, raw := range rawItems {
		object, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: comparison dependency item", ErrInvalidCanonicalPayload)
		}
		var item ComparisonDependencyItem
		fields := []struct {
			key   string
			value *string
		}{
			{"original_snapshot_id", &item.OriginalSnapshotID},
			{"copied_snapshot_id", &item.CopiedSnapshotID},
			{"record_id", &item.RecordID},
			{"revision_id", &item.RevisionID},
			{"hash", &item.Hash},
			{"kind", &item.Kind},
		}
		for _, field := range fields {
			if raw, exists := object[field.key]; exists {
				text, valid := raw.(string)
				if !valid {
					return nil, fmt.Errorf("%w: comparison dependency field %s", ErrInvalidCanonicalPayload, field.key)
				}
				*field.value = text
			}
		}
		context, ok := object["revision_context"].(string)
		if !ok {
			return nil, fmt.Errorf("%w: comparison dependency context", ErrInvalidCanonicalPayload)
		}
		item.RevisionContext = RevisionContext(context)
		switch item.RevisionContext {
		case RevisionContextBound:
			if !recordidentity.IsValid(item.RecordID) || !validClosedPreparedID(item.RevisionID, "rrv_") {
				return nil, fmt.Errorf("%w: comparison revision identity", ErrInvalidCanonicalPayload)
			}
		case RevisionContextNotApplicable:
			if item.RecordID != "" || item.RevisionID != "" || item.OriginalSnapshotID == "" {
				return nil, fmt.Errorf("%w: comparison snapshot context", ErrInvalidCanonicalPayload)
			}
		default:
			return nil, fmt.Errorf("%w: comparison dependency context", ErrInvalidCanonicalPayload)
		}
		if item.OriginalSnapshotID != "" {
			if !ValidSnapshotID(item.OriginalSnapshotID) || !ValidSnapshotID(item.CopiedSnapshotID) {
				return nil, fmt.Errorf("%w: comparison snapshot identity", ErrInvalidCanonicalPayload)
			}
			if _, err := ParseKindKey(item.Kind); err != nil {
				return nil, err
			}
		} else if item.CopiedSnapshotID != "" {
			return nil, fmt.Errorf("%w: metadata dependency copy", ErrInvalidCanonicalPayload)
		}
		if item.OriginalSnapshotID == "" && item.Kind != (KindKey{}).String() {
			return nil, fmt.Errorf("%w: metadata dependency kind", ErrInvalidCanonicalPayload)
		}
		var hash [32]byte
		if len(item.Hash) != 64 || strings.ToLower(item.Hash) != item.Hash {
			return nil, fmt.Errorf("%w: comparison dependency hash", ErrInvalidCanonicalPayload)
		}
		if _, err := hex.Decode(hash[:], []byte(item.Hash)); err != nil {
			return nil, fmt.Errorf("%w: comparison dependency hash", ErrInvalidCanonicalPayload)
		}
		items = append(items, item)
	}
	return items, nil
}

// ComparisonDependenciesFromNativeClaims binds every canonical item to already
// verified native claims. The caller must verify the signature and reauthorize
// sources within the revision transaction; this function is not that authority.
// Imported and historical references must not be passed invented native claims.
func ComparisonDependenciesFromNativeClaims(result PreparedComparisonResult, claims ComparisonIntentClaims) ([]ComparisonDependency, error) {
	if result.Validate() != nil || result.snapshotID != DeriveComparisonResultSnapshotID(result.recordID, claims.Digest) {
		return nil, ErrComparisonIntentStale
	}
	snapshot := result.snapshot
	if err := snapshot.Validate(comparisonResultDescriptor()); err != nil {
		return nil, err
	}
	payload, err := decodeSnapshotPayload(snapshot.payload.bytes)
	if err != nil {
		return nil, err
	}
	items, err := extractComparisonDependencyItems(payload)
	if err != nil {
		return nil, err
	}
	if claims.Purpose != ComparisonIntentPurpose || payload["digest"] != claims.Digest || len(items) != len(claims.Items) {
		return nil, ErrComparisonIntentStale
	}
	dependencies := make([]ComparisonDependency, 0, len(items))
	for index, item := range items {
		claim := claims.Items[index]
		if !recordidentity.IsValid(claim.SourceRecordID) ||
			item.OriginalSnapshotID != claim.SnapshotID || item.RecordID != claim.RecordID ||
			item.RevisionID != claim.RevisionID || item.RevisionContext != claim.RevisionContext ||
			item.Hash != claim.Hash || item.Kind != claim.Kind ||
			(item.RecordID != "" && item.RecordID != claim.SourceRecordID) {
			return nil, ErrComparisonIntentStale
		}
		if item.OriginalSnapshotID != "" &&
			item.CopiedSnapshotID != DeriveComparisonCopySnapshotID(result.recordID, item.OriginalSnapshotID, claims.Digest) {
			return nil, ErrComparisonIntentStale
		}
		dependencies = append(dependencies, ComparisonDependency{
			SourceRecordID: claim.SourceRecordID, SourceRevisionID: item.RevisionID,
			SourceSnapshotID: item.OriginalSnapshotID,
		})
	}
	slices.SortFunc(dependencies, compareComparisonDependencies)
	return slices.Compact(dependencies), nil
}

func compareComparisonDependencies(left, right ComparisonDependency) int {
	if order := strings.Compare(left.SourceRecordID, right.SourceRecordID); order != 0 {
		return order
	}
	if order := strings.Compare(left.SourceRevisionID, right.SourceRevisionID); order != 0 {
		return order
	}
	return strings.Compare(left.SourceSnapshotID, right.SourceSnapshotID)
}

// ComparisonDependencyCoverageMatches detects a missing, extra, or ambiguous
// row in a previously certified index without requiring live source objects.
// It checks coverage, not provenance: callers still need a trusted completion
// and an exact result/digest binding before inheriting the index.
func ComparisonDependencyCoverageMatches(items []ComparisonDependencyItem, dependencies []ComparisonDependency) bool {
	if len(items) == 0 || len(dependencies) == 0 || len(dependencies) > len(items) {
		return false
	}
	for index, dependency := range dependencies {
		if !recordidentity.IsValid(dependency.SourceRecordID) ||
			(dependency.SourceRevisionID != "" && !validClosedPreparedID(dependency.SourceRevisionID, "rrv_")) ||
			(dependency.SourceSnapshotID != "" && !ValidSnapshotID(dependency.SourceSnapshotID)) ||
			(index > 0 && compareComparisonDependencies(dependencies[index-1], dependency) >= 0) {
			return false
		}
		matched := false
		for _, item := range items {
			if comparisonDependencyMatchesItem(item, dependency) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, item := range items {
		matches := 0
		for _, dependency := range dependencies {
			if comparisonDependencyMatchesItem(item, dependency) {
				matches++
			}
		}
		if matches != 1 {
			return false
		}
	}
	return true
}

func comparisonDependencyMatchesItem(item ComparisonDependencyItem, dependency ComparisonDependency) bool {
	return item.OriginalSnapshotID == dependency.SourceSnapshotID &&
		item.RevisionID == dependency.SourceRevisionID &&
		(item.RecordID == "" || item.RecordID == dependency.SourceRecordID)
}

var ErrComparisonDependencyIndexIncomplete = errors.New("comparison_dependency_index_incomplete")
