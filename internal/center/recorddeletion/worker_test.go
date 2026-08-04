package recorddeletion

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordplatform"
)

func TestDeletionWorkerDeleteAppendUnknownKeepsFenceAndNeverWitnessesOrPurges(t *testing.T) {
	t.Parallel()

	claim := deletionTestWorkClaim(t, DeletionWorkAppendDeleteCommit)
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
	ledger := &deletionLedgerStub{appendErr: errors.New("transport timeout")}
	witness := &deletionEntryWitnessStub{}
	purger := &deletionOnlinePurgerStub{}
	worker := deletionTestWorker(repository, ledger, witness, purger)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := repository.calls, []string{"claim", "delete-unknown"}; !deletionEqualStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	if ledger.appendCalls != 1 || witness.calls != 0 || purger.calls != 0 || repository.finalizeNotCommittedCalls != 0 {
		t.Fatalf("unknown append state ledger=%d witness=%d purge=%d release=%d, want 1/0/0/0", ledger.appendCalls, witness.calls, purger.calls, repository.finalizeNotCommittedCalls)
	}
}

func TestDeletionWorkerPersistsDeleteEntryBeforeWitnessAndWitnessBeforePermanentFence(t *testing.T) {
	t.Parallel()

	appendClaim := deletionTestWorkClaim(t, DeletionWorkAppendDeleteCommit)
	entry := deletionTestLedgerEntry(t, appendClaim.Request)
	witnessClaim := deletionTestWorkClaim(t, DeletionWorkConfirmDeleteWitness)
	witnessClaim.Entry = &entry
	witnessClaim.Operation.LedgerSequence = entry.Sequence
	witnessClaim.Operation.LedgerEntryHash = entry.EntryHash
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&appendClaim, &witnessClaim}}
	ledger := &deletionLedgerStub{appendEntry: entry}
	witnessReceipt := deletionTestWitnessReceipt(entry)
	witness := &deletionEntryWitnessStub{receipt: witnessReceipt}
	purger := &deletionOnlinePurgerStub{}
	worker := deletionTestWorker(repository, ledger, witness, purger)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("append RunOnce() error = %v", err)
	}
	if witness.calls != 0 || purger.calls != 0 {
		t.Fatalf("append pass crossed cut point witness=%d purge=%d, want zero", witness.calls, purger.calls)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("witness RunOnce() error = %v", err)
	}
	if got, want := repository.calls, []string{"claim", "delete-entry", "claim", "delete-witnessed"}; !deletionEqualStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	if witness.calls != 1 || repository.promoteCalls != 0 || purger.calls != 0 {
		t.Fatalf("witness pass calls witness=%d promote=%d purge=%d, want 1/0/0", witness.calls, repository.promoteCalls, purger.calls)
	}
}

func TestDeletionWorkerWitnessFailureLeavesPendingStateWithoutCompensation(t *testing.T) {
	t.Parallel()

	claim := deletionTestWorkClaim(t, DeletionWorkConfirmDeleteWitness)
	entry := deletionTestLedgerEntry(t, claim.Request)
	claim.Entry = &entry
	claim.Operation.LedgerSequence = entry.Sequence
	claim.Operation.LedgerEntryHash = entry.EntryHash
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
	worker := deletionTestWorker(repository, &deletionLedgerStub{}, &deletionEntryWitnessStub{err: errors.New("witness unavailable")}, &deletionOnlinePurgerStub{})

	err := worker.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() error = nil, want witness failure")
	}
	if got, want := repository.calls, []string{"claim"}; !deletionEqualStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	if repository.markDeleteWitnessedCalls != 0 || repository.finalizeNotCommittedCalls != 0 {
		t.Fatal("witness failure advanced or released the operation")
	}
}

func TestDeletionWorkerUnknownDeleteRequiresSealedAbsenceProofBeforeOutcome(t *testing.T) {
	t.Parallel()

	t.Run("unresolved remains fenced", func(t *testing.T) {
		claim := deletionTestWorkClaim(t, DeletionWorkResolveDeleteCommit)
		repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
		ledger := &deletionLedgerStub{resolution: LedgerResolution{Kind: LedgerResolutionUnresolved}}
		worker := deletionTestWorker(repository, ledger, &deletionEntryWitnessStub{}, &deletionOnlinePurgerStub{})

		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
		if got, want := repository.calls, []string{"claim"}; !deletionEqualStrings(got, want) {
			t.Fatalf("repository calls = %#v, want %#v", got, want)
		}
	})

	t.Run("unsealed absence is rejected", func(t *testing.T) {
		claim := deletionTestWorkClaim(t, DeletionWorkResolveDeleteCommit)
		repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
		ledger := &deletionLedgerStub{resolution: LedgerResolution{Kind: LedgerResolutionAbsenceProven}}
		worker := deletionTestWorker(repository, ledger, &deletionEntryWitnessStub{}, &deletionOnlinePurgerStub{})

		err := worker.RunOnce(context.Background())
		if !errors.Is(err, ErrInvalidDeletionWorkerResult) {
			t.Fatalf("RunOnce() error = %v, want ErrInvalidDeletionWorkerResult", err)
		}
		if ledger.appendCalls != 0 || repository.outcomeUnknownCalls != 0 || repository.recordOutcomeCalls != 0 || repository.finalizeNotCommittedCalls != 0 {
			t.Fatal("unsealed absence appended, persisted, or released an outcome")
		}
	})
}

func TestDeletionWorkerSealedDeleteAbsenceAppendsOutcomeThenWaitsForWitnessBeforeRelease(t *testing.T) {
	t.Parallel()

	resolveClaim := deletionTestWorkClaim(t, DeletionWorkResolveDeleteCommit)
	proof, err := NewDeletionAbsenceProof(3, deletionTestDigest(90))
	if err != nil {
		t.Fatalf("NewDeletionAbsenceProof() error = %v", err)
	}
	outcomeRequest := resolveClaim.Request.AttemptNotCommitted(proof.ReleaseEpoch())
	outcomeEntry := deletionTestLedgerEntry(t, outcomeRequest)
	confirmClaim := deletionTestWorkClaim(t, DeletionWorkConfirmNotCommittedWitness)
	confirmClaim.Request = outcomeRequest
	confirmClaim.Entry = &outcomeEntry
	confirmClaim.Operation.State = DeletionStateReleasePending
	confirmClaim.Operation.LedgerSequence = outcomeEntry.Sequence
	confirmClaim.Operation.LedgerEntryHash = outcomeEntry.EntryHash
	confirmClaim.Operation.ReleaseEpoch = proof.ReleaseEpoch()
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&resolveClaim, &confirmClaim}}
	ledger := &deletionLedgerStub{
		resolution:  NewAbsenceProvenLedgerResolution(proof),
		appendEntry: outcomeEntry,
	}
	witness := &deletionEntryWitnessStub{receipt: deletionTestWitnessReceipt(outcomeEntry)}
	purger := &deletionOnlinePurgerStub{}
	worker := deletionTestWorker(repository, ledger, witness, purger)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("resolve RunOnce() error = %v", err)
	}
	if repository.recordOutcomeCalls != 1 || repository.finalizeNotCommittedCalls != 0 || witness.calls != 0 {
		t.Fatalf("outcome append state record=%d release=%d witness=%d, want 1/0/0", repository.recordOutcomeCalls, repository.finalizeNotCommittedCalls, witness.calls)
	}
	if ledger.lastAppend.EntryType != LedgerEntryAttemptNotCommitted || ledger.lastAppend.ReleaseEpoch != proof.ReleaseEpoch() || ledger.lastAppend.DeletionContractVersion != 0 {
		t.Fatalf("outcome append request = %#v", ledger.lastAppend)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("outcome witness RunOnce() error = %v", err)
	}
	if repository.finalizeNotCommittedCalls != 1 || witness.calls != 1 || purger.calls != 0 {
		t.Fatalf("outcome witness state release=%d witness=%d purge=%d, want 1/1/0", repository.finalizeNotCommittedCalls, witness.calls, purger.calls)
	}
	if got, want := repository.calls, []string{"claim", "outcome-entry", "claim", "not-committed"}; !deletionEqualStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
}

func TestDeletionWorkerOutcomeAppendUnknownRemainsFencedAndNeverReleases(t *testing.T) {
	t.Parallel()

	claim := deletionTestWorkClaim(t, DeletionWorkResolveDeleteCommit)
	proof, err := NewDeletionAbsenceProof(4, deletionTestDigest(91))
	if err != nil {
		t.Fatalf("NewDeletionAbsenceProof() error = %v", err)
	}
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
	ledger := &deletionLedgerStub{resolution: NewAbsenceProvenLedgerResolution(proof), appendErr: errors.New("outcome ack lost")}
	worker := deletionTestWorker(repository, ledger, &deletionEntryWitnessStub{}, &deletionOnlinePurgerStub{})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repository.outcomeUnknownCalls != 1 || repository.finalizeNotCommittedCalls != 0 {
		t.Fatalf("outcome unknown state marked=%d release=%d, want 1/0", repository.outcomeUnknownCalls, repository.finalizeNotCommittedCalls)
	}
	if got, want := repository.calls, []string{"claim", "outcome-unknown"}; !deletionEqualStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
}

func TestDeletionWorkerResolvesUnknownCommittedDeleteWithoutAppendingOutcome(t *testing.T) {
	t.Parallel()

	claim := deletionTestWorkClaim(t, DeletionWorkResolveDeleteCommit)
	entry := deletionTestLedgerEntry(t, claim.Request)
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
	ledger := &deletionLedgerStub{resolution: NewCommittedLedgerResolution(entry)}
	worker := deletionTestWorker(repository, ledger, &deletionEntryWitnessStub{}, &deletionOnlinePurgerStub{})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if ledger.appendCalls != 0 || repository.recordDeleteCalls != 1 || repository.recordOutcomeCalls != 0 || repository.finalizeNotCommittedCalls != 0 {
		t.Fatalf("resolved commit state append=%d delete=%d outcome=%d release=%d", ledger.appendCalls, repository.recordDeleteCalls, repository.recordOutcomeCalls, repository.finalizeNotCommittedCalls)
	}
}

func TestDeletionWorkerPermanentFenceAndPurgeUseSeparateDurableCutPoints(t *testing.T) {
	t.Parallel()

	promote := deletionTestWorkClaim(t, DeletionWorkPromotePermanentFence)
	propagate := deletionTestWorkClaim(t, DeletionWorkPropagatePermanentFence)
	beginPurge := deletionTestWorkClaim(t, DeletionWorkBeginOnlinePurge)
	purge := deletionTestWorkClaim(t, DeletionWorkPurgeOnline)
	repository := &deletionWorkerRepositoryStub{
		claims:           []*ClaimedDeletionWork{&promote, &propagate, &beginPurge, &purge},
		propagationReady: true,
	}
	receipt := OnlinePurgeReceipt{OperationID: purge.Operation.OperationID, ReceiptDigest: deletionTestDigest(99)}
	purger := &deletionOnlinePurgerStub{receipt: receipt}
	worker := deletionTestWorker(repository, &deletionLedgerStub{}, &deletionEntryWitnessStub{}, purger)

	for pass := 0; pass < 4; pass++ {
		if err := worker.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce() pass %d error = %v", pass, err)
		}
	}
	if got, want := repository.calls, []string{
		"claim", "permanent-fence",
		"claim", "propagation-ready", "read-fenced",
		"claim", "begin-purge",
		"claim", "online-purged",
	}; !deletionEqualStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	if purger.calls != 1 || repository.completePurgeCalls != 1 {
		t.Fatalf("purge calls=%d complete=%d, want 1/1", purger.calls, repository.completePurgeCalls)
	}
}

func TestDeletionWorkerPurgeFailureRecordsRetryWithoutPersistingErrorOrReceipt(t *testing.T) {
	t.Parallel()

	claim := deletionTestWorkClaim(t, DeletionWorkPurgeOnline)
	repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
	secret := "deleted record body must not persist"
	purger := &deletionOnlinePurgerStub{err: errors.New(secret)}
	worker := deletionTestWorker(repository, &deletionLedgerStub{}, &deletionEntryWitnessStub{}, purger)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repository.retryCalls != 1 || repository.completePurgeCalls != 0 || strings.Contains(strings.Join(repository.calls, " "), secret) {
		t.Fatalf("purge failure retry=%d complete=%d calls=%#v", repository.retryCalls, repository.completePurgeCalls, repository.calls)
	}
}

func TestDeletionWorkerPostCommitCutPointFailuresRecordRetryRequired(t *testing.T) {
	t.Parallel()

	failure := errors.New("post-commit cut point failed")
	tests := []struct {
		name         string
		stage        DeletionWorkStage
		configure    func(*deletionWorkerRepositoryStub, *deletionOnlinePurgerStub)
		wantCalls    []string
		wantPurge    int
		wantComplete int
	}{
		{
			name:  "promote permanent fence",
			stage: DeletionWorkPromotePermanentFence,
			configure: func(repository *deletionWorkerRepositoryStub, _ *deletionOnlinePurgerStub) {
				repository.promoteErr = failure
			},
			wantCalls: []string{"claim", "permanent-fence", "retry-required"},
		},
		{
			name:  "read permanent fence projection",
			stage: DeletionWorkPropagatePermanentFence,
			configure: func(repository *deletionWorkerRepositoryStub, _ *deletionOnlinePurgerStub) {
				repository.propagationErr = failure
			},
			wantCalls: []string{"claim", "propagation-ready", "retry-required"},
		},
		{
			name:  "persist read fence",
			stage: DeletionWorkPropagatePermanentFence,
			configure: func(repository *deletionWorkerRepositoryStub, _ *deletionOnlinePurgerStub) {
				repository.propagationReady = true
				repository.markReadFencedErr = failure
			},
			wantCalls: []string{"claim", "propagation-ready", "read-fenced", "retry-required"},
		},
		{
			name:  "begin online purge",
			stage: DeletionWorkBeginOnlinePurge,
			configure: func(repository *deletionWorkerRepositoryStub, _ *deletionOnlinePurgerStub) {
				repository.beginPurgeErr = failure
			},
			wantCalls: []string{"claim", "begin-purge", "retry-required"},
		},
		{
			name:  "persist online purge receipt",
			stage: DeletionWorkPurgeOnline,
			configure: func(repository *deletionWorkerRepositoryStub, purger *deletionOnlinePurgerStub) {
				claim := deletionTestWorkClaim(t, DeletionWorkPurgeOnline)
				purger.receipt = OnlinePurgeReceipt{OperationID: claim.Operation.OperationID, ReceiptDigest: deletionTestDigest(100)}
				repository.completePurgeErr = failure
			},
			wantCalls:    []string{"claim", "online-purged", "retry-required"},
			wantPurge:    1,
			wantComplete: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claim := deletionTestWorkClaim(t, tt.stage)
			repository := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
			purger := &deletionOnlinePurgerStub{}
			tt.configure(repository, purger)
			worker := deletionTestWorker(repository, &deletionLedgerStub{}, &deletionEntryWitnessStub{}, purger)

			if err := worker.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if repository.retryCalls != 1 || purger.calls != tt.wantPurge || repository.completePurgeCalls != tt.wantComplete {
				t.Fatalf("retry/purge/complete = %d/%d/%d, want 1/%d/%d",
					repository.retryCalls, purger.calls, repository.completePurgeCalls, tt.wantPurge, tt.wantComplete)
			}
			if !deletionEqualStrings(repository.calls, tt.wantCalls) {
				t.Fatalf("repository calls = %#v, want %#v", repository.calls, tt.wantCalls)
			}
		})
	}
}

func TestDeletionWorkerRejectsNilCancelledAndTypedNilDependencies(t *testing.T) {
	t.Parallel()

	validRepository := &deletionWorkerRepositoryStub{}
	validLedger := &deletionLedgerStub{}
	validWitness := &deletionEntryWitnessStub{}
	validPurger := &deletionOnlinePurgerStub{}
	var nilRepository *deletionWorkerRepositoryStub
	var nilLedger *deletionLedgerStub
	var nilWitness *deletionEntryWitnessStub
	var nilPurger *deletionOnlinePurgerStub
	for _, tt := range []struct {
		name       string
		repository DeletionWorkerRepository
		ledger     DeletionLedger
		witness    DeletionEntryWitness
		purger     DeletionOnlinePurger
	}{
		{name: "repository", repository: nilRepository, ledger: validLedger, witness: validWitness, purger: validPurger},
		{name: "ledger", repository: validRepository, ledger: nilLedger, witness: validWitness, purger: validPurger},
		{name: "witness", repository: validRepository, ledger: validLedger, witness: nilWitness, purger: validPurger},
		{name: "purger", repository: validRepository, ledger: validLedger, witness: validWitness, purger: nilPurger},
	} {
		t.Run(tt.name, func(t *testing.T) {
			worker := NewDeletionWorker(tt.repository, tt.ledger, tt.witness, tt.purger, deletionTestWorkerOptions())
			if err := worker.RunOnce(context.Background()); !errors.Is(err, ErrInvalidDeletionWorker) {
				t.Fatalf("RunOnce() error = %v, want ErrInvalidDeletionWorker", err)
			}
		})
	}

	worker := deletionTestWorker(validRepository, validLedger, validWitness, validPurger)
	if err := worker.RunOnce(nil); !errors.Is(err, ErrInvalidDeletionWorker) {
		t.Fatalf("RunOnce(nil) error = %v, want ErrInvalidDeletionWorker", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.RunOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOnce(cancelled) error = %v, want context.Canceled", err)
	}
	if validRepository.claimCalls != 0 {
		t.Fatalf("pre-cancelled worker claim calls = %d, want zero", validRepository.claimCalls)
	}
}

func TestDeletionWorkerRunLogsOnlyFixedSafeMessage(t *testing.T) {
	var logs bytes.Buffer
	secret := "drt1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	ctx, cancel := context.WithCancel(context.Background())
	repository := &deletionWorkerRepositoryStub{claimErr: errors.New("dependency carries " + secret), claimHook: cancel}
	options := deletionTestWorkerOptions()
	options.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	options.PollInterval = time.Millisecond
	worker := NewDeletionWorker(repository, &deletionLedgerStub{}, &deletionEntryWitnessStub{}, &deletionOnlinePurgerStub{}, options)

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := logs.String(); strings.Contains(got, secret) || !strings.Contains(got, "record deletion pass failed") {
		t.Fatalf("Run() log = %q, want fixed safe message", got)
	}
}

func deletionTestWorker(repository DeletionWorkerRepository, ledger DeletionLedger, witness DeletionEntryWitness, purger DeletionOnlinePurger) *DeletionWorker {
	return NewDeletionWorker(repository, ledger, witness, purger, deletionTestWorkerOptions())
}

func deletionTestWorkerOptions() DeletionWorkerOptions {
	return DeletionWorkerOptions{
		OwnerID:            "deletion_worker_01",
		OwnerLeaseDuration: 2 * time.Minute,
		PollInterval:       time.Second,
	}
}

func deletionTestWorkClaim(t *testing.T, stage DeletionWorkStage) ClaimedDeletionWork {
	t.Helper()
	request := deletionTestLedgerRequest(t, LedgerEntryDeleteCommit, 0)
	state := DeletionStateProvisionalFenced
	switch stage {
	case DeletionWorkResolveDeleteCommit:
		state = DeletionStateLedgerCommitUnknown
	case DeletionWorkConfirmDeleteWitness:
		state = DeletionStateWitnessPending
	case DeletionWorkPromotePermanentFence:
		state = DeletionStateDeleteRequested
	case DeletionWorkPropagatePermanentFence:
		state = DeletionStateFencePropagating
	case DeletionWorkBeginOnlinePurge:
		state = DeletionStateReadFenced
	case DeletionWorkPurgeOnline:
		state = DeletionStateOnlinePurging
	case DeletionWorkResolveRetry:
		state = DeletionStateRetryRequired
	case DeletionWorkResolveNotCommitted, DeletionWorkConfirmNotCommittedWitness:
		state = DeletionStateReleasePending
		request = deletionTestLedgerRequest(t, LedgerEntryAttemptNotCommitted, 3)
	}
	operation := deletionTestOperation(state)
	if state == DeletionStateReleasePending {
		operation.ReleaseEpoch = request.ReleaseEpoch
		operation.LedgerSequence = 0
		operation.LedgerEntryHash = [32]byte{}
	}
	return ClaimedDeletionWork{
		Operation: operation,
		Owner: recordplatform.OwnerLease{
			OwnerID:    "deletion_worker_01",
			Generation: 2,
			ExpiresAt:  time.Date(2026, time.August, 3, 13, 0, 0, 0, time.UTC),
		},
		Stage:      stage,
		Request:    request,
		RetryStage: DeletionWorkPurgeOnline,
	}
}

func deletionTestLedgerRequest(t *testing.T, entryType LedgerEntryType, releaseEpoch uint64) LedgerAppendRequest {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordPermanentDelete,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   deletionTestDigest(71),
		RequestScopeDigest: deletionTestDigest(72),
		PayloadDigest:      deletionTestDigest(73),
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	bytes, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	persisted, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(bytes[:])
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
	}
	request := LedgerAppendRequest{
		EntryType:               entryType,
		DeploymentID:            deletionTestDeploymentID(),
		ProjectID:               recordplatform.ProjectIDDefault,
		OperationID:             "rpo_operation01",
		ActorID:                 "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:                  recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: "rec_01"},
		TokenCommitment:         deletionTestDigest(74),
		RequestFingerprint:      persisted,
		ReasonCode:              DeletionReasonUserConfirmed,
		DeletionContractVersion: 1,
	}
	if entryType == LedgerEntryAttemptNotCommitted {
		request.DeletionContractVersion = 0
		request.ReleaseEpoch = releaseEpoch
	}
	return request
}

func deletionTestLedgerEntry(t *testing.T, request LedgerAppendRequest) DeletionLedgerEntry {
	t.Helper()
	entry := DeletionLedgerEntry{Request: request, Sequence: 11, EntryHash: deletionTestDigest(80)}
	if err := entry.Validate(); err != nil {
		t.Fatalf("DeletionLedgerEntry.Validate() error = %v", err)
	}
	return entry
}

func deletionTestWitnessReceipt(entry DeletionLedgerEntry) DeletionWitnessReceipt {
	return DeletionWitnessReceipt{
		Sequence:    entry.Sequence,
		EntryHash:   entry.EntryHash,
		ProofDigest: deletionTestDigest(81),
	}
}

type deletionWorkerRepositoryStub struct {
	claims                    []*ClaimedDeletionWork
	claimErr                  error
	claimHook                 func()
	propagationReady          bool
	promoteErr                error
	propagationErr            error
	markReadFencedErr         error
	beginPurgeErr             error
	completePurgeErr          error
	calls                     []string
	claimCalls                int
	recordDeleteCalls         int
	recordOutcomeCalls        int
	outcomeUnknownCalls       int
	markDeleteWitnessedCalls  int
	finalizeNotCommittedCalls int
	promoteCalls              int
	retryCalls                int
	completePurgeCalls        int
}

func (repository *deletionWorkerRepositoryStub) ClaimDeletionWork(context.Context, DeletionWorkClaimInput) (*ClaimedDeletionWork, error) {
	repository.claimCalls++
	repository.calls = append(repository.calls, "claim")
	if repository.claimHook != nil {
		repository.claimHook()
	}
	if repository.claimErr != nil {
		return nil, repository.claimErr
	}
	if len(repository.claims) == 0 {
		return nil, nil
	}
	claim := repository.claims[0]
	repository.claims = repository.claims[1:]
	return claim, nil
}

func (repository *deletionWorkerRepositoryStub) MarkDeleteCommitUnknown(context.Context, ClaimedDeletionWork) error {
	repository.calls = append(repository.calls, "delete-unknown")
	return nil
}

func (repository *deletionWorkerRepositoryStub) RecordDeleteEntry(_ context.Context, _ ClaimedDeletionWork, _ DeletionLedgerEntry) error {
	repository.recordDeleteCalls++
	repository.calls = append(repository.calls, "delete-entry")
	return nil
}

func (repository *deletionWorkerRepositoryStub) RecordOutcomeEntry(_ context.Context, _ ClaimedDeletionWork, _ DeletionLedgerEntry) error {
	repository.recordOutcomeCalls++
	repository.calls = append(repository.calls, "outcome-entry")
	return nil
}

func (repository *deletionWorkerRepositoryStub) MarkOutcomeCommitUnknown(_ context.Context, _ ClaimedDeletionWork, _ uint64) error {
	repository.outcomeUnknownCalls++
	repository.calls = append(repository.calls, "outcome-unknown")
	return nil
}

func (repository *deletionWorkerRepositoryStub) MarkDeleteWitnessed(_ context.Context, _ ClaimedDeletionWork, _ DeletionWitnessReceipt) error {
	repository.markDeleteWitnessedCalls++
	repository.calls = append(repository.calls, "delete-witnessed")
	return nil
}

func (repository *deletionWorkerRepositoryStub) FinalizeNotCommitted(_ context.Context, _ ClaimedDeletionWork, _ DeletionWitnessReceipt) error {
	repository.finalizeNotCommittedCalls++
	repository.calls = append(repository.calls, "not-committed")
	return nil
}

func (repository *deletionWorkerRepositoryStub) PromotePermanentFence(context.Context, ClaimedDeletionWork) error {
	repository.promoteCalls++
	repository.calls = append(repository.calls, "permanent-fence")
	return repository.promoteErr
}

func (repository *deletionWorkerRepositoryStub) PermanentFenceApplied(context.Context, ClaimedDeletionWork) (bool, error) {
	repository.calls = append(repository.calls, "propagation-ready")
	return repository.propagationReady, repository.propagationErr
}

func (repository *deletionWorkerRepositoryStub) MarkReadFenced(context.Context, ClaimedDeletionWork) error {
	repository.calls = append(repository.calls, "read-fenced")
	return repository.markReadFencedErr
}

func (repository *deletionWorkerRepositoryStub) BeginOnlinePurge(context.Context, ClaimedDeletionWork) error {
	repository.calls = append(repository.calls, "begin-purge")
	return repository.beginPurgeErr
}

func (repository *deletionWorkerRepositoryStub) CompleteOnlinePurge(_ context.Context, _ ClaimedDeletionWork, _ OnlinePurgeReceipt) error {
	repository.completePurgeCalls++
	repository.calls = append(repository.calls, "online-purged")
	return repository.completePurgeErr
}

func (repository *deletionWorkerRepositoryStub) MarkRetryRequired(_ context.Context, _ ClaimedDeletionWork, _ DeletionWorkStage) error {
	repository.retryCalls++
	repository.calls = append(repository.calls, "retry-required")
	return nil
}

func (repository *deletionWorkerRepositoryStub) ResumeRetry(context.Context, ClaimedDeletionWork, DeletionWorkStage) error {
	repository.calls = append(repository.calls, "resume-retry")
	return nil
}

type deletionLedgerStub struct {
	appendEntry  DeletionLedgerEntry
	appendErr    error
	resolution   LedgerResolution
	resolveErr   error
	lastAppend   LedgerAppendRequest
	appendCalls  int
	resolveCalls int
}

func (ledger *deletionLedgerStub) AppendDeletionEntry(_ context.Context, request LedgerAppendRequest) (DeletionLedgerEntry, error) {
	ledger.appendCalls++
	ledger.lastAppend = request
	return ledger.appendEntry, ledger.appendErr
}

func (ledger *deletionLedgerStub) ResolveDeletionEntry(context.Context, LedgerAppendRequest) (LedgerResolution, error) {
	ledger.resolveCalls++
	return ledger.resolution, ledger.resolveErr
}

type deletionEntryWitnessStub struct {
	receipt DeletionWitnessReceipt
	err     error
	calls   int
}

func (witness *deletionEntryWitnessStub) ConfirmDeletionEntry(context.Context, DeletionLedgerEntry) (DeletionWitnessReceipt, error) {
	witness.calls++
	return witness.receipt, witness.err
}

type deletionOnlinePurgerStub struct {
	receipt OnlinePurgeReceipt
	err     error
	calls   int
}

func (purger *deletionOnlinePurgerStub) PurgeOnline(context.Context, DeletionOperation) (OnlinePurgeReceipt, error) {
	purger.calls++
	return purger.receipt, purger.err
}

func deletionEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
