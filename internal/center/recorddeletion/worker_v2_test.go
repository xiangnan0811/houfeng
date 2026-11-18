package recorddeletion

import (
	"context"
	"errors"
	"testing"
)

type v2WorkerTestRepository struct {
	claim     *ClaimedDeletionWork
	completed int
}

func (repository *v2WorkerTestRepository) ClaimV2DeletionWork(context.Context, DeletionWorkClaimInput) (*ClaimedDeletionWork, error) {
	return repository.claim, nil
}
func (repository *v2WorkerTestRepository) CompleteV2OnlinePurge(context.Context, ClaimedDeletionWork, OnlinePurgeReceipt) error {
	repository.completed++
	return nil
}

type v2WorkerFailingPurger struct{ err error }

func (purger v2WorkerFailingPurger) PurgeOnline(context.Context, PurgeTarget) (OnlinePurgeReceipt, error) {
	return OnlinePurgeReceipt{}, purger.err
}

func v2WorkerTestClaim(t *testing.T) ClaimedDeletionWork {
	t.Helper()
	claim := deletionTestWorkClaim(t, DeletionWorkPurgeOnline)
	claim.Operation.ContractVersion = DeletionContractV2
	claim.Operation.LedgerSequence = 0
	claim.Operation.LedgerEntryHash = [32]byte{}
	claim.Request = LedgerAppendRequest{}
	claim.RetryStage = ""
	return claim
}

func TestV2DeletionWorkerRejectsLegacyOrLedgerClaims(t *testing.T) {
	claim := v2WorkerTestClaim(t)
	if err := claim.Validate(); err != nil {
		t.Fatalf("valid V2 claim: %v", err)
	}
	for name, mutate := range map[string]func(*ClaimedDeletionWork){
		"ledger request": func(c *ClaimedDeletionWork) { c.Request.OperationID = c.Operation.OperationID },
		"retry stage":    func(c *ClaimedDeletionWork) { c.RetryStage = DeletionWorkPurgeOnline },
		"wrong stage":    func(c *ClaimedDeletionWork) { c.Stage = DeletionWorkBeginOnlinePurge },
		"entry":          func(c *ClaimedDeletionWork) { c.Entry = &DeletionLedgerEntry{} },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := claim
			mutate(&invalid)
			if invalid.Validate() == nil {
				t.Fatal("invalid V2 claim accepted")
			}
			repository := &v2WorkerTestRepository{claim: &invalid}
			purger := &deletionOnlinePurgerStub{}
			if err := NewV2DeletionWorker(repository, purger, deletionTestWorkerOptions()).RunOnce(context.Background()); err == nil || purger.calls != 0 || repository.completed != 0 {
				t.Fatalf("invalid claim reached purge or completion: %v", err)
			}
		})
	}
	legacyRepo := &deletionWorkerRepositoryStub{claims: []*ClaimedDeletionWork{&claim}}
	legacyPurger := &deletionOnlinePurgerStub{}
	if err := deletionTestWorker(legacyRepo, &deletionLedgerStub{}, &deletionEntryWitnessStub{}, legacyPurger).RunOnce(context.Background()); err == nil || legacyPurger.calls != 0 || legacyRepo.retryCalls != 0 {
		t.Fatalf("V1 worker accepted V2 claim: %v", err)
	}
}

func TestV2DeletionWorkerPreservesPurgeFailure(t *testing.T) {
	claim := v2WorkerTestClaim(t)
	repository := &v2WorkerTestRepository{claim: &claim}
	lost := errors.New("owner lease lost")
	err := NewV2DeletionWorker(repository, v2WorkerFailingPurger{err: lost}, deletionTestWorkerOptions()).RunOnce(context.Background())
	if !errors.Is(err, lost) || repository.completed != 0 {
		t.Fatalf("purge failure = %v, completions = %d", err, repository.completed)
	}
}
