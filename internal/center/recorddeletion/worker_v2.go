package recorddeletion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// V2DeletionWorkerRepository owns V2 claims and completion independently of the V1 ledger.
type V2DeletionWorkerRepository interface {
	ClaimV2DeletionWork(context.Context, DeletionWorkClaimInput) (*ClaimedDeletionWork, error)
	CompleteV2OnlinePurge(context.Context, ClaimedDeletionWork, OnlinePurgeReceipt) error
}

type V2DeletionWorker struct {
	repository V2DeletionWorkerRepository
	purger     DeletionOnlinePurger
	options    DeletionWorkerOptions
}

func NewV2DeletionWorker(repository V2DeletionWorkerRepository, purger DeletionOnlinePurger, options DeletionWorkerOptions) *V2DeletionWorker {
	if options.PollInterval == 0 {
		options.PollInterval = time.Second
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &V2DeletionWorker{repository: repository, purger: purger, options: options}
}

func (worker *V2DeletionWorker) validate() error {
	if worker == nil || nilDeletionServiceDependency(worker.repository) || nilDeletionServiceDependency(worker.purger) ||
		(DeletionWorkClaimInput{OwnerID: worker.options.OwnerID, OwnerLeaseDuration: worker.options.OwnerLeaseDuration}).Validate() != nil || worker.options.PollInterval <= 0 {
		return ErrInvalidDeletionWorker
	}
	return nil
}

func (worker *V2DeletionWorker) Run(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidDeletionWorker
	}
	if err := worker.validate(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	ticker := time.NewTicker(worker.options.PollInterval)
	defer ticker.Stop()
	for {
		if err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			worker.options.Logger.Error("record deletion pass failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *V2DeletionWorker) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidDeletionWorker
	}
	if err := worker.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	claim, err := worker.repository.ClaimV2DeletionWork(ctx, DeletionWorkClaimInput{OwnerID: worker.options.OwnerID, OwnerLeaseDuration: worker.options.OwnerLeaseDuration})
	if err != nil {
		return fmt.Errorf("claim record deletion work: %w", err)
	}
	if claim == nil {
		return nil
	}
	if claim.Operation.ContractVersion != DeletionContractV2 || claim.Validate() != nil {
		return ErrInvalidDeletionWorkerResult
	}
	receipt, err := worker.purger.PurgeOnline(ctx, PurgeTarget{Operation: claim.Operation, OwnerClaim: OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}})
	if err != nil {
		return err
	}
	if err := receipt.validate(claim.Operation); err != nil {
		return err
	}
	return worker.repository.CompleteV2OnlinePurge(ctx, *claim, receipt)
}
