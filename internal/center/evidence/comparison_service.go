package evidence

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
)

type ComparisonFixedRevision struct {
	RecordID          string
	RevisionID        string
	ChosenSnapshotIDs []string
}

type ComparisonFixedItem struct {
	SnapshotID *string
	Revision   *ComparisonFixedRevision
}

type ComparisonEvaluateRequest struct {
	Actor           ActorScope
	Items           []ComparisonFixedItem
	BaselineIndex   int
	Alignment       CoverageAlignment
	RequestedWindow TimeWindow
	Tolerance       time.Duration
	BucketWidth     *time.Duration
	Detail          *ComparisonDetail
	Now             time.Time
}

type ComparisonRevisionKey struct {
	RecordID   string
	RevisionID string
}

type ComparisonLoadedSnapshot struct {
	SnapshotID          string
	RecordID            string
	Kind                KindKey
	Hash                [sha256.Size]byte
	Snapshot            CanonicalSnapshot
	RecordScope         recordauth.ResourceScope
	SourceAuthorization recordauth.SourceAuthorization
	SourceAvailable     bool
	Unreadable          bool
}

type ComparisonLoadedRevision struct {
	RecordID    string
	RevisionID  string
	Metadata    RevisionMetadataSnapshot
	RecordScope recordauth.ResourceScope
	SnapshotIDs []string
}

type ComparisonSaveEligibility struct {
	Eligible bool
	Blockers []ComparisonReason
}

type ComparisonEvaluateOutput struct {
	Digest          [sha256.Size]byte
	Items           []ResolvedComparisonItem
	Review          []ComparabilityFinding
	Pairwise        []Comparison
	Series          []Series
	AvailableKinds  []KindKey
	SaveEligibility ComparisonSaveEligibility
	Intent          *ComparisonIntent
}

type ComparisonSelectionSource interface {
	LoadComparisonSnapshots(context.Context, ActorScope, []string) (map[string]ComparisonLoadedSnapshot, error)
	LoadComparisonRevisions(context.Context, ActorScope, []ComparisonRevisionKey) (map[ComparisonRevisionKey]ComparisonLoadedRevision, error)
}

func ResolveFixedComparison(
	ctx context.Context,
	registry Registry,
	source ComparisonSelectionSource,
	signer ComparisonIntentSigner,
	request ComparisonEvaluateRequest,
) (ComparisonEvaluateOutput, error) {
	if ctx == nil || source == nil || len(registry.Keys()) == 0 {
		return ComparisonEvaluateOutput{}, ErrInvalidComparisonSelection
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return ComparisonEvaluateOutput{}, fmt.Errorf("%w: actor", ErrInvalidComparisonSelection)
	}
	normalized, snapshotIDs, revisionKeys, err := normalizeFixedComparisonItems(request)
	if err != nil {
		return ComparisonEvaluateOutput{}, err
	}
	revisions := map[ComparisonRevisionKey]ComparisonLoadedRevision{}
	if len(revisionKeys) > 0 {
		revisions, err = source.LoadComparisonRevisions(ctx, actor.Clone(), append([]ComparisonRevisionKey(nil), revisionKeys...))
		if err != nil {
			return ComparisonEvaluateOutput{}, err
		}
	}
	for _, key := range revisionKeys {
		revision, ok := revisions[key]
		if !ok || !authorizeComparisonRecord(actor, revision.RecordScope) {
			return ComparisonEvaluateOutput{}, ErrComparisonSelectionNotFound
		}
		for _, snapshotID := range revision.SnapshotIDs {
			if ValidSnapshotID(snapshotID) {
				snapshotIDs = append(snapshotIDs, snapshotID)
			}
		}
		for _, snapshotID := range chosenSnapshotIDs(normalized, key) {
			snapshotIDs = append(snapshotIDs, snapshotID)
		}
	}
	snapshots := map[string]ComparisonLoadedSnapshot{}
	if len(snapshotIDs) > 0 {
		snapshots, err = source.LoadComparisonSnapshots(ctx, actor.Clone(), uniqueSnapshotIDs(snapshotIDs))
		if err != nil {
			return ComparisonEvaluateOutput{}, err
		}
	}
	inputs := make([]ComparisonItemInput, 0, len(normalized))
	available := make(map[KindKey]struct{})
	for _, item := range normalized {
		resolved, err := resolveFixedComparisonItem(actor, item, revisions, snapshots, request.Detail)
		if err != nil {
			return ComparisonEvaluateOutput{}, err
		}
		if resolved.input.Kind.Kind != "" {
			available[resolved.input.Kind] = struct{}{}
		}
		for _, extra := range resolved.extraKinds {
			available[extra] = struct{}{}
		}
		inputs = append(inputs, resolved.input)
	}
	evaluated, err := EvaluateComparison(registry, ComparisonEvaluateInput{
		Items:           inputs,
		BaselineIndex:   request.BaselineIndex,
		Alignment:       request.Alignment,
		RequestedWindow: normalizeWindow(request.RequestedWindow),
		Tolerance:       request.Tolerance,
		Detail:          request.Detail,
	})
	if err != nil {
		return ComparisonEvaluateOutput{}, err
	}
	eligibility := comparisonSaveEligibility(evaluated.Review)
	output := ComparisonEvaluateOutput{
		Digest:          evaluated.Digest,
		Items:           evaluated.Items,
		Review:          evaluated.Review,
		Pairwise:        evaluated.Pairwise,
		Series:          evaluated.Series,
		AvailableKinds:  sortedKindKeys(available),
		SaveEligibility: eligibility,
	}
	if !eligibility.Eligible || signer == nil {
		return output, nil
	}
	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}
	intent, signErr := signer.Sign(BuildComparisonIntentClaims(ComparisonIntentClaimsInput{
		Actor:           actor,
		Items:           evaluated.Items,
		BaselineIndex:   request.BaselineIndex,
		Alignment:       request.Alignment,
		RequestedWindow: normalizeWindow(request.RequestedWindow),
		Tolerance:       request.Tolerance,
		BucketWidth:     request.BucketWidth,
		Digest:          evaluated.Digest,
		Review:          evaluated.Review,
		Detail:          request.Detail,
		Now:             now.UTC(),
	}))
	if signErr != nil {
		return ComparisonEvaluateOutput{}, fmt.Errorf("%w: sign comparison intent", ErrComparisonIntentUnavailable)
	}
	output.Intent = &intent
	return output, nil
}

type normalizedFixedItem struct {
	snapshotID *string
	revision   *ComparisonFixedRevision
}

type resolvedFixedItem struct {
	input      ComparisonItemInput
	extraKinds []KindKey
}

func normalizeFixedComparisonItems(request ComparisonEvaluateRequest) ([]normalizedFixedItem, []string, []ComparisonRevisionKey, error) {
	if len(request.Items) < 2 || len(request.Items) > 6 ||
		request.BaselineIndex < 0 || request.BaselineIndex >= len(request.Items) ||
		(request.Alignment != CoverageActual && request.Alignment != CoverageCommonOverlap) {
		return nil, nil, nil, fmt.Errorf("%w: item count or baseline", ErrInvalidComparisonSelection)
	}
	window := normalizeWindow(request.RequestedWindow)
	if window.Start.IsZero() || !window.End.After(window.Start) {
		return nil, nil, nil, fmt.Errorf("%w: requested window", ErrInvalidComparisonSelection)
	}
	normalized := make([]normalizedFixedItem, 0, len(request.Items))
	snapshotIDs := make([]string, 0, len(request.Items))
	revisionKeys := make([]ComparisonRevisionKey, 0)
	for _, item := range request.Items {
		hasSnapshot := item.SnapshotID != nil
		hasRevision := item.Revision != nil
		if hasSnapshot == hasRevision {
			return nil, nil, nil, fmt.Errorf("%w: snapshot xor revision", ErrInvalidComparisonSelection)
		}
		if hasSnapshot {
			snapshotID := *item.SnapshotID
			if !ValidSnapshotID(snapshotID) {
				return nil, nil, nil, fmt.Errorf("%w: snapshot", ErrInvalidComparisonSelection)
			}
			snapshotIDs = append(snapshotIDs, snapshotID)
			normalized = append(normalized, normalizedFixedItem{snapshotID: &snapshotID})
			continue
		}
		revision := *item.Revision
		if !validClosedPreparedID(revision.RecordID, "rec_") || !validClosedPreparedID(revision.RevisionID, "rrv_") {
			return nil, nil, nil, fmt.Errorf("%w: revision", ErrInvalidComparisonSelection)
		}
		chosen := make([]string, 0, len(revision.ChosenSnapshotIDs))
		seen := make(map[string]struct{}, len(revision.ChosenSnapshotIDs))
		for _, snapshotID := range revision.ChosenSnapshotIDs {
			if !ValidSnapshotID(snapshotID) {
				return nil, nil, nil, fmt.Errorf("%w: chosen snapshot", ErrInvalidComparisonSelection)
			}
			if _, exists := seen[snapshotID]; exists {
				return nil, nil, nil, fmt.Errorf("%w: duplicate chosen snapshot", ErrInvalidComparisonSelection)
			}
			seen[snapshotID] = struct{}{}
			chosen = append(chosen, snapshotID)
		}
		revision.ChosenSnapshotIDs = chosen
		revisionKeys = append(revisionKeys, ComparisonRevisionKey{RecordID: revision.RecordID, RevisionID: revision.RevisionID})
		normalized = append(normalized, normalizedFixedItem{revision: &revision})
	}
	return normalized, snapshotIDs, revisionKeys, nil
}

func resolveFixedComparisonItem(
	actor ActorScope,
	item normalizedFixedItem,
	revisions map[ComparisonRevisionKey]ComparisonLoadedRevision,
	snapshots map[string]ComparisonLoadedSnapshot,
	detail *ComparisonDetail,
) (resolvedFixedItem, error) {
	if item.snapshotID != nil {
		loaded, ok := snapshots[*item.snapshotID]
		if !ok || loaded.SnapshotID != *item.snapshotID || !authorizeComparisonCandidate(actor, loaded.RecordScope, loaded.SourceAuthorization) {
			return resolvedFixedItem{}, ErrComparisonSelectionNotFound
		}
		return resolvedFixedItem{input: comparisonItemFromSnapshot(loaded, RevisionContextNotApplicable, nil)}, nil
	}
	key := ComparisonRevisionKey{RecordID: item.revision.RecordID, RevisionID: item.revision.RevisionID}
	revision, ok := revisions[key]
	if !ok || !authorizeComparisonRecord(actor, revision.RecordScope) {
		return resolvedFixedItem{}, ErrComparisonSelectionNotFound
	}
	selected, extra, err := selectRevisionSnapshots(*item.revision, revision, snapshots, actor, detail)
	if err != nil {
		return resolvedFixedItem{}, err
	}
	if selected.SnapshotID == "" {
		input := ComparisonItemInput{
			RevisionContext: RevisionContextBound,
			Revision:        cloneRevisionMetadataSnapshot(&revision.Metadata),
			RecordID:        item.revision.RecordID,
			RevisionID:      item.revision.RevisionID,
			Reasons:         []ComparisonReason{ReasonMetadataOnly},
		}
		if len(revision.RecordScope.Sources) > 0 {
			input.SubjectKind = string(revision.RecordScope.Sources[0].Kind)
			input.SubjectID = revision.RecordScope.Sources[0].SourceID
		}
		return resolvedFixedItem{input: input}, nil
	}
	input := comparisonItemFromSnapshot(selected, RevisionContextBound, &revision.Metadata)
	input.RecordID = item.revision.RecordID
	input.RevisionID = item.revision.RevisionID
	return resolvedFixedItem{input: input, extraKinds: extra}, nil
}

func selectRevisionSnapshots(
	request ComparisonFixedRevision,
	revision ComparisonLoadedRevision,
	snapshots map[string]ComparisonLoadedSnapshot,
	actor ActorScope,
	detail *ComparisonDetail,
) (ComparisonLoadedSnapshot, []KindKey, error) {
	allowed := make(map[string]struct{}, len(revision.SnapshotIDs))
	for _, snapshotID := range revision.SnapshotIDs {
		allowed[snapshotID] = struct{}{}
	}
	wanted := append([]string(nil), revision.SnapshotIDs...)
	if len(request.ChosenSnapshotIDs) > 0 {
		wanted = append([]string(nil), request.ChosenSnapshotIDs...)
	}
	loaded := make([]ComparisonLoadedSnapshot, 0, len(wanted))
	byKind := make(map[KindKey][]ComparisonLoadedSnapshot)
	for _, snapshotID := range wanted {
		if _, ok := allowed[snapshotID]; !ok {
			return ComparisonLoadedSnapshot{}, nil, fmt.Errorf("%w: chosen snapshot", ErrInvalidComparisonSelection)
		}
		snapshot, ok := snapshots[snapshotID]
		if !ok || !authorizeComparisonCandidate(actor, snapshot.RecordScope, snapshot.SourceAuthorization) {
			return ComparisonLoadedSnapshot{}, nil, ErrComparisonSelectionNotFound
		}
		loaded = append(loaded, snapshot)
		byKind[snapshot.Kind] = append(byKind[snapshot.Kind], snapshot)
	}
	if len(loaded) == 0 {
		return ComparisonLoadedSnapshot{}, nil, nil
	}
	for _, group := range byKind {
		if len(group) > 1 {
			return ComparisonLoadedSnapshot{}, nil, ErrComparisonSelectionIncomplete
		}
	}
	extra := make([]KindKey, 0, len(byKind))
	for key := range byKind {
		extra = append(extra, key)
	}
	if detail != nil {
		if group := byKind[detail.Kind]; len(group) == 1 {
			return group[0], extra, nil
		}
	}
	sort.Slice(loaded, func(i, j int) bool {
		if loaded[i].Kind != loaded[j].Kind {
			return loaded[i].Kind.String() < loaded[j].Kind.String()
		}
		return loaded[i].SnapshotID < loaded[j].SnapshotID
	})
	return loaded[0], extra, nil
}

func comparisonItemFromSnapshot(
	loaded ComparisonLoadedSnapshot,
	context RevisionContext,
	revision *RevisionMetadataSnapshot,
) ComparisonItemInput {
	reasons := make([]ComparisonReason, 0, 3)
	if loaded.Unreadable || loaded.Hash != loaded.Snapshot.Hash() {
		reasons = append(reasons, ReasonSnapshotUnreadable)
	}
	if loaded.SourceAuthorization.State == recordauth.SourceStateTombstoned {
		reasons = append(reasons, ReasonSourceTombstoned)
	}
	if !loaded.SourceAvailable {
		reasons = append(reasons, ReasonSourceUnavailable)
	}
	item := ComparisonItemInput{
		SnapshotID:      loaded.SnapshotID,
		Hash:            loaded.Hash,
		Kind:            loaded.Kind,
		RevisionContext: context,
		Revision:        cloneRevisionMetadataSnapshot(revision),
		SubjectKind:     string(loaded.SourceAuthorization.Kind),
		SubjectID:       loaded.SourceAuthorization.SourceID,
		Snapshot:        loaded.Snapshot,
		Reasons:         reasons,
	}
	if context != RevisionContextBound {
		item.Revision = nil
	}
	return item
}

func comparisonSaveEligibility(review []ComparabilityFinding) ComparisonSaveEligibility {
	blockers := make([]ComparisonReason, 0)
	seen := make(map[ComparisonReason]struct{})
	for _, finding := range review {
		if finding.Reason != ReasonSnapshotUnreadable {
			continue
		}
		if _, exists := seen[finding.Reason]; exists {
			continue
		}
		seen[finding.Reason] = struct{}{}
		blockers = append(blockers, finding.Reason)
	}
	return ComparisonSaveEligibility{Eligible: len(blockers) == 0, Blockers: blockers}
}

func authorizeComparisonRecord(actor ActorScope, recordScope recordauth.ResourceScope) bool {
	if recordScope.Version == 0 {
		return false
	}
	if err := recordauth.Authorize(actor, recordauth.CapabilityComparisonRead, recordScope); err != nil {
		return false
	}
	return recordauth.Authorize(actor, recordauth.CapabilityEvidenceRead, recordScope) == nil
}

func chosenSnapshotIDs(items []normalizedFixedItem, key ComparisonRevisionKey) []string {
	for _, item := range items {
		if item.revision != nil && item.revision.RecordID == key.RecordID && item.revision.RevisionID == key.RevisionID {
			return append([]string(nil), item.revision.ChosenSnapshotIDs...)
		}
	}
	return nil
}

func uniqueSnapshotIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func sortedKindKeys(values map[KindKey]struct{}) []KindKey {
	out := make([]KindKey, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}
