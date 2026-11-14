package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// The replay verifier authenticates tokens without loading or recapturing the
// original sources. The handler's read boundary still has to authorize the
// stored first revision before returning the result.
type handlerReplaySelection struct{}

func (handlerReplaySelection) LoadComparisonSnapshots(context.Context, evidence.ActorScope, []string) (map[string]evidence.ComparisonLoadedSnapshot, error) {
	return nil, evidence.ErrComparisonSelectionNotFound
}

func (handlerReplaySelection) LoadComparisonRevisions(context.Context, evidence.ActorScope, []evidence.ComparisonRevisionKey) (map[evidence.ComparisonRevisionKey]evidence.ComparisonLoadedRevision, error) {
	return nil, evidence.ErrComparisonSelectionNotFound
}

func TestLegacyComparisonPublicationHTTPReplaysOnlyProvenAuthorizedResult(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	const legacyID = "rec_comparisonsave"
	keyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(keyDir, "cmp_test"), []byte("comparison-replay-fixture-key-material-32-bytes"), 0400); err != nil {
		t.Fatal(err)
	}
	signer, err := evidence.OpenComparisonIntentKeyring(keyDir, "cmp_test", nil)
	if err != nil {
		t.Fatal(err)
	}
	window := evidence.TimeWindow{Start: time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC), End: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	inputs := []evidence.ComparisonItemInput{
		{SnapshotID: "evs_sourcea", Hash: sha256.Sum256([]byte("source-a")), Kind: evidence.CommandAuditV1Key(), RevisionContext: evidence.RevisionContextNotApplicable, SourceRecordID: "rec_sourcea"},
		{SnapshotID: "evs_sourceb", Hash: sha256.Sum256([]byte("source-b")), Kind: evidence.CommandAuditV1Key(), RevisionContext: evidence.RevisionContextNotApplicable, SourceRecordID: "rec_sourceb"},
	}
	tokenFor := func(items []evidence.ComparisonItemInput, issued time.Time) (string, string) {
		t.Helper()
		evaluated, err := evidence.EvaluateComparison(evidence.Registry{}, evidence.ComparisonEvaluateInput{Items: items, BaselineIndex: 0, Alignment: evidence.CoverageActual, RequestedWindow: window})
		if err != nil {
			t.Fatal(err)
		}
		claims := evidence.BuildComparisonIntentClaims(evidence.ComparisonIntentClaimsInput{
			Actor: actor, Items: evaluated.Items, BaselineIndex: 0, Alignment: evidence.CoverageActual,
			RequestedWindow: window, Digest: evaluated.Digest, Review: evaluated.Review, Now: issued,
		})
		intent, err := signer.Sign(claims)
		if err != nil {
			t.Fatal(err)
		}
		return intent.Token, fmt.Sprintf("%x", evaluated.Digest)
	}
	now := time.Now().UTC()
	currentToken, digest := tokenFor(inputs, now)
	expiredToken, _ := tokenFor(inputs, now.Add(-time.Hour))
	reorderedToken, _ := tokenFor([]evidence.ComparisonItemInput{inputs[1], inputs[0]}, now)
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject, ProjectID: actor.ProjectID,
		PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Subjects: mustRecordsHandlerRecord(t, actor).Current.Input.Subjects(),
		Title:    "Saved comparison", BodyMarkdown: "Comparison", MarkdownDialectVersion: records.MarkdownDialectVersionV1,
		RecordType: records.RecordTypeNote, ImpactLevel: "low", VisibilityScope: visibility, AuthorID: actor.UserID,
		EvidenceSnapshotIDs: []string{
			evidence.DeriveComparisonCopySnapshotID(legacyID, "evs_sourcea", digest),
			evidence.DeriveComparisonCopySnapshotID(legacyID, "evs_sourceb", digest),
			evidence.DeriveComparisonResultSnapshotID(legacyID, digest),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	revision := records.RecordRevision{RecordID: legacyID, RevisionID: "rrv_legacy", RevisionNo: 1, Input: input, CreatedAt: now.Add(-time.Hour)}
	fingerprint, err := records.CompletedCreateFingerprint(actor, draft.DraftID, draft.ETag, revision)
	if err != nil {
		t.Fatal(err)
	}
	candidate := records.CompletedCreateCandidate{RecordID: legacyID, RevisionID: revision.RevisionID, RequestFingerprint: fingerprint}
	options, memory := newHandlerBirthFixture(t)
	options.ComparisonReplay = (&evidence.Service{}).WithFixedComparison(handlerReplaySelection{}, nil, signer)
	options.CreateReplay = func(_ context.Context, _ recordauth.ActorScope, key string) (records.CompletedCreateCandidate, bool, error) {
		return candidate, key == "legacy-completed", nil
	}
	var readErr error
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		getRevision: func(_ context.Context, request records.RecordRevisionGetRequest) (records.RecordRevision, error) {
			if readErr != nil {
				return records.RecordRevision{}, readErr
			}
			if request.Actor.CanonicalHash() != actor.CanonicalHash() {
				return records.RecordRevision{}, recordauth.ErrDenied
			}
			if request.RecordID != legacyID || request.RevisionID != revision.RevisionID {
				t.Fatalf("unexpected replay read: %#v", request)
			}
			return revision, nil
		},
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			t.Fatal("completed legacy replay must not read a consumed draft")
			return records.Draft{}, records.ErrDraftNotFound
		},
		createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			t.Fatal("legacy replay must not create a root")
			return records.RevisionCommitResult{}, nil
		},
	}, options)
	post := func(actor recordauth.ActorScope, key, body string) *httptest.ResponseRecorder {
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
		request.Header.Set("Idempotency-Key", key)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	minimal := func(token string) string {
		return `{"record_id":"` + legacyID + `","comparison_intent":` + strconvQuote(token) + `}`
	}
	for _, token := range []string{currentToken, expiredToken} {
		recorder := post(actor, "legacy-completed", minimal(token))
		if recorder.Code != http.StatusOK {
			t.Fatalf("minimal replay status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var result struct {
			RecordID   string `json:"record_id"`
			RevisionID string `json:"revision_id"`
			Replayed   bool   `json:"replayed"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.RecordID != legacyID || result.RevisionID != revision.RevisionID || !result.Replayed {
			t.Fatalf("legacy replay = %#v error %v", result, err)
		}
	}
	full := `{"record_id":"` + legacyID + `","draft_id":` + strconvQuote(draft.DraftID) + `,"draft_etag":` + strconvQuote(draft.ETag.String()) + `,"comparison_intent":` + strconvQuote(currentToken) + `}`
	if recorder := post(actor, "legacy-completed", full); recorder.Code != http.StatusOK {
		t.Fatalf("full replay status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	changedETag, err := records.NewDraftETag(draft.DraftID, actor.UserID, draft.Version+1, draft.Payload)
	if err != nil {
		t.Fatal(err)
	}
	changed := `{"record_id":"` + legacyID + `","draft_id":` + strconvQuote(draft.DraftID) + `,"draft_etag":` + strconvQuote(changedETag.String()) + `,"comparison_intent":` + strconvQuote(currentToken) + `}`
	assertRecordsHandlerError(t, post(actor, "legacy-completed", changed), http.StatusConflict, "idempotency_conflict")
	assertRecordsHandlerError(t, post(actor, "legacy-completed", minimal(reorderedToken)), http.StatusConflict, "idempotency_conflict")
	assertRecordsHandlerError(t, post(actor, "legacy-completed", minimal(currentToken+"tampered")), http.StatusUnprocessableEntity, "comparison_intent_invalid")
	assertRecordsHandlerError(t, post(actor, "legacy-completed", `{"record_id":"`+legacyID+`"}`), http.StatusBadRequest, "invalid_request")
	assertRecordsHandlerError(t, post(actor, "missing-proof", minimal(currentToken)), http.StatusBadRequest, "invalid_request")
	other := actor.Clone()
	other.GroupIDs = []string{"rag_other"}
	assertRecordsHandlerError(t, post(other, "legacy-completed", minimal(currentToken)), http.StatusNotFound, "resource_not_found")
	readErr = records.ErrRecordDeletionReserved
	assertRecordsHandlerError(t, post(actor, "legacy-completed", minimal(currentToken)), http.StatusNotFound, "resource_not_found")
	if memory.begins != 0 {
		t.Fatal("legacy replay attempted a new allocation")
	}
}
