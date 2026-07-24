package recordplatform

import (
	"errors"
	"testing"
	"time"
)

func TestResolveIdempotencyV1ClassifiesReplayLiveOwnerTakeoverAndImmutableMismatch(t *testing.T) {
	now := time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC)
	request := testRequestFingerprintV1(t, 0x11)
	differentRequest := testRequestFingerprintV1(t, 0x12)
	persistedRequest := testPersistedRequestFingerprintV1(t, 0x11)
	persistedResult := testPersistedRequestFingerprintV1(t, 0x13)
	key := IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: "client-key.1"}
	liveOwner := OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: now.Add(time.Minute)}

	cases := []struct {
		name       string
		record     IdempotencyRecordV1
		request    RequestFingerprintV1
		wantAction IdempotencyAction
		wantResult PersistedRequestFingerprintV1
		wantErr    error
	}{
		{
			name: "same completed request replays stored result only",
			record: IdempotencyRecordV1{
				Key:                key,
				RequestFingerprint: persistedRequest,
				ResultFingerprint:  &persistedResult,
				Status:             IdempotencyStatusCompleted,
				ExpiresAt:          now.Add(time.Hour),
			},
			request:    request,
			wantAction: IdempotencyActionReplay,
			wantResult: persistedResult,
		},
		{
			name: "same live request reports stable in progress",
			record: IdempotencyRecordV1{
				Key:                key,
				RequestFingerprint: persistedRequest,
				Status:             IdempotencyStatusInProgress,
				Owner:              &liveOwner,
				ExpiresAt:          now.Add(2 * time.Minute),
			},
			request: request,
			wantErr: ErrIdempotencyInProgress,
		},
		{
			name: "same expired request requires takeover",
			record: IdempotencyRecordV1{
				Key:                key,
				RequestFingerprint: persistedRequest,
				Status:             IdempotencyStatusInProgress,
				Owner:              &OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: now.Add(-time.Nanosecond)},
				ExpiresAt:          now.Add(time.Minute),
			},
			request:    request,
			wantAction: IdempotencyActionTakeover,
		},
		{
			name: "different fingerprint never mutates original row",
			record: IdempotencyRecordV1{
				Key:                key,
				RequestFingerprint: persistedRequest,
				Status:             IdempotencyStatusInProgress,
				Owner:              &liveOwner,
				ExpiresAt:          now.Add(2 * time.Minute),
			},
			request: differentRequest,
			wantErr: ErrIdempotencyKeyReused,
		},
		{
			name: "inherited conflict state fails closed read only",
			record: IdempotencyRecordV1{
				Key:                key,
				RequestFingerprint: persistedRequest,
				Status:             IdempotencyStatusConflict,
				ExpiresAt:          now.Add(time.Hour),
			},
			request: request,
			wantErr: ErrIdempotencyConflictState,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			resolution, err := ResolveIdempotencyV1(test.record, test.request, now)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("ResolveIdempotencyV1() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveIdempotencyV1() error = %v", err)
			}
			if resolution.Action != test.wantAction {
				t.Fatalf("ResolveIdempotencyV1() action = %v, want %v", resolution.Action, test.wantAction)
			}
			if test.wantAction == IdempotencyActionReplay && !resolution.ResultFingerprint.Equal(test.wantResult) {
				t.Fatal("ResolveIdempotencyV1() did not return the expected replay fingerprint")
			}
		})
	}
}

func TestIdempotencyRecordV1RejectsMissingResultBadExpiryAndStaleOwner(t *testing.T) {
	now := time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC)
	key := IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: "client-key.1"}
	request := testPersistedRequestFingerprintV1(t, 0x11)
	owner := OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: now.Add(time.Minute)}
	zeroResult, err := ParseTrustedPersistedRequestFingerprintV1(make([]byte, 32))
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() zero digest error = %v", err)
	}

	for _, record := range []IdempotencyRecordV1{
		{Key: key, RequestFingerprint: request, Status: IdempotencyStatusCompleted, ExpiresAt: now.Add(time.Hour)},
		{Key: key, RequestFingerprint: request, Status: IdempotencyStatusInProgress, Owner: &owner, ExpiresAt: owner.ExpiresAt},
		{Key: key, RequestFingerprint: request, Status: IdempotencyStatusCompleted, ResultFingerprint: &zeroResult, ExpiresAt: now.Add(time.Hour)},
	} {
		if err := record.Validate(); !errors.Is(err, ErrInvalidIdempotencyRecord) {
			t.Fatalf("IdempotencyRecordV1.Validate(%#v) error = %v, want ErrInvalidIdempotencyRecord", record, err)
		}
	}

	if err := RequireLiveOwnerFenceV1(owner, OwnerLease{OwnerID: "worker_01", Generation: 2, ExpiresAt: owner.ExpiresAt}, now); !errors.Is(err, ErrLostOwnerLease) {
		t.Fatalf("RequireLiveOwnerFenceV1() generation error = %v, want ErrLostOwnerLease", err)
	}
	if err := RequireLiveOwnerFenceV1(owner, owner, owner.ExpiresAt); !errors.Is(err, ErrLostOwnerLease) {
		t.Fatalf("RequireLiveOwnerFenceV1() expired error = %v, want ErrLostOwnerLease", err)
	}
}

func TestRequireLiveOwnerFenceV1RejectsPreRenewExpiryToken(t *testing.T) {
	now := time.Date(2026, time.July, 24, 11, 0, 0, 0, time.UTC)
	preRenewOwner := OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: now.Add(time.Minute)}
	renewedOwner := OwnerLease{OwnerID: "worker_01", Generation: 1, ExpiresAt: now.Add(2 * time.Minute)}

	if err := RequireLiveOwnerFenceV1(renewedOwner, preRenewOwner, now); !errors.Is(err, ErrLostOwnerLease) {
		t.Fatalf("RequireLiveOwnerFenceV1() pre-renew expiry error = %v, want ErrLostOwnerLease", err)
	}
}

func TestIdempotencyClaimV1RejectsExpiryThatCollapsesToOwnerExpiryAfterMicrosecondNormalization(t *testing.T) {
	input := IdempotencyClaimInputV1{
		Key:                IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: "client-key.1"},
		RequestFingerprint: testRequestFingerprintV1(t, 0x11),
		OwnerID:            "worker_01",
		OwnerLeaseDuration: time.Microsecond,
		RecordTTL:          time.Microsecond + 500*time.Nanosecond,
	}
	if err := input.Validate(); !errors.Is(err, ErrInvalidIdempotencyClaim) {
		t.Fatalf("IdempotencyClaimInputV1.Validate() error = %v, want ErrInvalidIdempotencyClaim after persisted duration normalization", err)
	}
}

func TestIdempotencyClaimV1RejectsUnsealedFingerprintLiteral(t *testing.T) {
	input := IdempotencyClaimInputV1{
		Key:                IdempotencyKey{ProjectID: ProjectIDDefault, OperationKind: OperationKindRecordCreate, Key: "client-key.1"},
		RequestFingerprint: RequestFingerprintV1{},
		OwnerID:            "worker_01",
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          2 * time.Minute,
	}
	if err := input.Validate(); !errors.Is(err, ErrInvalidIdempotencyClaim) {
		t.Fatalf("IdempotencyClaimInputV1.Validate() error = %v, want ErrInvalidIdempotencyClaim", err)
	}
}

func testRequestFingerprintV1(t *testing.T, payloadDigest byte) RequestFingerprintV1 {
	t.Helper()
	input := testRequestFingerprintInputV1()
	input.PayloadDigest = testRecordPlatformDigest(payloadDigest)
	fingerprint, err := FingerprintRequestV1(input)
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return fingerprint
}

func testPersistedRequestFingerprintV1(t *testing.T, payloadDigest byte) PersistedRequestFingerprintV1 {
	t.Helper()
	fingerprint := testRequestFingerprintV1(t, payloadDigest)
	persisted, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	readback, err := ParseTrustedPersistedRequestFingerprintV1(persisted[:])
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
	}
	return readback
}
