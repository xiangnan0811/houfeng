package handlers

import (
	"encoding/hex"
	"net/http"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

type comparisonCandidateInput struct {
	Subjects        []comparisonSubjectInput `json:"subjects"`
	RequestedWindow evidenceTimeWindow       `json:"requested_window"`
	Kinds           []comparisonKindInput    `json:"kinds"`
}

type comparisonSubjectInput struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type comparisonKindInput struct {
	Kind          evidence.KindName `json:"kind"`
	SchemaVersion uint16            `json:"schema_version"`
}

type comparisonCandidateResponse struct {
	Subjects   []comparisonSubjectResponse  `json:"subjects"`
	Candidates []comparisonCandidateItemDTO `json:"candidates"`
}

type comparisonSubjectResponse struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type comparisonCandidateItemDTO struct {
	Subject         comparisonSubjectResponse `json:"subject"`
	SnapshotID      string                    `json:"snapshot_id"`
	RecordID        string                    `json:"record_id"`
	RevisionIDs     []string                  `json:"revision_ids"`
	Kind            evidence.KindName         `json:"kind"`
	SchemaVersion   evidence.SchemaVersion    `json:"schema_version"`
	CanonicalHash   string                    `json:"canonical_hash"`
	RequestedWindow evidenceTimeWindow        `json:"requested_window"`
	ActualWindow    evidenceTimeWindow        `json:"actual_window"`
	QualityStatus   evidence.QualityStatus    `json:"quality_status"`
	CapturedAt      time.Time                 `json:"captured_at"`
	Recommendation  string                    `json:"recommendation"`
}

func handleEvidenceComparisonCandidates(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application evidenceHandlerApplication,
) {
	if request.Method != http.MethodPost {
		writeEvidenceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var input comparisonCandidateInput
	if !decodeEvidenceRequestJSON(w, request, &input) {
		return
	}
	if input.Subjects == nil {
		writeEvidenceError(w, http.StatusUnprocessableEntity, "comparison_selection_invalid", "comparison selection is invalid")
		return
	}
	subjects := make([]evidence.ComparisonSubjectRef, 0, len(input.Subjects))
	for _, subject := range input.Subjects {
		subjects = append(subjects, evidence.ComparisonSubjectRef{Kind: subject.Kind, ID: subject.ID})
	}
	kinds := make([]evidence.KindKey, 0, len(input.Kinds))
	for _, key := range input.Kinds {
		kinds = append(kinds, evidence.KindKey{Kind: key.Kind, SchemaVersion: evidence.SchemaVersion(key.SchemaVersion)})
	}
	result, err := application.ResolveComparisonCandidates(request.Context(), evidence.ComparisonCandidateRequest{
		Actor: actor, Subjects: subjects,
		RequestedWindow: evidence.TimeWindow{Start: input.RequestedWindow.Start, End: input.RequestedWindow.End},
		Kinds:           kinds,
	})
	if err != nil {
		writeEvidenceApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newComparisonCandidateResponse(result))
}

type comparisonEvaluateInput struct {
	Items            []comparisonFixedItemInput `json:"items"`
	BaselineIndex    int                        `json:"baseline_index"`
	Alignment        evidence.CoverageAlignment `json:"alignment"`
	RequestedWindow  evidenceTimeWindow         `json:"requested_window"`
	ToleranceSeconds int64                      `json:"tolerance_seconds"`
	BucketSeconds    *int64                     `json:"bucket_seconds"`
	Detail           *comparisonDetailInput     `json:"detail"`
}

type comparisonFixedItemInput struct {
	SnapshotID  *string  `json:"snapshot_id"`
	RecordID    string   `json:"record_id"`
	RevisionID  string   `json:"revision_id"`
	SnapshotIDs []string `json:"snapshot_ids"`
}

type comparisonDetailInput struct {
	Kind          evidence.KindName `json:"kind"`
	SchemaVersion uint16            `json:"schema_version"`
	Metric        string            `json:"metric"`
}

type comparisonEvaluateResponse struct {
	Digest           string                       `json:"digest"`
	Items            []comparisonResolvedItemDTO  `json:"items"`
	Review           []comparisonFindingDTO       `json:"review"`
	AvailableKinds   []comparisonKindInput        `json:"available_kinds"`
	Pairwise         []comparisonPairwiseDTO      `json:"pairwise"`
	Series           []comparisonSeriesDTO        `json:"series"`
	SaveEligibility  comparisonSaveEligibilityDTO `json:"save_eligibility"`
	ComparisonIntent *comparisonIntentDTO         `json:"comparison_intent,omitempty"`
}

type comparisonResolvedItemDTO struct {
	SnapshotID      string                     `json:"snapshot_id"`
	Hash            string                     `json:"canonical_hash"`
	Kind            evidence.KindName          `json:"kind"`
	SchemaVersion   evidence.SchemaVersion     `json:"schema_version"`
	RevisionContext evidence.RevisionContext   `json:"revision_context"`
	RecordID        string                     `json:"record_id,omitempty"`
	RevisionID      string                     `json:"revision_id,omitempty"`
	SubjectKind     string                     `json:"subject_kind,omitempty"`
	SubjectID       string                     `json:"subject_id,omitempty"`
	Revision        *comparisonRevisionMetaDTO `json:"revision"`
}

type comparisonRevisionMetaDTO struct {
	RecordType     string     `json:"record_type"`
	BusinessStatus string     `json:"business_status"`
	StatusGroup    string     `json:"status_group"`
	ImpactLevel    string     `json:"impact_level"`
	OccurredAt     *time.Time `json:"occurred_at"`
}

type comparisonFindingDTO struct {
	ItemIndex     int                       `json:"item_index"`
	Kind          evidence.KindName         `json:"kind,omitempty"`
	SchemaVersion evidence.SchemaVersion    `json:"schema_version,omitempty"`
	Reason        evidence.ComparisonReason `json:"reason"`
}

type comparisonPairwiseDTO struct {
	ItemIndex     int                    `json:"item_index"`
	Kind          evidence.KindName      `json:"kind"`
	SchemaVersion evidence.SchemaVersion `json:"schema_version"`
	Compatible    bool                   `json:"compatible"`
	Reason        string                 `json:"reason"`
	Values        map[string]any         `json:"values"`
}

type comparisonSeriesDTO struct {
	ItemIndex int                    `json:"item_index"`
	MetricID  string                 `json:"metric_id"`
	Segments  [][]comparisonPointDTO `json:"segments"`
	Unit      string                 `json:"unit"`
}

type comparisonPointDTO struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Value float64   `json:"value"`
}

type comparisonSaveEligibilityDTO struct {
	Eligible bool                        `json:"eligible"`
	Blockers []evidence.ComparisonReason `json:"blockers"`
}

type comparisonIntentDTO struct {
	Token     string    `json:"token"`
	KeyID     string    `json:"key_id"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func handleEvidenceFixedComparison(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application evidenceHandlerApplication,
) {
	if request.Method != http.MethodPost {
		writeEvidenceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var input comparisonEvaluateInput
	if !decodeEvidenceRequestJSON(w, request, &input) {
		return
	}
	if input.Items == nil {
		writeEvidenceError(w, http.StatusUnprocessableEntity, "comparison_selection_invalid", "comparison selection is invalid")
		return
	}
	items := make([]evidence.ComparisonFixedItem, 0, len(input.Items))
	for _, item := range input.Items {
		fixed := evidence.ComparisonFixedItem{SnapshotID: item.SnapshotID}
		if item.RecordID != "" || item.RevisionID != "" || len(item.SnapshotIDs) > 0 {
			fixed.Revision = &evidence.ComparisonFixedRevision{
				RecordID: item.RecordID, RevisionID: item.RevisionID,
				ChosenSnapshotIDs: append([]string(nil), item.SnapshotIDs...),
			}
		}
		items = append(items, fixed)
	}
	var detail *evidence.ComparisonDetail
	if input.Detail != nil {
		detail = &evidence.ComparisonDetail{
			Kind:   evidence.KindKey{Kind: input.Detail.Kind, SchemaVersion: evidence.SchemaVersion(input.Detail.SchemaVersion)},
			Metric: input.Detail.Metric,
		}
	}
	var bucket *time.Duration
	if input.BucketSeconds != nil {
		value := time.Duration(*input.BucketSeconds) * time.Second
		bucket = &value
	}
	result, err := application.EvaluateFixedComparison(request.Context(), evidence.ComparisonEvaluateRequest{
		Actor: actor, Items: items, BaselineIndex: input.BaselineIndex, Alignment: input.Alignment,
		RequestedWindow: evidence.TimeWindow{Start: input.RequestedWindow.Start, End: input.RequestedWindow.End},
		Tolerance:       time.Duration(input.ToleranceSeconds) * time.Second,
		BucketWidth:     bucket, Detail: detail,
	})
	if err != nil {
		writeEvidenceApplicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newComparisonEvaluateResponse(result))
}

func newComparisonEvaluateResponse(result evidence.ComparisonEvaluateOutput) comparisonEvaluateResponse {
	response := comparisonEvaluateResponse{
		Digest:         hex.EncodeToString(result.Digest[:]),
		Items:          make([]comparisonResolvedItemDTO, 0, len(result.Items)),
		Review:         make([]comparisonFindingDTO, 0, len(result.Review)),
		AvailableKinds: make([]comparisonKindInput, 0, len(result.AvailableKinds)),
		Pairwise:       make([]comparisonPairwiseDTO, 0, len(result.Pairwise)),
		Series:         make([]comparisonSeriesDTO, 0, len(result.Series)),
		SaveEligibility: comparisonSaveEligibilityDTO{
			Eligible: result.SaveEligibility.Eligible,
			Blockers: append([]evidence.ComparisonReason(nil), result.SaveEligibility.Blockers...),
		},
	}
	if response.SaveEligibility.Blockers == nil {
		response.SaveEligibility.Blockers = make([]evidence.ComparisonReason, 0)
	}
	for _, item := range result.Items {
		dto := comparisonResolvedItemDTO{
			SnapshotID: item.SnapshotID, Hash: hex.EncodeToString(item.Hash[:]),
			Kind: item.Kind.Kind, SchemaVersion: item.Kind.SchemaVersion,
			RevisionContext: item.RevisionContext,
			RecordID:        item.RecordID, RevisionID: item.RevisionID,
			SubjectKind: item.SubjectKind, SubjectID: item.SubjectID,
		}
		if item.Revision != nil && item.RevisionContext == evidence.RevisionContextBound {
			revision := &comparisonRevisionMetaDTO{
				RecordType: item.Revision.RecordType, BusinessStatus: item.Revision.BusinessStatus,
				StatusGroup: item.Revision.StatusGroup, ImpactLevel: item.Revision.ImpactLevel,
			}
			if item.Revision.HasOccurredAt {
				occurred := item.Revision.OccurredAt.UTC()
				revision.OccurredAt = &occurred
			}
			dto.Revision = revision
		}
		response.Items = append(response.Items, dto)
	}
	for _, finding := range result.Review {
		response.Review = append(response.Review, comparisonFindingDTO{
			ItemIndex: finding.ItemIndex, Kind: finding.Kind.Kind, SchemaVersion: finding.Kind.SchemaVersion,
			Reason: finding.Reason,
		})
	}
	for _, key := range result.AvailableKinds {
		response.AvailableKinds = append(response.AvailableKinds, comparisonKindInput{Kind: key.Kind, SchemaVersion: uint16(key.SchemaVersion)})
	}
	for _, pairwise := range result.Pairwise {
		values := pairwise.Values
		if values == nil {
			values = map[string]any{}
		}
		response.Pairwise = append(response.Pairwise, comparisonPairwiseDTO{
			ItemIndex: pairwise.ItemIndex,
			Kind:      pairwise.Key.Kind, SchemaVersion: pairwise.Key.SchemaVersion,
			Compatible: pairwise.Compatible, Reason: pairwise.Reason, Values: values,
		})
	}
	for _, series := range result.Series {
		segments := make([][]comparisonPointDTO, 0, len(series.Segments))
		for _, segment := range series.Segments {
			points := make([]comparisonPointDTO, 0, len(segment))
			for _, point := range segment {
				points = append(points, comparisonPointDTO{Start: point.Start.UTC(), End: point.End.UTC(), Value: point.Value})
			}
			segments = append(segments, points)
		}
		response.Series = append(response.Series, comparisonSeriesDTO{
			ItemIndex: series.ItemIndex, MetricID: series.MetricID, Segments: segments, Unit: series.Unit,
		})
	}
	if result.Intent != nil {
		response.ComparisonIntent = &comparisonIntentDTO{
			Token: result.Intent.Token, KeyID: result.Intent.KeyID,
			IssuedAt: result.Intent.IssuedAt.UTC(), ExpiresAt: result.Intent.ExpiresAt.UTC(),
		}
	}
	return response
}

func newComparisonCandidateResponse(result evidence.ComparisonCandidateResult) comparisonCandidateResponse {
	response := comparisonCandidateResponse{
		Subjects:   make([]comparisonSubjectResponse, 0, len(result.Subjects)),
		Candidates: make([]comparisonCandidateItemDTO, 0, len(result.Candidates)),
	}
	for _, subject := range result.Subjects {
		response.Subjects = append(response.Subjects, comparisonSubjectResponse{Kind: subject.Kind, ID: subject.ID})
	}
	for _, candidate := range result.Candidates {
		revisionIDs := candidate.RevisionIDs
		if revisionIDs == nil {
			revisionIDs = make([]string, 0)
		}
		response.Candidates = append(response.Candidates, comparisonCandidateItemDTO{
			Subject:    comparisonSubjectResponse{Kind: candidate.Subject.Kind, ID: candidate.Subject.ID},
			SnapshotID: candidate.SnapshotID, RecordID: candidate.RecordID, RevisionIDs: revisionIDs,
			Kind: candidate.Kind.Kind, SchemaVersion: candidate.Kind.SchemaVersion,
			CanonicalHash:   hex.EncodeToString(candidate.CanonicalHash[:]),
			RequestedWindow: evidenceTimeWindow{Start: candidate.RequestedWindow.Start.UTC(), End: candidate.RequestedWindow.End.UTC()},
			ActualWindow:    evidenceTimeWindow{Start: candidate.ActualWindow.Start.UTC(), End: candidate.ActualWindow.End.UTC()},
			QualityStatus:   candidate.Quality.Status, CapturedAt: candidate.CapturedAt.UTC(), Recommendation: candidate.Recommendation,
		})
	}
	return response
}
