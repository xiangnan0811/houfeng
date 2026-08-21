package evidence

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/recordauth"
)

const RecommendationNearestWindow = "nearest_window"

var ErrComparisonSubjectNotFound = errors.New("comparison subject not found")

type ComparisonSubjectRef struct {
	Kind string
	ID   string
}

type ComparisonCandidateRequest struct {
	Actor           ActorScope
	Subjects        []ComparisonSubjectRef
	RequestedWindow TimeWindow
	Kinds           []KindKey
}

type ComparisonCandidateRef struct {
	Subject             ComparisonSubjectRef
	SnapshotID          string
	RecordID            string
	RevisionIDs         []string
	Kind                KindKey
	CanonicalHash       [32]byte
	RequestedWindow     TimeWindow
	ActualWindow        TimeWindow
	Quality             Quality
	CapturedAt          time.Time
	SourceAuthorization recordauth.SourceAuthorization
}

type ComparisonCandidate struct {
	Subject         ComparisonSubjectRef
	SnapshotID      string
	RecordID        string
	RevisionIDs     []string
	Kind            KindKey
	CanonicalHash   [32]byte
	RequestedWindow TimeWindow
	ActualWindow    TimeWindow
	Quality         Quality
	CapturedAt      time.Time
	Recommendation  string
}

type ComparisonCandidateResult struct {
	Subjects   []ComparisonSubjectRef
	Candidates []ComparisonCandidate
}

type ComparisonSubjectResolver interface {
	ResolveLiveSubject(context.Context, ActorScope, ComparisonSubjectRef) error
}

type ComparisonCandidateSource interface {
	ListComparisonCandidateRefs(context.Context, []ComparisonSubjectRef, TimeWindow, []KindKey) ([]ComparisonCandidateRef, error)
}

type ComparisonRecordScopeSource interface {
	ResolveComparisonRecordScope(context.Context, ActorScope, string) (recordauth.ResourceScope, error)
}

func ResolveComparisonCandidates(
	ctx context.Context,
	registry Registry,
	subjects ComparisonSubjectResolver,
	source ComparisonCandidateSource,
	records ComparisonRecordScopeSource,
	request ComparisonCandidateRequest,
) (ComparisonCandidateResult, error) {
	if ctx == nil || subjects == nil || source == nil || records == nil || len(registry.Keys()) == 0 {
		return ComparisonCandidateResult{}, ErrInvalidComparisonSelection
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return ComparisonCandidateResult{}, fmt.Errorf("%w: actor", ErrInvalidComparisonSelection)
	}
	normalizedSubjects, err := normalizeComparisonSubjects(request.Subjects)
	if err != nil {
		return ComparisonCandidateResult{}, err
	}
	window := normalizeWindow(request.RequestedWindow)
	if window.Start.IsZero() || !window.End.After(window.Start) {
		return ComparisonCandidateResult{}, fmt.Errorf("%w: requested window", ErrInvalidComparisonSelection)
	}
	kinds, err := normalizeComparisonKindFilter(registry, request.Kinds)
	if err != nil {
		return ComparisonCandidateResult{}, err
	}
	for _, subject := range normalizedSubjects {
		if err := subjects.ResolveLiveSubject(ctx, actor.Clone(), subject); err != nil {
			if errors.Is(err, ErrInvalidComparisonSelection) {
				return ComparisonCandidateResult{}, err
			}
			return ComparisonCandidateResult{}, ErrComparisonSubjectNotFound
		}
	}
	refs, err := source.ListComparisonCandidateRefs(ctx, append([]ComparisonSubjectRef(nil), normalizedSubjects...), window, append([]KindKey(nil), kinds...))
	if err != nil {
		return ComparisonCandidateResult{}, err
	}
	scopes := make(map[string]recordauth.ResourceScope)
	candidates := make([]ComparisonCandidate, 0, len(refs))
	subjectIndex := comparisonSubjectIndex(normalizedSubjects)
	for _, ref := range refs {
		if _, known := subjectIndex[ref.Subject]; !known || !ValidSnapshotID(ref.SnapshotID) || !validClosedPreparedID(ref.RecordID, "rec_") {
			continue
		}
		if len(kinds) > 0 && !comparisonKindAllowed(kinds, ref.Kind) {
			continue
		}
		if _, err := registry.LookupKey(ref.Kind); err != nil {
			continue
		}
		recordScope, ok := scopes[ref.RecordID]
		if !ok {
			recordScope, err = records.ResolveComparisonRecordScope(ctx, actor.Clone(), ref.RecordID)
			if err != nil {
				scopes[ref.RecordID] = recordauth.ResourceScope{}
				continue
			}
			scopes[ref.RecordID] = recordScope
		}
		if recordScope.Version == 0 || !authorizeComparisonCandidate(actor, recordScope, ref.SourceAuthorization) {
			continue
		}
		candidates = append(candidates, ComparisonCandidate{
			Subject: ref.Subject, SnapshotID: ref.SnapshotID, RecordID: ref.RecordID,
			RevisionIDs: append([]string(nil), ref.RevisionIDs...), Kind: ref.Kind, CanonicalHash: ref.CanonicalHash,
			RequestedWindow: normalizeWindow(ref.RequestedWindow), ActualWindow: normalizeWindow(ref.ActualWindow),
			Quality: ref.Quality, CapturedAt: normalizeTime(ref.CapturedAt), Recommendation: RecommendationNearestWindow,
		})
	}
	sortComparisonCandidates(candidates, subjectIndex, window)
	return ComparisonCandidateResult{Subjects: normalizedSubjects, Candidates: candidates}, nil
}

func normalizeComparisonSubjects(subjects []ComparisonSubjectRef) ([]ComparisonSubjectRef, error) {
	if len(subjects) < 2 || len(subjects) > 6 {
		return nil, fmt.Errorf("%w: subject count", ErrInvalidComparisonSelection)
	}
	normalized := make([]ComparisonSubjectRef, 0, len(subjects))
	seen := make(map[ComparisonSubjectRef]struct{}, len(subjects))
	for _, subject := range subjects {
		subject.Kind = strings.TrimSpace(subject.Kind)
		subject.ID = strings.TrimSpace(subject.ID)
		if !validComparisonSubjectRef(subject) {
			return nil, fmt.Errorf("%w: subject", ErrInvalidComparisonSelection)
		}
		if _, exists := seen[subject]; exists {
			return nil, fmt.Errorf("%w: duplicate subject", ErrInvalidComparisonSelection)
		}
		seen[subject] = struct{}{}
		normalized = append(normalized, subject)
	}
	return normalized, nil
}

func normalizeComparisonKindFilter(registry Registry, kinds []KindKey) ([]KindKey, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	normalized := make([]KindKey, 0, len(kinds))
	seen := make(map[KindKey]struct{}, len(kinds))
	for _, key := range kinds {
		if _, err := registry.LookupKey(key); err != nil {
			return nil, fmt.Errorf("%w: kind filter", ErrInvalidComparisonSelection)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	return normalized, nil
}

func validComparisonSubjectRef(subject ComparisonSubjectRef) bool {
	var prefix string
	switch recordauth.SourceKind(subject.Kind) {
	case recordauth.SourceKindVPS:
		prefix = "vps_"
	case recordauth.SourceKindMonitoringInstance:
		prefix = "mi_"
	case recordauth.SourceKindTarget:
		prefix = "tg_"
	default:
		return false
	}
	if len(subject.ID) != len(prefix)+16 || !strings.HasPrefix(subject.ID, prefix) {
		return false
	}
	for _, character := range subject.ID[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func comparisonSubjectIndex(subjects []ComparisonSubjectRef) map[ComparisonSubjectRef]int {
	index := make(map[ComparisonSubjectRef]int, len(subjects))
	for position, subject := range subjects {
		index[subject] = position
	}
	return index
}

func comparisonKindAllowed(kinds []KindKey, key KindKey) bool {
	for _, candidate := range kinds {
		if candidate == key {
			return true
		}
	}
	return false
}

func authorizeComparisonCandidate(actor ActorScope, recordScope recordauth.ResourceScope, source recordauth.SourceAuthorization) bool {
	normalizedSource, err := recordauth.NormalizeSourceAuthorization(source)
	if err != nil {
		return false
	}
	intersection := recordScope
	intersection.Sources = append(append([]recordauth.SourceAuthorization(nil), recordScope.Sources...), normalizedSource)
	if err := recordauth.Authorize(actor, recordauth.CapabilityComparisonRead, intersection); err != nil {
		return false
	}
	return recordauth.Authorize(actor, recordauth.CapabilityEvidenceRead, intersection) == nil
}

func sortComparisonCandidates(candidates []ComparisonCandidate, subjectIndex map[ComparisonSubjectRef]int, requested TimeWindow) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		leftSubject, rightSubject := subjectIndex[left.Subject], subjectIndex[right.Subject]
		if leftSubject != rightSubject {
			return leftSubject < rightSubject
		}
		if left.Kind != right.Kind {
			if left.Kind.Kind != right.Kind.Kind {
				return left.Kind.Kind < right.Kind.Kind
			}
			return left.Kind.SchemaVersion < right.Kind.SchemaVersion
		}
		leftDistance, rightDistance := comparisonWindowDistance(left.ActualWindow, requested), comparisonWindowDistance(right.ActualWindow, requested)
		if leftDistance != rightDistance {
			return leftDistance < rightDistance
		}
		leftQuality, rightQuality := comparisonQualityRank(left.Quality), comparisonQualityRank(right.Quality)
		if leftQuality != rightQuality {
			return leftQuality < rightQuality
		}
		if !left.CapturedAt.Equal(right.CapturedAt) {
			return left.CapturedAt.After(right.CapturedAt)
		}
		return left.SnapshotID < right.SnapshotID
	})
}

func comparisonWindowDistance(actual, requested TimeWindow) time.Duration {
	start := actual.Start.Sub(requested.Start)
	if start < 0 {
		start = -start
	}
	end := actual.End.Sub(requested.End)
	if end < 0 {
		end = -end
	}
	return start + end
}

func comparisonQualityRank(quality Quality) int {
	switch quality.Status {
	case QualityComplete:
		return 0
	case QualityPartial:
		return 1
	case QualityDegraded:
		return 2
	case QualityUnknown:
		return 3
	default:
		return 4
	}
}
