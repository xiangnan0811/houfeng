package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
)

const (
	ComparisonCopyReason            = "explicit comparison copy"
	comparisonCopyIdentityDomain    = "houfeng.evidence.comparison-copy.v1:"
	comparisonResultIdentityDomain  = "houfeng.evidence.comparison-result.v1:"
	comparisonResultRetentionReason = "comparison result metadata"
)

type ComparisonSaveRequest struct {
	Actor    ActorScope
	RecordID string
	Token    string
	Now      time.Time
	// AllowExpiredReplay reconstructs the deterministic save plan for an
	// already-completed Idempotency-Key. The HTTP handler may set this only
	// after peeking a completed claim; expired tokens are not live credentials.
	AllowExpiredReplay bool
}

type PreparedComparisonCopy struct {
	recordID             string
	snapshotID           string
	copiedFromSnapshotID string
	snapshot             CanonicalSnapshot
}

func (copy PreparedComparisonCopy) RecordID() string { return copy.recordID }

func (copy PreparedComparisonCopy) SnapshotID() string { return copy.snapshotID }

func (copy PreparedComparisonCopy) CopiedFromSnapshotID() string { return copy.copiedFromSnapshotID }

func (copy PreparedComparisonCopy) Snapshot() CanonicalSnapshot { return copy.snapshot }

func NewPreparedComparisonCopy(
	recordID, snapshotID, copiedFromSnapshotID string,
	snapshot CanonicalSnapshot,
) (PreparedComparisonCopy, error) {
	copy := PreparedComparisonCopy{
		recordID:             recordID,
		snapshotID:           snapshotID,
		copiedFromSnapshotID: copiedFromSnapshotID,
		snapshot:             snapshot,
	}
	if err := copy.Validate(); err != nil {
		return PreparedComparisonCopy{}, err
	}
	return copy, nil
}

func (copy PreparedComparisonCopy) Validate() error {
	if !validClosedPreparedID(copy.recordID, "rec_") || !ValidSnapshotID(copy.snapshotID) ||
		!ValidSnapshotID(copy.copiedFromSnapshotID) || copy.snapshotID == copy.copiedFromSnapshotID ||
		copy.snapshot.Hash() == [sha256.Size]byte{} || copy.snapshot.Size() == 0 {
		return fmt.Errorf("%w: comparison copy", ErrInvalidRevisionPreparation)
	}
	return nil
}

type PreparedComparisonResult struct {
	recordID   string
	snapshotID string
	snapshot   CanonicalSnapshot
}

func (result PreparedComparisonResult) RecordID() string { return result.recordID }

func (result PreparedComparisonResult) SnapshotID() string { return result.snapshotID }

func (result PreparedComparisonResult) Snapshot() CanonicalSnapshot { return result.snapshot }

func NewPreparedComparisonResult(recordID, snapshotID string, snapshot CanonicalSnapshot) (PreparedComparisonResult, error) {
	result := PreparedComparisonResult{recordID: recordID, snapshotID: snapshotID, snapshot: snapshot}
	if err := result.Validate(); err != nil {
		return PreparedComparisonResult{}, err
	}
	return result, nil
}

func (result PreparedComparisonResult) Empty() bool {
	return result.recordID == "" && result.snapshotID == "" && result.snapshot.Size() == 0
}

func (result PreparedComparisonResult) Validate() error {
	if !validClosedPreparedID(result.recordID, "rec_") || !ValidSnapshotID(result.snapshotID) ||
		result.snapshot.Envelope().Key != ComparisonResultV1Key() ||
		result.snapshot.Hash() == [sha256.Size]byte{} || result.snapshot.Size() == 0 {
		return fmt.Errorf("%w: comparison result", ErrInvalidRevisionPreparation)
	}
	return nil
}

type ComparisonSavePreparation struct {
	Token  string
	Claims ComparisonIntentClaims
	Copies []PreparedComparisonCopy
	Result PreparedComparisonResult
}

func (save ComparisonSavePreparation) Empty() bool {
	return save.Token == "" && len(save.Copies) == 0 && save.Result.Empty()
}

func PrepareComparisonSave(
	ctx context.Context,
	registry Registry,
	source ComparisonSelectionSource,
	signer ComparisonIntentSigner,
	payloads CapturePayloadSink,
	request ComparisonSaveRequest,
) (ComparisonSavePreparation, error) {
	if ctx == nil || source == nil || signer == nil || len(registry.Keys()) == 0 {
		return ComparisonSavePreparation{}, ErrInvalidComparisonSelection
	}
	if !validClosedPreparedID(request.RecordID, "rec_") || request.Token == "" {
		return ComparisonSavePreparation{}, ErrComparisonIntentInvalid
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil || !actorEquals(actor, request.Actor) {
		return ComparisonSavePreparation{}, fmt.Errorf("%w: actor", ErrInvalidComparisonSelection)
	}
	now := request.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	claims, err := signer.Verify(request.Token, now)
	if errors.Is(err, ErrComparisonIntentExpired) {
		if !request.AllowExpiredReplay || claims.Purpose != ComparisonIntentPurpose {
			return ComparisonSavePreparation{}, ErrComparisonIntentExpired
		}
	} else if err != nil {
		return ComparisonSavePreparation{}, err
	}
	actorHash := actor.CanonicalHash()
	if claims.Purpose != ComparisonIntentPurpose ||
		claims.ActorHash != hex.EncodeToString(actorHash[:]) ||
		claims.ProjectID != string(actor.ProjectID) {
		return ComparisonSavePreparation{}, ErrComparisonIntentInvalid
	}
	evaluated, err := reevaluateComparisonClaims(ctx, registry, source, actor, claims, now)
	if err != nil {
		return ComparisonSavePreparation{}, err
	}
	warningsDigest := comparisonWarningsDigest(evaluated.Review)
	if hex.EncodeToString(evaluated.Digest[:]) != claims.Digest ||
		hex.EncodeToString(warningsDigest[:]) != claims.WarningsDigest ||
		!evaluated.SaveEligibility.Eligible {
		return ComparisonSavePreparation{}, ErrComparisonIntentStale
	}

	copies := make([]PreparedComparisonCopy, 0, len(evaluated.Items))
	originalToCopied := make(map[string]string, len(evaluated.Items))
	for _, item := range evaluated.Items {
		if item.SnapshotID == "" {
			continue
		}
		loaded, ok := evaluated.loaded[item.SnapshotID]
		if !ok || loaded.Unreadable || loaded.Snapshot.Size() == 0 {
			return ComparisonSavePreparation{}, ErrComparisonIntentStale
		}
		copiedID := DeriveComparisonCopySnapshotID(request.RecordID, item.SnapshotID, claims.Digest)
		copy := PreparedComparisonCopy{
			recordID:             request.RecordID,
			snapshotID:           copiedID,
			copiedFromSnapshotID: item.SnapshotID,
			snapshot:             loaded.Snapshot,
		}
		if err := copy.Validate(); err != nil {
			return ComparisonSavePreparation{}, err
		}
		copies = append(copies, copy)
		originalToCopied[item.SnapshotID] = copiedID
	}

	kind, err := NewComparisonResultKind()
	if err != nil {
		return ComparisonSavePreparation{}, err
	}
	resultSnapshot, err := buildComparisonResultSnapshot(kind, evaluated, claims, originalToCopied, now)
	if err != nil {
		return ComparisonSavePreparation{}, err
	}
	if payloads != nil {
		if err := payloads.PersistCapturePayload(ctx, resultSnapshot); err != nil {
			return ComparisonSavePreparation{}, fmt.Errorf("persist comparison result payload: %w", err)
		}
	}
	result := PreparedComparisonResult{
		recordID:   request.RecordID,
		snapshotID: DeriveComparisonResultSnapshotID(request.RecordID, claims.Digest),
		snapshot:   resultSnapshot,
	}
	if err := result.Validate(); err != nil {
		return ComparisonSavePreparation{}, err
	}
	return ComparisonSavePreparation{
		Token:  request.Token,
		Claims: claims,
		Copies: copies,
		Result: result,
	}, nil
}

func DeriveComparisonCopySnapshotID(recordID, sourceSnapshotID, digestHex string) string {
	sum := sha256.Sum256([]byte(comparisonCopyIdentityDomain + recordID + "|" + sourceSnapshotID + "|" + digestHex))
	return "evs_" + hex.EncodeToString(sum[:16])
}

func DeriveComparisonResultSnapshotID(recordID, digestHex string) string {
	sum := sha256.Sum256([]byte(comparisonResultIdentityDomain + recordID + "|" + digestHex))
	return "evs_" + hex.EncodeToString(sum[:16])
}

type comparisonSaveEvaluation struct {
	ComparisonEvaluateOutput
	loaded    map[string]ComparisonLoadedSnapshot
	revisions map[ComparisonRevisionKey]ComparisonLoadedRevision
}

func reevaluateComparisonClaims(
	ctx context.Context,
	registry Registry,
	source ComparisonSelectionSource,
	actor ActorScope,
	claims ComparisonIntentClaims,
	now time.Time,
) (comparisonSaveEvaluation, error) {
	items, err := comparisonFixedItemsFromClaims(claims)
	if err != nil {
		return comparisonSaveEvaluation{}, err
	}
	window, err := parseComparisonClaimWindow(claims)
	if err != nil {
		return comparisonSaveEvaluation{}, err
	}
	var bucket *time.Duration
	if claims.BucketSeconds != nil {
		value := time.Duration(*claims.BucketSeconds) * time.Second
		bucket = &value
	}
	evaluated, err := ResolveFixedComparison(ctx, registry, source, nil, ComparisonEvaluateRequest{
		Actor:           actor,
		Items:           items,
		BaselineIndex:   claims.BaselineIndex,
		Alignment:       claims.Alignment,
		RequestedWindow: window,
		Tolerance:       time.Duration(claims.ToleranceSeconds) * time.Second,
		BucketWidth:     bucket,
		Detail:          comparisonDetailFromClaims(claims),
		Now:             now,
	})
	if err != nil {
		if errors.Is(err, ErrComparisonSelectionNotFound) || errors.Is(err, ErrComparisonSelectionIncomplete) {
			return comparisonSaveEvaluation{}, ErrComparisonIntentStale
		}
		return comparisonSaveEvaluation{}, err
	}
	snapshotIDs := make([]string, 0, len(evaluated.Items))
	for _, item := range evaluated.Items {
		if item.SnapshotID == "" {
			continue
		}
		snapshotIDs = append(snapshotIDs, item.SnapshotID)
	}
	loaded, err := source.LoadComparisonSnapshots(ctx, actor.Clone(), snapshotIDs)
	if err != nil {
		return comparisonSaveEvaluation{}, err
	}
	revisions := map[ComparisonRevisionKey]ComparisonLoadedRevision{}
	revisionKeys := make([]ComparisonRevisionKey, 0)
	for _, item := range items {
		if item.Revision == nil {
			continue
		}
		revisionKeys = append(revisionKeys, ComparisonRevisionKey{RecordID: item.Revision.RecordID, RevisionID: item.Revision.RevisionID})
	}
	if len(revisionKeys) > 0 {
		revisions, err = source.LoadComparisonRevisions(ctx, actor.Clone(), revisionKeys)
		if err != nil {
			return comparisonSaveEvaluation{}, err
		}
	}
	return comparisonSaveEvaluation{ComparisonEvaluateOutput: evaluated, loaded: loaded, revisions: revisions}, nil
}

func comparisonFixedItemsFromClaims(claims ComparisonIntentClaims) ([]ComparisonFixedItem, error) {
	items := make([]ComparisonFixedItem, 0, len(claims.Items))
	for _, item := range claims.Items {
		if item.RecordID != "" || item.RevisionID != "" {
			if !validClosedPreparedID(item.RecordID, "rec_") || !validClosedPreparedID(item.RevisionID, "rrv_") {
				return nil, ErrComparisonIntentStale
			}
			revision := ComparisonFixedRevision{RecordID: item.RecordID, RevisionID: item.RevisionID}
			if item.SnapshotID != "" {
				if !ValidSnapshotID(item.SnapshotID) {
					return nil, ErrComparisonIntentStale
				}
				revision.ChosenSnapshotIDs = []string{item.SnapshotID}
			} else if len(item.ChosenSnapshotIDs) > 0 {
				revision.ChosenSnapshotIDs = append([]string(nil), item.ChosenSnapshotIDs...)
			}
			items = append(items, ComparisonFixedItem{Revision: &revision})
			continue
		}
		if item.SnapshotID == "" || !ValidSnapshotID(item.SnapshotID) {
			return nil, ErrComparisonIntentStale
		}
		snapshotID := item.SnapshotID
		items = append(items, ComparisonFixedItem{SnapshotID: &snapshotID})
	}
	if len(items) < 2 {
		return nil, ErrComparisonIntentStale
	}
	return items, nil
}

func comparisonDetailFromClaims(claims ComparisonIntentClaims) *ComparisonDetail {
	if claims.DetailKind == "" {
		return nil
	}
	return &ComparisonDetail{
		Kind:   KindKey{Kind: KindName(claims.DetailKind), SchemaVersion: SchemaVersion(claims.DetailSchemaVersion)},
		Metric: claims.DetailMetric,
	}
}

func parseComparisonClaimWindow(claims ComparisonIntentClaims) (TimeWindow, error) {
	start, err := time.Parse(time.RFC3339Nano, claims.RequestedStart)
	if err != nil {
		return TimeWindow{}, ErrComparisonIntentStale
	}
	end, err := time.Parse(time.RFC3339Nano, claims.RequestedEnd)
	if err != nil {
		return TimeWindow{}, ErrComparisonIntentStale
	}
	return TimeWindow{Start: start.UTC(), End: end.UTC()}, nil
}

func buildComparisonResultSnapshot(
	kind *ComparisonResultKind,
	evaluated comparisonSaveEvaluation,
	claims ComparisonIntentClaims,
	originalToCopied map[string]string,
	now time.Time,
) (CanonicalSnapshot, error) {
	if len(evaluated.Items) == 0 || claims.BaselineIndex < 0 || claims.BaselineIndex >= len(evaluated.Items) {
		return CanonicalSnapshot{}, ErrComparisonIntentStale
	}
	window, err := parseComparisonClaimWindow(claims)
	if err != nil {
		return CanonicalSnapshot{}, err
	}
	envelope, err := comparisonResultEnvelope(evaluated, claims, window, now)
	if err != nil {
		return CanonicalSnapshot{}, err
	}

	payload := comparisonResultCanonicalPayload(evaluated, claims, originalToCopied)
	snapshot, _, err := NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, RedactionNormalOnly)
	if err != nil {
		return CanonicalSnapshot{}, err
	}
	return snapshot, nil
}

func comparisonResultCanonicalPayload(
	evaluated comparisonSaveEvaluation,
	claims ComparisonIntentClaims,
	originalToCopied map[string]string,
) map[string]any {
	items := make([]any, 0, len(evaluated.Items))
	for _, item := range evaluated.Items {
		encoded := map[string]any{
			"original_snapshot_id": item.SnapshotID,
			"hash":                 hex.EncodeToString(item.Hash[:]),
			"kind":                 item.Kind.String(),
			"revision_context":     string(item.RevisionContext),
		}
		if copied := originalToCopied[item.SnapshotID]; copied != "" {
			encoded["copied_snapshot_id"] = copied
		}
		if item.RecordID != "" {
			encoded["record_id"] = item.RecordID
		}
		if item.RevisionID != "" {
			encoded["revision_id"] = item.RevisionID
		}
		if item.Revision != nil && item.RevisionContext == RevisionContextBound {
			encoded["record_type"] = item.Revision.RecordType
			encoded["business_status"] = item.Revision.BusinessStatus
			encoded["status_group"] = item.Revision.StatusGroup
			encoded["impact_level"] = item.Revision.ImpactLevel
			if item.Revision.HasOccurredAt {
				encoded["occurred_at"] = item.Revision.OccurredAt.UTC().Format(time.RFC3339Nano)
			}
		}
		items = append(items, encoded)
	}
	warnings := make([]any, 0, len(evaluated.Review))
	for _, finding := range evaluated.Review {
		encoded := map[string]any{"reason": string(finding.Reason)}
		if finding.Kind.Kind != "" {
			encoded["kind"] = finding.Kind.String()
		}
		if finding.ItemIndex != 0 || finding.Kind.Kind != "" {
			encoded["item_index"] = finding.ItemIndex
		}
		warnings = append(warnings, encoded)
	}
	differences := make([]any, 0, len(evaluated.Pairwise))
	for _, pairwise := range evaluated.Pairwise {
		differences = append(differences, comparisonDifferenceFromPairwise(pairwise))
	}
	kinds := make([]any, 0, len(evaluated.AvailableKinds))
	for _, key := range evaluated.AvailableKinds {
		kinds = append(kinds, key.String())
	}
	payload := map[string]any{
		"version":             "comparison_result/v1",
		"baseline_index":      claims.BaselineIndex,
		"alignment":           string(claims.Alignment),
		"requested_from":      claims.RequestedStart,
		"requested_to":        claims.RequestedEnd,
		"tolerance_seconds":   claims.ToleranceSeconds,
		"digest":              claims.Digest,
		"registry_version":    claims.RegistryVersion,
		"calculation_version": claims.CalculationVersion,
		"items":               items,
		"warnings":            warnings,
		"system_differences":  differences,
		"available_kinds":     kinds,
	}
	if claims.BucketSeconds != nil {
		payload["bucket_seconds"] = *claims.BucketSeconds
	}
	return payload
}

func comparisonDifferenceFromPairwise(pairwise Comparison) map[string]any {
	encoded := map[string]any{
		"item_index": pairwise.ItemIndex,
		"kind":       pairwise.Key.String(),
		"compatible": pairwise.Compatible,
		"reason":     pairwise.Reason,
		"equal":      pairwise.Values["equal"] == true,
	}
	if left := stringValue(pairwise.Values["left_hash"]); left != "" {
		encoded["left_hash"] = left
	}
	if right := stringValue(pairwise.Values["right_hash"]); right != "" {
		encoded["right_hash"] = right
	}
	if matched, ok := numberValue(pairwise.Values["matched"]); ok {
		encoded["matched"] = int64(matched)
	}
	if unmatched, ok := numberValue(pairwise.Values["unmatched_baseline"]); ok {
		encoded["unmatched_baseline"] = int64(unmatched)
	}
	if unmatched, ok := numberValue(pairwise.Values["unmatched_item"]); ok {
		encoded["unmatched_item"] = int64(unmatched)
	}
	if deltas, ok := pairwise.Values["deltas"].([]any); ok && len(deltas) > 0 {
		encoded["deltas"] = deltas
	}
	return encoded
}

func comparisonResultEnvelope(
	evaluated comparisonSaveEvaluation,
	claims ComparisonIntentClaims,
	window TimeWindow,
	now time.Time,
) (SnapshotEnvelope, error) {
	authorization, err := pickComparisonResultAuthorization(evaluated)
	if err != nil {
		return SnapshotEnvelope{}, err
	}
	observed := window.End
	captured := now.UTC()
	if captured.Before(observed) {
		captured = observed
	}
	source := IdentitySnapshot{
		Type:   string(authorization.Kind),
		ID:     authorization.SourceID,
		Fields: map[string]string{"display_name": "comparison result source"},
	}
	return SnapshotEnvelope{
		Key:                ComparisonResultV1Key(),
		Subject:            IdentitySnapshot{Type: "comparison", ID: "comparison_result", Fields: map[string]string{"display_name": "Comparison result"}},
		Source:             source,
		Authorization:      authorization,
		RequestedWindow:    window,
		ActualWindow:       window,
		ObservedAt:         observed,
		CapturedAt:         captured,
		ReferencedAt:       captured,
		SourceRevision:     claims.Digest,
		SourceWatermark:    captured.Format(time.RFC3339Nano),
		SourceDigest:       evaluated.Digest,
		ProducerVersion:    comparisonResultProducerVersion,
		CalculationVersion: ComparisonCalculationVersion,
		Units:              UnitsSemantics{Status: UnitsNotApplicable, Reason: comparisonResultRetentionReason},
		Quality:            Quality{Status: QualityComplete, SampleCount: uint64(len(evaluated.Items))},
		Sensitivity:        SensitivityNormal,
		ActualPrecision:    DurationSemantics{Applicable: false, Reason: comparisonResultRetentionReason},
		BucketWidth:        DurationSemantics{Applicable: false, Reason: comparisonResultRetentionReason},
		QuotaOutcome:       QuotaOutcome{Status: QuotaAllowed},
		Retention: RetentionSemantics{
			Immutable: true, Scope: RetentionScopeRecordRevision, SourceDeletion: SourceDeletionSnapshotRetained,
		},
	}, nil
}

func pickComparisonResultAuthorization(evaluated comparisonSaveEvaluation) (AuthorizationScope, error) {
	type candidate struct {
		key  string
		auth AuthorizationScope
	}
	candidates := make([]candidate, 0)
	for _, loaded := range evaluated.loaded {
		if loaded.SourceAuthorization.SourceID == "" {
			continue
		}
		candidates = append(candidates, candidate{
			key:  string(loaded.SourceAuthorization.Kind) + "\x00" + loaded.SourceAuthorization.SourceID,
			auth: loaded.SourceAuthorization,
		})
	}
	for _, revision := range evaluated.revisions {
		for _, source := range revision.RecordScope.Sources {
			if source.SourceID == "" {
				continue
			}
			candidates = append(candidates, candidate{
				key:  string(source.Kind) + "\x00" + source.SourceID,
				auth: source,
			})
		}
	}
	if len(candidates) == 0 {
		return AuthorizationScope{}, ErrComparisonIntentStale
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].key < candidates[right].key })
	return candidates[0].auth, nil
}

func actorEquals(left, right ActorScope) bool {
	return left.CanonicalHash() == right.CanonicalHash()
}

func NewRevisionPreparationFromComparisonSave(
	recordID string,
	save ComparisonSavePreparation,
) (RevisionPreparation, error) {
	ordered := make([]string, 0, len(save.Copies)+1)
	for _, copy := range save.Copies {
		ordered = append(ordered, copy.SnapshotID())
	}
	if !save.Result.Empty() {
		ordered = append(ordered, save.Result.SnapshotID())
	}
	return NewRevisionPreparation(recordID, RevisionPreparationValues{
		OrderedSnapshotIDs: ordered,
		ComparisonSave:     save,
	})
}
