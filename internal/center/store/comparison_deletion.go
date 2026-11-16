package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

const comparisonDeletionHealthDigestDomainV1 = "houfeng.record-comparison.deletion-health.v1"

var _ recorddeletion.RecordComparisonStore = (*PostgresEvidenceRepository)(nil)

// NewPostgresComparisonDeletionStore keeps canonical reads on the runtime
// repository and restricted purge commands on the deletion worker repository.
func NewPostgresComparisonDeletionStore(runtime, worker *PostgresEvidenceRepository) (recorddeletion.RecordComparisonStore, error) {
	if runtime == nil || runtime.platform == nil || runtime.platform.beginTx == nil || nilAdmissionGate(runtime.platform.gate) ||
		worker == nil || worker.platform == nil || worker.platform.beginTx == nil || nilAdmissionGate(worker.platform.gate) {
		return nil, recorddeletion.ErrInvalidComparisonAdapter
	}
	return &postgresComparisonDeletionStore{PostgresEvidenceRepository: worker, runtime: runtime}, nil
}

type postgresComparisonDeletionStore struct {
	*PostgresEvidenceRepository
	runtime *PostgresEvidenceRepository
}

func (store *postgresComparisonDeletionStore) RecordComparisonHealth(ctx context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	return store.runtime.RecordComparisonHealth(ctx)
}

func (store *postgresComparisonDeletionStore) PreviewComparisonDeletion(ctx context.Context, target recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	return store.runtime.PreviewComparisonDeletion(ctx, target)
}

func (repository *PostgresEvidenceRepository) RecordComparisonHealth(ctx context.Context) (recorddeletion.AdapterHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return recorddeletion.AdapterHealthSnapshot{}, recorddeletion.ErrInvalidComparisonAdapter
	}
	var health recorddeletion.AdapterHealthSnapshot
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		decision, err := readComparisonDeletionDecision(ctx, transaction.tx, nil)
		if err != nil {
			return err
		}
		if !decision.Health.Healthy() {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		health = decision.Health
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterHealthSnapshot{}, err
	}
	return health, nil
}

func (repository *PostgresEvidenceRepository) PurgeRecordComparison(
	ctx context.Context,
	command recorddeletion.ComparisonPurgeCommand,
) (recorddeletion.AdapterPurgeReceipt, error) {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrInvalidComparisonAdapter
	}
	var receipt recorddeletion.AdapterPurgeReceipt
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		var err error
		receipt, err = runClientComparisonPurgeFunction(ctx, transaction.tx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_comparison_purge($1)`, `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_comparison_purge_v1($1)`, recorddeletion.AdapterNameRecordComparison,
			command.Operation, command.OwnerClaim, command.SurfaceDigest)
		return err
	})
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, err
	}
	return receipt, nil
}

func (repository *PostgresEvidenceRepository) VerifyRecordComparisonPurge(
	ctx context.Context,
	command recorddeletion.ComparisonPurgeCommand,
	receipt recorddeletion.AdapterPurgeReceipt,
) error {
	if ctx == nil || repository == nil || repository.platform == nil || command.Validate() != nil {
		return recorddeletion.ErrInvalidComparisonAdapter
	}
	target := recorddeletion.PurgeTarget{Operation: command.Operation, OwnerClaim: command.OwnerClaim}
	descriptor, err := recorddeletion.NewAdapterDescriptor(recorddeletion.AdapterNameRecordComparison, recorddeletion.RecordComparisonSurfaceNames())
	if err != nil || recorddeletion.ValidateAdapterPurgeReceipt(receipt, target, descriptor) != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	return repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		actual, err := runClientComparisonPurgeFunction(ctx, transaction.tx, `
			select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at
			from public.record_comparison_purge_verify($1)`, `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_comparison_purge_verify_v1($1)`, recorddeletion.AdapterNameRecordComparison,
			command.Operation, command.OwnerClaim, command.SurfaceDigest)
		if err != nil {
			return err
		}
		return matchClientComparisonPurgeReceipt(receipt, actual)
	})
}
