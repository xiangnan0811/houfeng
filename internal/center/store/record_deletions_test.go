package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresRecordDeletionCoreHealthAndPreviewUseAdmittedContentFreeSnapshots(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordPlatformTx{}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "to_regprocedure"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*bool) = true
				return nil
			}}
		case strings.Contains(sql, "as record_core_surface_count"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*int64) = int64(len(recorddeletion.RecordCoreSurfaceNames()))
				return nil
			}}
		case strings.Contains(sql, "as core_dependency_material"):
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*[]byte) = []byte(`{"revisions":["rrv_corepreview01"]}`)
				*dest[1].(*[]byte) = []byte(`{"revision_count":1,"draft_count":0}`)
				return nil
			}}
		default:
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
	}
	repository := testStoreDeletionRepository(tx)

	health, err := repository.RecordCoreHealth(context.Background())
	if err != nil {
		t.Fatalf("RecordCoreHealth() error = %v", err)
	}
	if !health.Healthy() || health.Revision() != 1 || health.ProofDigest() == ([32]byte{}) {
		t.Fatalf("RecordCoreHealth() = %#v", health)
	}
	tx.committed = false
	target := recorddeletion.PreviewTarget{
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_corepreview01",
		},
		CurrentRevisionID:     "rrv_corepreview01",
		LockVersion:           2,
		AuthorizationEpoch:    3,
		ContentDeliveryEpoch:  4,
		DependencyGraphDigest: testStoreRecordPlatformDigest(0x52),
	}
	preview, err := repository.PreviewRecordCore(context.Background(), target)
	if err != nil {
		t.Fatalf("PreviewRecordCore() error = %v", err)
	}
	if err := preview.Validate(); err != nil {
		t.Fatalf("PreviewRecordCore() = %#v: %v", preview, err)
	}
	if !tx.committed || len(tx.querySQL) != 4 {
		t.Fatalf("core health/preview queries=%d committed=%t, want 4/true", len(tx.querySQL), tx.committed)
	}
	previewSQL := tx.querySQL[3]
	for _, want := range []string{
		"reservation.state in ('fenced', 'committed')",
		"root.current_revision_id = $2",
		"root.lock_version = $3",
		"root.authorization_epoch = $4",
		"epoch.delivery_epoch = $5",
		"payload_hash",
		"checkpoint_payload_hash",
		"canonical_hash",
	} {
		if !strings.Contains(previewSQL, want) {
			t.Errorf("core preview SQL missing %q", want)
		}
	}
	for _, forbidden := range []string{"body_markdown", "current_title", "identity_snapshot"} {
		if strings.Contains(previewSQL, forbidden) {
			t.Errorf("core preview SQL reads content-bearing column %q", forbidden)
		}
	}
}

func TestPostgresRecordDeletionLegacyIssuanceIsClosed(t *testing.T) {
	t.Parallel()
	tx := &fakeRecordPlatformTx{}
	repository := testStoreDeletionRepository(tx)
	if _, err := repository.CreatePreview(context.Background(), testStoreDeletionCreatePreviewCommand(t)); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("CreatePreview() error = %v, want legacy issuance unavailable", err)
	}
	if _, err := repository.ReservePreview(context.Background(), testStoreDeletionReservePreviewCommand(t)); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("ReservePreview() error = %v, want legacy issuance unavailable", err)
	}
	if len(tx.querySQL) != 0 || len(tx.execSQL) != 0 || tx.committed {
		t.Fatal("legacy issuance touched the database")
	}
}

func TestPostgresRecordDeletionResolvePreviewUsesLocatorAndLoadsDurableReplay(t *testing.T) {
	t.Parallel()

	command := testStoreDeletionCreatePreviewCommand(t)
	reservationID := "drs_0123456789abcdef"
	expiresAt := time.Date(2026, time.August, 3, 18, 10, 0, 0, time.UTC)
	fingerprintBytes, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_resolvepreview01", ReservationID: reservationID,
		Object:          command.Object,
		ReasonCode:      recorddeletion.DeletionReasonUserConfirmed,
		State:           recorddeletion.DeletionStateWitnessPending,
		FenceEpoch:      9,
		LedgerSequence:  11,
		LedgerEntryHash: testStoreRecordPlatformDigest(0x4a)}
	issued, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(issued.Transport())
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v", err)
	}

	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "from public.deletion_reservations as reservation") {
			return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*string) = reservationID
			*dest[1].(*string) = command.Object.ProjectID
			*dest[2].(*string) = command.Object.ObjectKind
			*dest[3].(*string) = command.Object.ObjectID
			*dest[4].(*[]byte) = append([]byte(nil), command.ActorScopeDigest[:]...)
			*dest[5].(*[]byte) = append([]byte(nil), command.TokenCommitment[:]...)
			*dest[6].(*[]byte) = append([]byte(nil), fingerprintBytes[:]...)
			*dest[7].(*[]byte) = append([]byte(nil), command.BindingDigest[:]...)
			*dest[8].(*int64) = int64(command.WitnessHead.Sequence)
			*dest[9].(*[]byte) = append([]byte(nil), command.WitnessHead.EntryHash[:]...)
			*dest[10].(*time.Time) = expiresAt
			*dest[11].(*bool) = true
			*dest[12].(*string) = operation.OperationID
			*dest[13].(*string) = string(operation.ReasonCode)
			*dest[14].(*string) = string(operation.State)
			*dest[15].(*int64) = int64(operation.FenceEpoch)
			*dest[16].(*int64) = int64(operation.LedgerSequence)
			*dest[17].(*[]byte) = append([]byte(nil), operation.LedgerEntryHash[:]...)
			*dest[18].(*int64) = int64(operation.ReleaseEpoch)
			*dest[19].(*[]byte) = append([]byte(nil), operation.ReceiptDigest[:]...)
			return nil
		}}
	}}
	repository := NewPostgresRecordDeletionRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
	lookup := recorddeletion.PreviewLookup{ReservationID: reservationID, Object: command.Object, Token: token}

	stored, err := repository.ResolvePreview(context.Background(), lookup)
	if err != nil {
		t.Fatalf("ResolvePreview() error = %v", err)
	}
	if stored.ReservationID != reservationID || stored.Object != command.Object ||
		stored.ActorScopeDigest != command.ActorScopeDigest || stored.TokenCommitment != command.TokenCommitment ||
		stored.BindingDigest != command.BindingDigest || stored.WitnessHead != command.WitnessHead ||
		!command.RequestFingerprint.MatchesPersisted(stored.RequestFingerprint) || !stored.ExpiresAt.Equal(expiresAt) ||
		stored.Operation == nil || *stored.Operation != operation || stored.Validate() != nil {
		t.Fatalf("ResolvePreview() = %#v", stored)
	}
	if len(tx.querySQL) != 1 || !tx.committed {
		t.Fatalf("ResolvePreview() queries=%d committed=%t", len(tx.querySQL), tx.committed)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, required := range []string{
		"where reservation.reservation_id = $1",
		"reservation.project_id = $2",
		"reservation.object_kind = $3",
		"reservation.object_id = $4",
		"left join public.record_purge_operations",
		"operation.operation_id is not null",
	} {
		if !strings.Contains(compact, required) {
			t.Errorf("ResolvePreview() SQL missing %q:\n%s", required, compact)
		}
	}
	if strings.Contains(compact, "deletion_token_commitment =") {
		t.Fatalf("ResolvePreview() uses token commitment as lookup key:\n%s", compact)
	}
}

func TestPostgresRecordDeletionResolveOperationStatusLoadsContentFreeAuthorizedProjection(t *testing.T) {
	t.Parallel()

	operation := testStoreDeletionOperation(recorddeletion.DeletionStateOnlinePurged)
	operation.ReceiptDigest = testStoreRecordPlatformDigest(0x62)
	initiatorActorID := "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
		if !strings.Contains(sql, "from public.record_purge_operations as operation") ||
			!strings.Contains(sql, "join public.deletion_reservations as reservation") {
			return fakeRecordPlatformRow{scan: func(...any) error { return errors.New("unexpected query") }}
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*string) = operation.OperationID
			*dest[1].(*string) = operation.ReservationID
			*dest[2].(*string) = operation.Object.ProjectID
			*dest[3].(*string) = operation.Object.ObjectKind
			*dest[4].(*string) = operation.Object.ObjectID
			*dest[5].(*string) = initiatorActorID
			*dest[6].(*string) = string(operation.ReasonCode)
			*dest[7].(*string) = string(operation.State)
			*dest[8].(*int64) = int64(operation.FenceEpoch)
			*dest[9].(*int64) = int64(operation.LedgerSequence)
			*dest[10].(*[]byte) = append([]byte(nil), operation.LedgerEntryHash[:]...)
			*dest[11].(*int64) = int64(operation.ReleaseEpoch)
			*dest[12].(*[]byte) = append([]byte(nil), operation.ReceiptDigest[:]...)
			return nil
		}}
	}}
	repository := testStoreDeletionRepository(tx)

	status, err := repository.ResolveOperationStatus(
		context.Background(),
		recordplatform.ProjectIDDefault,
		operation.OperationID,
	)
	if err != nil {
		t.Fatalf("ResolveOperationStatus() error = %v", err)
	}
	if status.Operation != operation || status.InitiatorActorID != initiatorActorID || status.Validate() != nil {
		t.Fatalf("ResolveOperationStatus() = %#v", status)
	}
	if len(tx.querySQL) != 1 || len(tx.queryArgs) != 1 || !tx.committed {
		t.Fatalf("ResolveOperationStatus() queries=%d args=%d committed=%t", len(tx.querySQL), len(tx.queryArgs), tx.committed)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, required := range []string{
		"where operation.operation_id = $1",
		"operation.project_id = $2",
		"reservation.project_id = operation.project_id",
	} {
		if !strings.Contains(compact, required) {
			t.Errorf("ResolveOperationStatus() SQL missing %q:\n%s", required, compact)
		}
	}
	for _, forbidden := range []string{
		"deletion_token_commitment",
		"request_fingerprint",
		"actor_scope_digest",
		"preview_binding_digest",
		"body_markdown",
		"current_title",
	} {
		if strings.Contains(compact, forbidden) {
			t.Errorf("ResolveOperationStatus() SQL includes forbidden field %q", forbidden)
		}
	}
	if got := tx.queryArgs[0]; len(got) != 2 || got[0] != operation.OperationID || got[1] != string(recordplatform.ProjectIDDefault) {
		t.Fatalf("ResolveOperationStatus() args = %#v", got)
	}
}

func TestPostgresRecordDeletionResolveOperationStatusFailsClosedForMissingProjection(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}}
	repository := testStoreDeletionRepository(tx)

	_, err := repository.ResolveOperationStatus(
		context.Background(),
		recordplatform.ProjectIDDefault,
		"rpo_missing01",
	)
	if !errors.Is(err, recorddeletion.ErrDeletionStatusUnavailable) {
		t.Fatalf("ResolveOperationStatus() error = %v, want ErrDeletionStatusUnavailable", err)
	}
}

func TestPostgresRecordDeletionClaimRejectsAnotherOwner(t *testing.T) {
	t.Parallel()
	command := testStoreDeletionReservePreviewCommand(t)
	fingerprint, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, time.August, 3, 19, 0, 0, 0, time.UTC)
	tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*string) = "rpo_claimtakeover01"
			*dest[1].(*string) = command.Preview.ReservationID
			*dest[2].(*string) = command.Preview.Object.ProjectID
			*dest[3].(*string) = string(recorddeletion.DeletionStateLedgerCommitUnknown)
			*dest[4].(*string) = string(command.DeploymentID)
			*dest[5].(*string) = command.ActorID
			*dest[6].(*string) = string(command.ReasonCode)
			*dest[7].(*int64) = int64(command.DeletionContractVersion)
			*dest[8].(*string) = ""
			*dest[9].(*int64) = 0
			*dest[10].(*[]byte) = nil
			*dest[11].(*int64) = 0
			*dest[12].(*[]byte) = nil
			*dest[13].(*string) = ""
			*dest[14].(*string) = "different_worker"
			*dest[15].(*int64) = 5
			*dest[16].(*time.Time) = observed.Add(time.Minute)
			*dest[17].(*string) = "fenced"
			*dest[18].(*string) = command.Preview.Object.ObjectKind
			*dest[19].(*string) = command.Preview.Object.ObjectID
			*dest[20].(*[]byte) = command.Preview.TokenCommitment[:]
			*dest[21].(*[]byte) = fingerprint[:]
			*dest[22].(*int64) = int64(command.Record.ContentDeliveryEpoch + 1)
			*dest[23].(*string) = "different_worker"
			*dest[24].(*int64) = 5
			expires := observed.Add(time.Minute)
			*dest[25].(**time.Time) = &expires
			*dest[26].(*time.Time) = observed
			return nil
		}}
	}}
	claim, err := testStoreDeletionRepository(tx).ClaimDeletionWork(context.Background(), recorddeletion.DeletionWorkClaimInput{
		OwnerID: "requested_worker", OwnerLeaseDuration: time.Minute,
	})
	if !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) || claim != nil || tx.committed {
		t.Fatalf("ClaimDeletionWork() = %#v, %v, committed=%t; adopted another owner", claim, err, tx.committed)
	}
}

func TestPostgresRecordDeletionContinuationsPreserveCallerClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	appendClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkAppendDeleteCommit)
	resolveClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveDeleteCommit)
	confirmClaim := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkConfirmDeleteWitness)
	resolveOutcome := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveNotCommitted)
	confirmOutcome := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkConfirmNotCommittedWitness)
	promote := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPromotePermanentFence)
	propagate := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPropagatePermanentFence)
	begin := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkBeginOnlinePurge)
	purge := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkPurgeOnline)
	retry := testStoreDeletionWorkerClaim(t, recorddeletion.DeletionWorkResolveRetry)
	entry := testStoreDeletionLedgerEntry(t, appendClaim.Request)
	outcome := testStoreDeletionLedgerEntry(t, resolveOutcome.Request)
	witness := recorddeletion.DeletionWitnessReceipt{Sequence: confirmClaim.Entry.Sequence, EntryHash: confirmClaim.Entry.EntryHash, ProofDigest: testStoreRecordPlatformDigest(0x6b)}
	outcomeWitness := recorddeletion.DeletionWitnessReceipt{Sequence: confirmOutcome.Entry.Sequence, EntryHash: confirmOutcome.Entry.EntryHash, ProofDigest: testStoreRecordPlatformDigest(0x6c)}
	completed := recorddeletion.OnlinePurgeReceipt{OperationID: purge.Operation.OperationID, ReceiptDigest: testStoreRecordPlatformDigest(0x6d)}
	tests := []struct {
		name  string
		claim recorddeletion.ClaimedDeletionWork
		run   func(*PostgresRecordDeletionRepository) error
	}{
		{"append unknown", appendClaim, func(r *PostgresRecordDeletionRepository) error { return r.MarkDeleteCommitUnknown(ctx, appendClaim) }},
		{"record entry", appendClaim, func(r *PostgresRecordDeletionRepository) error { return r.RecordDeleteEntry(ctx, appendClaim, entry) }},
		{"record witnessed entry", confirmClaim, func(r *PostgresRecordDeletionRepository) error {
			return r.MarkDeleteWitnessed(ctx, confirmClaim, witness)
		}},
		{"outcome unknown", resolveClaim, func(r *PostgresRecordDeletionRepository) error {
			return r.MarkOutcomeCommitUnknown(ctx, resolveClaim, resolveOutcome.Operation.ReleaseEpoch)
		}},
		{"record outcome", resolveOutcome, func(r *PostgresRecordDeletionRepository) error {
			return r.RecordOutcomeEntry(ctx, resolveOutcome, outcome)
		}},
		{"finalize outcome", confirmOutcome, func(r *PostgresRecordDeletionRepository) error {
			return r.FinalizeNotCommitted(ctx, confirmOutcome, outcomeWitness)
		}},
		{"promote", promote, func(r *PostgresRecordDeletionRepository) error { return r.PromotePermanentFence(ctx, promote) }},
		{"read fence", propagate, func(r *PostgresRecordDeletionRepository) error { return r.MarkReadFenced(ctx, propagate) }},
		{"begin purge", begin, func(r *PostgresRecordDeletionRepository) error { return r.BeginOnlinePurge(ctx, begin) }},
		{"complete purge", purge, func(r *PostgresRecordDeletionRepository) error { return r.CompleteOnlinePurge(ctx, purge, completed) }},
		{"mark retry", purge, func(r *PostgresRecordDeletionRepository) error {
			return r.MarkRetryRequired(ctx, purge, recorddeletion.DeletionWorkPurgeOnline)
		}},
		{"resume retry", retry, func(r *PostgresRecordDeletionRepository) error {
			return r.ResumeRetry(ctx, retry, recorddeletion.DeletionWorkPurgeOnline)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, accepted := range []bool{true, false} {
				tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, _ string, args ...any) pgx.Row {
					if len(args) != 1 {
						t.Fatal("continuation did not send one fixed envelope")
					}
					body, ok := args[0].([]byte)
					if !ok {
						t.Fatal("continuation envelope is not bytes")
					}
					var owner deletionV1OwnerEnvelope
					if err := json.Unmarshal(body, &owner); err != nil {
						t.Fatal(err)
					}
					expiry, err := time.Parse(time.RFC3339Nano, owner.OwnerExpiresAt)
					if err != nil || owner.OperationID != tc.claim.Operation.OperationID || owner.ReservationID != tc.claim.Operation.ReservationID ||
						owner.ProjectID != tc.claim.Operation.Object.ProjectID || owner.RecordID != tc.claim.Operation.Object.ObjectID ||
						owner.FenceEpoch != uint64(tc.claim.Operation.FenceEpoch) || owner.OwnerID != tc.claim.Owner.OwnerID ||
						owner.OwnerGeneration != tc.claim.Owner.Generation || !expiry.Equal(tc.claim.Owner.ExpiresAt) {
						t.Fatalf("continuation changed caller claim: %#v", owner)
					}
					return fakeRecordPlatformRow{scan: func(dest ...any) error { *dest[0].(*bool) = accepted; return nil }}
				}}
				repository := testStoreDeletionRepository(tx)
				repository.newAuditID = func() (string, error) { return "rda_0123456789abcdef", nil }
				err := tc.run(repository)
				if accepted && (err != nil || !tx.committed) {
					t.Fatalf("accepted continuation = %v, committed=%t", err, tx.committed)
				}
				if !accepted && (!errors.Is(err, recordplatform.ErrLostOwnerLease) || tx.committed) {
					t.Fatalf("lost-owner continuation = %v, committed=%t", err, tx.committed)
				}
			}
		})
	}
}

func TestPostgresRecordDeletionCoreHealthAndPreviewFailClosed(t *testing.T) {
	t.Parallel()

	t.Run("missing owned surface", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*int64) = int64(len(recorddeletion.RecordCoreSurfaceNames()) - 1)
				return nil
			}}
		}}
		repository := testStoreDeletionRepository(tx)
		if _, err := repository.RecordCoreHealth(context.Background()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
			t.Fatalf("RecordCoreHealth() error = %v, want ErrDeletionSafetyUnavailable", err)
		}
		if tx.committed {
			t.Fatal("RecordCoreHealth() committed an invalid surface snapshot")
		}
	})

	t.Run("stale preview tuple", func(t *testing.T) {
		tx := &fakeRecordPlatformTx{}
		repository := testStoreDeletionRepository(tx)
		target := recorddeletion.PreviewTarget{
			Object:            recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_corepreview01"},
			CurrentRevisionID: "rrv_corepreview01", LockVersion: 2, AuthorizationEpoch: 3,
			ContentDeliveryEpoch: 4, DependencyGraphDigest: testStoreRecordPlatformDigest(0x52),
		}
		if _, err := repository.PreviewRecordCore(context.Background(), target); !errors.Is(err, recorddeletion.ErrDeletionPreviewStale) {
			t.Fatalf("PreviewRecordCore() error = %v, want ErrDeletionPreviewStale", err)
		}
		if tx.committed {
			t.Fatal("PreviewRecordCore() committed a stale preview snapshot")
		}
	})
}

func TestPostgresRecordDeletionCoreCommandPreservesClaimAndValidatesReceipt(t *testing.T) {
	t.Parallel()
	operation := testStoreDeletionOperation(recorddeletion.DeletionStateOnlinePurging)
	operation.ContractVersion = recorddeletion.DeletionContractV2
	operation.LedgerSequence = 0
	operation.LedgerEntryHash = [32]byte{}
	claim := recorddeletion.OperationOwnerClaim{OperationID: operation.OperationID, Owner: recordplatform.OwnerLease{
		OwnerID: "deletion_worker_01", Generation: 4, ExpiresAt: time.Date(2026, time.August, 3, 16, 5, 0, 0, time.UTC),
	}}
	target := recorddeletion.PurgeTarget{Operation: operation, OwnerClaim: claim}
	surface := recorddeletion.RecordCoreSurfaceDigest()
	digest := digestRecordCorePurgeReceipt(operation, surface, 9)
	verifiedAt := time.Date(2026, time.August, 3, 16, 0, 0, 0, time.UTC)
	makeTx := func(corrupt bool) *fakeRecordPlatformTx {
		return &fakeRecordPlatformTx{queryRow: func(_ context.Context, _ string, args ...any) pgx.Row {
			if len(args) != 1 {
				t.Fatal("core command did not send one envelope")
			}
			body, ok := args[0].([]byte)
			if !ok {
				t.Fatal("core command envelope is not bytes")
			}
			var envelope clientComparisonPurgeEnvelope
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatal(err)
			}
			expires, err := time.Parse(time.RFC3339Nano, envelope.OwnerExpiresAt)
			if err != nil || envelope.OperationID != operation.OperationID || envelope.ReservationID != operation.ReservationID ||
				envelope.RecordID != operation.Object.ObjectID || envelope.FenceEpoch != uint64(operation.FenceEpoch) ||
				envelope.OwnerID != claim.Owner.OwnerID || envelope.OwnerGeneration != claim.Owner.Generation || !expires.Equal(claim.Owner.ExpiresAt) {
				t.Fatalf("core command changed owner claim: %#v", envelope)
			}
			return fakeRecordPlatformRow{scan: func(dest ...any) error {
				*dest[0].(*[]byte) = append([]byte(nil), surface[:]...)
				got := digest
				if corrupt {
					got[0] ^= 1
				}
				*dest[1].(*[]byte) = append([]byte(nil), got[:]...)
				*dest[2].(*int64) = 9
				*dest[3].(*time.Time) = verifiedAt
				return nil
			}}
		}}
	}
	tx := makeTx(false)
	repository := testStoreDeletionRepository(tx)
	adapter, err := recorddeletion.NewCoreAdapter(testStoreCorePurgeAdapter{repository: repository})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := adapter.PurgeDeletion(context.Background(), target)
	if err != nil || receipt.ReceiptDigest != digest || receipt.RemovedRowCount != 9 || !tx.committed {
		t.Fatalf("PurgeDeletion() = %#v, %v, committed=%t", receipt, err, tx.committed)
	}
	verifyTx := makeTx(false)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return verifyTx, nil }
	if err := adapter.VerifyDeletion(context.Background(), target, receipt); err != nil || !verifyTx.committed {
		t.Fatalf("VerifyDeletion() = %v, committed=%t", err, verifyTx.committed)
	}
	corruptTx := makeTx(true)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return corruptTx, nil }
	if _, err := adapter.PurgeDeletion(context.Background(), target); err == nil || corruptTx.committed {
		t.Fatalf("corrupt receipt accepted: %v, committed=%t", err, corruptTx.committed)
	}
	badTarget := target
	badTarget.OwnerClaim = recorddeletion.OperationOwnerClaim{}
	if _, err := adapter.PurgeDeletion(context.Background(), badTarget); err == nil {
		t.Fatal("purge accepted a missing caller owner")
	}
}

func TestRecordCorePurgeReceiptDigestDoesNotRetainObjectIdentity(t *testing.T) {
	t.Parallel()

	operation := testStoreDeletionOperation(recorddeletion.DeletionStateOnlinePurging)
	surfaceDigest := recorddeletion.RecordCoreSurfaceDigest()
	want := digestRecordCorePurgeReceipt(operation, surfaceDigest, 9)

	otherObject := operation
	otherObject.Object.ProjectID = "another_project"
	otherObject.Object.ObjectID = "rec_anotherpurge01"
	if got := digestRecordCorePurgeReceipt(otherObject, surfaceDigest, 9); got != want {
		t.Fatal("content-free core purge receipt digest changes with project/record identity")
	}
}

type testStoreCorePurgeAdapter struct {
	repository *PostgresRecordDeletionRepository
}

func (adapter testStoreCorePurgeAdapter) RecordCoreHealth(context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	return recorddeletion.AdapterHealthSnapshot{}, nil
}

func (adapter testStoreCorePurgeAdapter) PreviewRecordCore(context.Context, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	return recorddeletion.AdapterPreviewSnapshot{}, nil
}

func (adapter testStoreCorePurgeAdapter) PurgeRecordCore(ctx context.Context, command recorddeletion.CorePurgeCommand) (recorddeletion.AdapterPurgeReceipt, error) {
	return adapter.repository.PurgeRecordCore(ctx, command)
}

func (adapter testStoreCorePurgeAdapter) VerifyRecordCorePurge(ctx context.Context, command recorddeletion.CorePurgeCommand, receipt recorddeletion.AdapterPurgeReceipt) error {
	return adapter.repository.VerifyRecordCorePurge(ctx, command, receipt)
}

func testStoreDeletionRepository(tx *fakeRecordPlatformTx) *PostgresRecordDeletionRepository {
	return &PostgresRecordDeletionRepository{
		platform: &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
		},
	}
}

func testStoreDeletionOperation(state recorddeletion.DeletionState) recorddeletion.DeletionOperation {
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_corepurge01", ReservationID: "drs_corepurge01",
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_corepurge01",
		},
		ReasonCode:      recorddeletion.DeletionReasonUserConfirmed,
		State:           state,
		FenceEpoch:      7,
		LedgerSequence:  11,
		LedgerEntryHash: testStoreRecordPlatformDigest(0x61)}
	return operation
}

func testStoreDeletionWorkerClaim(t *testing.T, stage recorddeletion.DeletionWorkStage) recorddeletion.ClaimedDeletionWork {
	t.Helper()
	command := testStoreDeletionReservePreviewCommand(t)
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_workercutpoint01", ReservationID: command.Preview.ReservationID,
		Object:     command.Preview.Object,
		ReasonCode: command.ReasonCode,
		State:      recorddeletion.DeletionStateProvisionalFenced,
		FenceEpoch: command.Record.ContentDeliveryEpoch + 1}
	request := recorddeletion.LedgerAppendRequest{
		EntryType:               recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID:            command.DeploymentID,
		ProjectID:               recordplatform.ProjectID(command.Preview.Object.ProjectID),
		OperationID:             operation.OperationID,
		ActorID:                 command.ActorID,
		Object:                  operation.Object,
		TokenCommitment:         command.Preview.TokenCommitment,
		RequestFingerprint:      command.Preview.RequestFingerprint,
		ReasonCode:              operation.ReasonCode,
		DeletionContractVersion: command.DeletionContractVersion,
	}
	claim := recorddeletion.ClaimedDeletionWork{
		Operation: operation,
		Owner: recordplatform.OwnerLease{
			OwnerID: "deletion_worker_01", Generation: 3,
			ExpiresAt: time.Date(2026, time.August, 3, 21, 0, 0, 0, time.UTC),
		},
		Stage: stage, Request: request,
	}
	switch stage {
	case recorddeletion.DeletionWorkResolveDeleteCommit:
		claim.Operation.State = recorddeletion.DeletionStateLedgerCommitUnknown
	case recorddeletion.DeletionWorkConfirmDeleteWitness:
		claim.Operation.State = recorddeletion.DeletionStateWitnessPending
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Entry = &entry
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
	case recorddeletion.DeletionWorkResolveNotCommitted, recorddeletion.DeletionWorkConfirmNotCommittedWitness:
		claim.Operation.State = recorddeletion.DeletionStateReleasePending
		claim.Operation.ReleaseEpoch = 5
		claim.Request = request.AttemptNotCommitted(claim.Operation.ReleaseEpoch)
		if stage == recorddeletion.DeletionWorkConfirmNotCommittedWitness {
			entry := testStoreDeletionLedgerEntry(t, claim.Request)
			claim.Entry = &entry
			claim.Operation.LedgerSequence = entry.Sequence
			claim.Operation.LedgerEntryHash = entry.EntryHash
		}
	case recorddeletion.DeletionWorkPromotePermanentFence:
		claim.Operation.State = recorddeletion.DeletionStateDeleteRequested
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
	case recorddeletion.DeletionWorkPropagatePermanentFence,
		recorddeletion.DeletionWorkBeginOnlinePurge,
		recorddeletion.DeletionWorkPurgeOnline:
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
		switch stage {
		case recorddeletion.DeletionWorkPropagatePermanentFence:
			claim.Operation.State = recorddeletion.DeletionStateFencePropagating
		case recorddeletion.DeletionWorkBeginOnlinePurge:
			claim.Operation.State = recorddeletion.DeletionStateReadFenced
		case recorddeletion.DeletionWorkPurgeOnline:
			claim.Operation.State = recorddeletion.DeletionStateOnlinePurging
		}
	case recorddeletion.DeletionWorkResolveRetry:
		entry := testStoreDeletionLedgerEntry(t, request)
		claim.Operation.State = recorddeletion.DeletionStateRetryRequired
		claim.Operation.LedgerSequence = entry.Sequence
		claim.Operation.LedgerEntryHash = entry.EntryHash
		claim.RetryStage = recorddeletion.DeletionWorkPurgeOnline
	}
	if err := claim.Validate(); err != nil {
		t.Fatalf("test deletion worker claim invalid: %v", err)
	}
	return claim
}

func testStoreDeletionLedgerEntry(t *testing.T, request recorddeletion.LedgerAppendRequest) recorddeletion.DeletionLedgerEntry {
	t.Helper()
	entry := recorddeletion.DeletionLedgerEntry{
		Request: request, Sequence: 13, EntryHash: testStoreRecordPlatformDigest(0x6a),
	}
	if err := entry.Validate(); err != nil {
		t.Fatalf("test deletion ledger entry invalid: %v", err)
	}
	return entry
}

func testStoreDeletionCreatePreviewCommand(t *testing.T) recorddeletion.CreatePreviewCommand {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordPermanentDelete,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testStoreRecordPlatformDigest(0x31),
		RequestScopeDigest: testStoreRecordPlatformDigest(0x32),
		PayloadDigest:      testStoreRecordPlatformDigest(0x33),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return recorddeletion.CreatePreviewCommand{
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   "rec_previewstore01",
		},
		ActorScopeDigest:   testStoreRecordPlatformDigest(0x31),
		TokenCommitment:    testStoreRecordPlatformDigest(0x34),
		RequestFingerprint: fingerprint,
		BindingDigest:      testStoreRecordPlatformDigest(0x35),
		Record: recorddeletion.DeletionRecordSnapshot{
			RecordID:                 "rec_previewstore01",
			CurrentRevisionID:        "rrv_previewstore01",
			LockVersion:              4,
			AuthorizationEpoch:       5,
			ContentDeliveryEpoch:     6,
			Authorization:            recordauth.ResourceScope{Version: recordauth.ResourceScopeVersionV1, ProjectID: recordauth.ProjectIDDefault},
			DependencyGraphDigest:    testStoreRecordPlatformDigest(0x36),
			BackupInventoryDigest:    testStoreRecordPlatformDigest(0x37),
			ProcessorInventoryDigest: testStoreRecordPlatformDigest(0x38),
		},
		WitnessHead:            recorddeletion.WitnessHead{Sequence: 7, EntryHash: testStoreRecordPlatformDigest(0x39)},
		AdapterReadinessDigest: testStoreRecordPlatformDigest(0x3a),
		AdapterPreviewDigest:   testStoreRecordPlatformDigest(0x3b),
		TTL:                    recorddeletion.DeletionPreviewTTL,
	}
}

func testStoreDeletionReservePreviewCommand(t *testing.T) recorddeletion.ReservePreviewCommand {
	t.Helper()
	create := testStoreDeletionCreatePreviewCommand(t)
	fingerprintBytes, err := create.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	persisted, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(fingerprintBytes[:])
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
	}
	preview := recorddeletion.StoredPreview{
		ReservationID:      "drs_0123456789abcdef",
		Object:             create.Object,
		ActorScopeDigest:   create.ActorScopeDigest,
		TokenCommitment:    create.TokenCommitment,
		RequestFingerprint: persisted,
		BindingDigest:      create.BindingDigest,
		WitnessHead:        create.WitnessHead,
		ExpiresAt:          time.Date(2026, time.August, 3, 18, 10, 0, 0, time.UTC),
	}
	return recorddeletion.ReservePreviewCommand{
		DeploymentID:            recordplatform.DeploymentID("dp-" + strings.Repeat("a", 64)),
		ActorID:                 "usr_0123456789abcdef01234567",
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
		Preview:                 preview,
		Record:                  create.Record,
		ExpectedBindingDigest:   create.BindingDigest,
		RequestFingerprint:      create.RequestFingerprint,
		ObservedWitnessHead:     recorddeletion.WitnessHead{Sequence: 9, EntryHash: testStoreRecordPlatformDigest(0x4b)},
		OwnerID:                 "deletion_worker_01",
		OwnerLeaseDuration:      2 * time.Minute,
		ReasonCode:              recorddeletion.DeletionReasonUserConfirmed,
	}
}
