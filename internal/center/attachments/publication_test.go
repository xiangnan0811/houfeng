package attachments

import (
	"crypto/sha256"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBlobPublicationTargetRequiresExactDigestIdentity(t *testing.T) {
	t.Parallel()

	target := testBlobPublicationTarget()
	if err := target.Validate(); err != nil {
		t.Fatalf("BlobPublicationTarget.Validate() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*BlobPublicationTarget)
	}{
		{name: "empty digest", mutate: func(value *BlobPublicationTarget) { value.SHA256 = [sha256.Size]byte{} }},
		{name: "wrong key", mutate: func(value *BlobPublicationTarget) {
			alternateDigest := sha256.Sum256([]byte("alternate-publication-target"))
			value.Key = "sha256/" + hexDigest(alternateDigest)
		}},
		{name: "zero size", mutate: func(value *BlobPublicationTarget) { value.SizeBytes = 0 }},
		{name: "maximum size", mutate: func(value *BlobPublicationTarget) { value.SizeBytes = math.MaxInt64 }},
		{name: "unknown backend", mutate: func(value *BlobPublicationTarget) { value.BackendKind = "memory" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := target
			test.mutate(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatalf("BlobPublicationTarget.Validate(%s) error = nil", test.name)
			}
		})
	}
}

func TestBlobPublicationPrepareRequestBindsClosedOwnerKinds(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		request BlobPublicationPrepareRequest
		wantErr bool
	}{
		{name: "upload", request: BlobPublicationPrepareRequest{
			ProjectID: "default", OwnerKind: BlobPublicationOwnerUpload,
			OwnerID: "aup_publication1", OwnerGeneration: 1,
			Target: testBlobPublicationTarget(), PublishExpiresAt: expiresAt,
		}},
		{name: "processor preview", request: BlobPublicationPrepareRequest{
			ProjectID: "default", OwnerKind: BlobPublicationOwnerProcessorPreview,
			OwnerID: "apj_publication1", OwnerGeneration: 7,
			Target: testBlobPublicationTarget(), PublishExpiresAt: expiresAt,
		}},
		{name: "upload generation drift", wantErr: true, request: BlobPublicationPrepareRequest{
			ProjectID: "default", OwnerKind: BlobPublicationOwnerUpload,
			OwnerID: "aup_publication1", OwnerGeneration: 2,
			Target: testBlobPublicationTarget(), PublishExpiresAt: expiresAt,
		}},
		{name: "owner prefix mismatch", wantErr: true, request: BlobPublicationPrepareRequest{
			ProjectID: "default", OwnerKind: BlobPublicationOwnerProcessorPreview,
			OwnerID: "aup_publication1", OwnerGeneration: 1,
			Target: testBlobPublicationTarget(), PublishExpiresAt: expiresAt,
		}},
		{name: "unknown owner", wantErr: true, request: BlobPublicationPrepareRequest{
			ProjectID: "default", OwnerKind: "restore", OwnerID: "rpo_publication1", OwnerGeneration: 1,
			Target: testBlobPublicationTarget(), PublishExpiresAt: expiresAt,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if test.wantErr && err == nil {
				t.Fatal("BlobPublicationPrepareRequest.Validate() error = nil")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("BlobPublicationPrepareRequest.Validate() error = %v", err)
			}
		})
	}
}

func TestBlobPublicationIntentTransitionsOnlyToExactPublishedVersion(t *testing.T) {
	t.Parallel()

	prepared := testBlobPublicationIntent()
	if err := prepared.Validate(); err != nil {
		t.Fatalf("prepared BlobPublicationIntent.Validate() error = %v", err)
	}
	if _, ok := prepared.Object(); ok {
		t.Fatal("prepared BlobPublicationIntent.Object() unexpectedly returned an object")
	}

	published := prepared
	published.State = BlobPublicationStatePublished
	published.ObjectVersion = "publication-v1"
	if err := published.Validate(); err != nil {
		t.Fatalf("published BlobPublicationIntent.Validate() error = %v", err)
	}
	object, ok := published.Object()
	if !ok || object != (BlobObject{
		Key: published.Target.Key, ObjectVersion: published.ObjectVersion,
		SHA256: published.Target.SHA256, SizeBytes: published.Target.SizeBytes,
		BackendKind: published.Target.BackendKind,
	}) {
		t.Fatalf("published BlobPublicationIntent.Object() = %#v/%t", object, ok)
	}

	invalidStates := []BlobPublicationIntent{prepared, published}
	invalidStates[0].ObjectVersion = "publication-v1"
	invalidStates[1].ObjectVersion = ""
	for _, invalid := range invalidStates {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid state/version BlobPublicationIntent.Validate(%#v) error = nil", invalid)
		}
	}
	invalid := prepared
	invalid.State = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown-state BlobPublicationIntent.Validate() error = nil")
	}
	invalid = prepared
	invalid.PublicationID = "bpi_invalid_publication"
	if err := invalid.Validate(); err == nil {
		t.Fatal("underscore BlobPublicationIntent.Validate() error = nil")
	}
}

func TestBlobPublicationIntentStateControlsObjectAvailability(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		state         BlobPublicationState
		objectVersion string
		wantObject    bool
		wantErr       bool
	}{
		{name: "prepared", state: BlobPublicationStatePrepared},
		{name: "prepared rejects version", state: BlobPublicationStatePrepared, objectVersion: "publication-v1", wantErr: true},
		{name: "published requires version", state: BlobPublicationStatePublished, wantErr: true},
		{name: "published", state: BlobPublicationStatePublished, objectVersion: "publication-v1", wantObject: true},
		{name: "cleanup claimed before version resolve", state: BlobPublicationStateCleanupClaimed},
		{name: "cleanup claimed with version", state: BlobPublicationStateCleanupClaimed, objectVersion: "publication-v1", wantObject: true},
		{name: "retry wait before version resolve", state: BlobPublicationStateRetryWait},
		{name: "retry wait with version", state: BlobPublicationStateRetryWait, objectVersion: "publication-v1", wantObject: true},
		{name: "completed already absent before version resolve", state: BlobPublicationStateCompleted},
		{name: "completed exact delete", state: BlobPublicationStateCompleted, objectVersion: "publication-v1", wantObject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			intent := testBlobPublicationIntent()
			intent.State = test.state
			intent.ObjectVersion = test.objectVersion
			err := intent.Validate()
			if test.wantErr {
				if err == nil {
					t.Fatal("BlobPublicationIntent.Validate() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("BlobPublicationIntent.Validate() error = %v", err)
			}
			_, gotObject := intent.Object()
			if gotObject != test.wantObject {
				t.Fatalf("BlobPublicationIntent.Object() available = %t, want %t", gotObject, test.wantObject)
			}
		})
	}
}

func TestBlobPublicationVersionRequestBindsCompleteObservedObject(t *testing.T) {
	t.Parallel()

	prepared := testBlobPublicationIntent()
	request := BlobPublicationVersionRequest{
		Intent: prepared,
		Object: testBlobPublicationObjectVersion(prepared, "publication-v1"),
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("BlobPublicationVersionRequest.Validate() error = %v", err)
	}
	alternateVersion := request
	alternateVersion.Object.VersionID = "publication-v2"
	if err := alternateVersion.Validate(); err != nil {
		t.Fatalf("BlobPublicationVersionRequest.Validate(alternate bounded version) error = %v", err)
	}

	for _, test := range []struct {
		name                   string
		mutate                 func(*BlobPublicationVersionRequest)
		wantValidObjectFixture bool
	}{
		{name: "valid alternate key and digest", wantValidObjectFixture: true, mutate: func(value *BlobPublicationVersionRequest) {
			alternateDigest := sha256.Sum256([]byte("alternate-publication-object"))
			value.Object.Key = "sha256/" + hexDigest(alternateDigest)
			value.Object.SHA256 = alternateDigest
		}},
		{name: "valid alternate size", wantValidObjectFixture: true, mutate: func(value *BlobPublicationVersionRequest) {
			value.Object.SizeBytes++
		}},
		{name: "empty version", mutate: func(value *BlobPublicationVersionRequest) {
			value.Object.VersionID = ""
		}},
		{name: "oversized version", mutate: func(value *BlobPublicationVersionRequest) {
			value.Object.VersionID = strings.Repeat("v", 1025)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := request
			test.mutate(&invalid)
			if test.wantValidObjectFixture {
				if err := invalid.Object.Validate(); err != nil {
					t.Fatalf("alternate ObjectVersion fixture is invalid: %v", err)
				}
			}
			if err := invalid.Validate(); err == nil {
				t.Fatal("mismatched BlobPublicationVersionRequest.Validate() error = nil")
			}
		})
	}
}

func TestBlobPublicationCleanupClaimRequestRequiresDefaultLeaseDuration(t *testing.T) {
	t.Parallel()

	if DefaultBlobPublicationCleanupLeaseDuration != 5*time.Minute {
		t.Fatalf("DefaultBlobPublicationCleanupLeaseDuration = %v, want %v",
			DefaultBlobPublicationCleanupLeaseDuration, 5*time.Minute)
	}
	request := BlobPublicationCleanupClaimRequest{
		ProjectID:          "default",
		BackendKind:        BackendKindLocal,
		CleanupOwnerID:     "publication_reconciler_1",
		OwnerLeaseDuration: DefaultBlobPublicationCleanupLeaseDuration,
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("BlobPublicationCleanupClaimRequest.Validate() error = %v", err)
	}
	for _, lease := range []time.Duration{
		DefaultBlobPublicationCleanupLeaseDuration - time.Nanosecond,
		DefaultBlobPublicationCleanupLeaseDuration + time.Nanosecond,
		time.Duration(math.MaxInt64),
	} {
		invalid := request
		invalid.OwnerLeaseDuration = lease
		if err := invalid.Validate(); err == nil {
			t.Fatalf("BlobPublicationCleanupClaimRequest.Validate(lease %v) error = nil", lease)
		}
	}
}

func TestBlobPublicationCleanupClaimFencesOwnerGenerationAndLease(t *testing.T) {
	t.Parallel()

	intent := testBlobPublicationIntent()
	intent.State = BlobPublicationStateCleanupClaimed
	claim := BlobPublicationCleanupClaim{
		Intent: intent, CleanupOwnerID: "publication_reconciler_1",
		CleanupGeneration: 1, Attempt: 1,
		ObservedLeaseExpiresAt: time.Date(2026, time.August, 7, 12, 5, 0, 0, time.UTC),
	}
	if err := claim.Validate(); err != nil {
		t.Fatalf("BlobPublicationCleanupClaim.Validate() error = %v", err)
	}
	for _, mutate := range []func(*BlobPublicationCleanupClaim){
		func(value *BlobPublicationCleanupClaim) { value.CleanupOwnerID = "" },
		func(value *BlobPublicationCleanupClaim) { value.CleanupGeneration = 0 },
		func(value *BlobPublicationCleanupClaim) { value.Attempt = 0 },
		func(value *BlobPublicationCleanupClaim) { value.ObservedLeaseExpiresAt = time.Time{} },
		func(value *BlobPublicationCleanupClaim) { value.Intent.State = BlobPublicationStatePublished },
	} {
		invalid := claim
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatal("invalid BlobPublicationCleanupClaim.Validate() error = nil")
		}
	}
}

func TestBlobPublicationCleanupVersionRequestOnlyResolvesUnknownVersion(t *testing.T) {
	claim := testBlobPublicationCleanupClaim("")
	object := testBlobPublicationObjectVersion(claim.Intent, "publication-resolved-v1")
	request := BlobPublicationCleanupVersionRequest{Claim: claim, Object: object}
	if err := request.Validate(); err != nil {
		t.Fatalf("BlobPublicationCleanupVersionRequest.Validate() error = %v", err)
	}
	known := claim
	known.Intent.ObjectVersion = object.VersionID
	if err := (BlobPublicationCleanupVersionRequest{Claim: known, Object: object}).Validate(); err == nil {
		t.Fatal("known-version BlobPublicationCleanupVersionRequest.Validate() error = nil")
	}
}

func TestBlobPublicationCleanupCompletionRequiresExactOutcomeAndReceipt(t *testing.T) {
	t.Parallel()

	versionedClaim := testBlobPublicationCleanupClaim("publication-v1")
	retry := BlobPublicationCleanupRetryRequest{
		Claim: versionedClaim, RetryAt: time.Date(2026, time.August, 7, 12, 10, 0, 0, time.UTC),
	}
	if err := retry.Validate(); err != nil {
		t.Fatalf("BlobPublicationCleanupRetryRequest.Validate() error = %v", err)
	}
	deletedReceipt := testBlobPublicationDeletionReceipt(versionedClaim, true)
	alreadyAbsentReceipt := testBlobPublicationDeletionReceipt(versionedClaim, false)
	unresolvedClaim := testBlobPublicationCleanupClaim("")
	valid := []BlobPublicationCleanupCompletionRequest{
		{
			Claim: versionedClaim, Outcome: BlobPublicationCompletionOutcomeDeleted,
			Receipt: deletedReceipt,
		},
		{
			Claim: versionedClaim, Outcome: BlobPublicationCompletionOutcomeAlreadyAbsent,
			Receipt: alreadyAbsentReceipt,
		},
		{
			Claim: unresolvedClaim, Outcome: BlobPublicationCompletionOutcomeAlreadyAbsent,
			Receipt: DeletionReceipt{},
		},
	}
	for _, completion := range valid {
		if err := completion.Validate(); err != nil {
			t.Fatalf("BlobPublicationCleanupCompletionRequest.Validate(%s, version %q) error = %v",
				completion.Outcome, completion.Claim.Intent.ObjectVersion, err)
		}
	}

	fabricatedUnresolvedReceipt := DeletionReceipt{
		Version: testBlobPublicationObjectVersion(unresolvedClaim.Intent, "fabricated-version"),
	}
	invalid := []struct {
		name       string
		completion BlobPublicationCleanupCompletionRequest
	}{
		{name: "consumed outcome", completion: BlobPublicationCleanupCompletionRequest{
			Claim: versionedClaim, Outcome: BlobPublicationCompletionOutcomeConsumed, Receipt: deletedReceipt,
		}},
		{name: "deleted outcome with already-absent receipt", completion: BlobPublicationCleanupCompletionRequest{
			Claim: versionedClaim, Outcome: BlobPublicationCompletionOutcomeDeleted, Receipt: alreadyAbsentReceipt,
		}},
		{name: "already-absent outcome with deleted receipt", completion: BlobPublicationCleanupCompletionRequest{
			Claim: versionedClaim, Outcome: BlobPublicationCompletionOutcomeAlreadyAbsent, Receipt: deletedReceipt,
		}},
		{name: "known version with zero already-absent receipt", completion: BlobPublicationCleanupCompletionRequest{
			Claim: versionedClaim, Outcome: BlobPublicationCompletionOutcomeAlreadyAbsent,
		}},
		{name: "unresolved deleted", completion: BlobPublicationCleanupCompletionRequest{
			Claim: unresolvedClaim, Outcome: BlobPublicationCompletionOutcomeDeleted,
		}},
		{name: "unresolved absence with fabricated receipt", completion: BlobPublicationCleanupCompletionRequest{
			Claim: unresolvedClaim, Outcome: BlobPublicationCompletionOutcomeAlreadyAbsent,
			Receipt: fabricatedUnresolvedReceipt,
		}},
	}
	versionDrift := valid[0]
	versionDrift.Receipt.Version.VersionID = "other-version"
	invalid = append(invalid, struct {
		name       string
		completion BlobPublicationCleanupCompletionRequest
	}{name: "object version drift", completion: versionDrift})
	digestDrift := valid[0]
	alternateDigest := sha256.Sum256([]byte("alternate-publication-cleanup-receipt"))
	digestDrift.Receipt.Version.SHA256 = alternateDigest
	digestDrift.Receipt.Version.Key = "sha256/" + hexDigest(alternateDigest)
	invalid = append(invalid, struct {
		name       string
		completion BlobPublicationCleanupCompletionRequest
	}{name: "digest and digest-derived key drift", completion: digestDrift})
	sizeDrift := valid[0]
	sizeDrift.Receipt.Version.SizeBytes++
	invalid = append(invalid, struct {
		name       string
		completion BlobPublicationCleanupCompletionRequest
	}{name: "size drift", completion: sizeDrift})

	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := test.completion.Validate(); err == nil {
				t.Fatal("mismatched BlobPublicationCleanupCompletionRequest.Validate() error = nil")
			}
		})
	}
}

func TestBlobPublicationCleanupResultBindsExactCompletionRequest(t *testing.T) {
	t.Parallel()

	claim := testBlobPublicationCleanupClaim("publication-v1")
	completion := BlobPublicationCleanupCompletionRequest{
		Claim: claim, Outcome: BlobPublicationCompletionOutcomeDeleted,
		Receipt: testBlobPublicationDeletionReceipt(claim, true),
	}
	result := testBlobPublicationCleanupResult(completion)
	if err := result.Validate(); err != nil {
		t.Fatalf("BlobPublicationCleanupResult.Validate() error = %v", err)
	}
	if err := result.ValidateAgainst(completion); err != nil {
		t.Fatalf("BlobPublicationCleanupResult.ValidateAgainst() error = %v", err)
	}

	unresolvedCompletion := BlobPublicationCleanupCompletionRequest{
		Claim:   testBlobPublicationCleanupClaim(""),
		Outcome: BlobPublicationCompletionOutcomeAlreadyAbsent,
	}
	unresolvedResult := testBlobPublicationCleanupResult(unresolvedCompletion)
	if unresolvedResult.Object != (BlobObject{}) || unresolvedResult.Receipt != (DeletionReceipt{}) {
		t.Fatalf("unresolved already-absent result fabricated physical identity: %#v", unresolvedResult)
	}
	if err := unresolvedResult.Validate(); err != nil {
		t.Fatalf("unresolved BlobPublicationCleanupResult.Validate() error = %v", err)
	}
	if err := unresolvedResult.ValidateAgainst(unresolvedCompletion); err != nil {
		t.Fatalf("unresolved BlobPublicationCleanupResult.ValidateAgainst() error = %v", err)
	}
	driftedUnresolvedClaim := unresolvedCompletion
	driftedUnresolvedClaim.Claim.Intent.PublicationID = "bpi_otherpublication"
	if err := driftedUnresolvedClaim.Validate(); err != nil {
		t.Fatalf("drifted unresolved completion fixture is invalid: %v", err)
	}
	if err := unresolvedResult.ValidateAgainst(driftedUnresolvedClaim); err == nil {
		t.Fatal("unresolved BlobPublicationCleanupResult.ValidateAgainst(publication drift) error = nil")
	}

	for _, test := range []struct {
		name                string
		mutate              func(*BlobPublicationCleanupResult)
		wantStandaloneValid bool
	}{
		{name: "publication ID drift", wantStandaloneValid: true, mutate: func(value *BlobPublicationCleanupResult) {
			value.PublicationID = "bpi_otherpublication"
		}},
		{name: "object drift", wantStandaloneValid: true, mutate: func(value *BlobPublicationCleanupResult) {
			value.Object.BackendKind = BackendKindS3
		}},
		{name: "outcome drift", wantStandaloneValid: true, mutate: func(value *BlobPublicationCleanupResult) {
			value.Outcome = BlobPublicationCompletionOutcomeAlreadyAbsent
			value.Receipt.Deleted = false
		}},
		{name: "receipt drift", mutate: func(value *BlobPublicationCleanupResult) {
			value.Receipt.Version.VersionID = "other-version"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			drifted := result
			test.mutate(&drifted)
			if test.wantStandaloneValid {
				if err := drifted.Validate(); err != nil {
					t.Fatalf("standalone drift fixture is invalid: %v", err)
				}
			}
			if err := drifted.ValidateAgainst(completion); err == nil {
				t.Fatal("BlobPublicationCleanupResult.ValidateAgainst(drift) error = nil")
			}
		})
	}
}

func testBlobPublicationTarget() BlobPublicationTarget {
	digest := sha256.Sum256([]byte("publication-target"))
	return BlobPublicationTarget{
		Key: "sha256/" + hexDigest(digest), SHA256: digest,
		SizeBytes: 19, BackendKind: BackendKindLocal,
	}
}

func testBlobPublicationIntent() BlobPublicationIntent {
	return BlobPublicationIntent{
		PublicationID: "bpi_publication1", ProjectID: "default",
		OwnerKind: BlobPublicationOwnerUpload, OwnerID: "aup_publication1", OwnerGeneration: 1,
		Target:           testBlobPublicationTarget(),
		State:            BlobPublicationStatePrepared,
		PublishExpiresAt: time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC),
	}
}

func testBlobPublicationObjectVersion(intent BlobPublicationIntent, versionID string) ObjectVersion {
	return ObjectVersion{
		Key: intent.Target.Key, VersionID: versionID,
		SHA256: intent.Target.SHA256, SizeBytes: intent.Target.SizeBytes,
	}
}

func testBlobPublicationCleanupClaim(objectVersion string) BlobPublicationCleanupClaim {
	intent := testBlobPublicationIntent()
	intent.State = BlobPublicationStateCleanupClaimed
	intent.ObjectVersion = objectVersion
	return BlobPublicationCleanupClaim{
		Intent: intent, CleanupOwnerID: "publication_reconciler_1",
		CleanupGeneration: 1, Attempt: 1,
		ObservedLeaseExpiresAt: time.Date(2026, time.August, 7, 12, 5, 0, 0, time.UTC),
	}
}

func testBlobPublicationDeletionReceipt(claim BlobPublicationCleanupClaim, deleted bool) DeletionReceipt {
	return DeletionReceipt{
		Version: testBlobPublicationObjectVersion(claim.Intent, claim.Intent.ObjectVersion),
		Deleted: deleted,
	}
}

func testBlobPublicationCleanupResult(
	completion BlobPublicationCleanupCompletionRequest,
) BlobPublicationCleanupResult {
	result := BlobPublicationCleanupResult{
		PublicationID: completion.Claim.Intent.PublicationID,
		Outcome:       completion.Outcome,
		Receipt:       completion.Receipt,
	}
	if object, ok := completion.Claim.Intent.Object(); ok {
		result.Object = object
	}
	return result
}
