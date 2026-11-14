//go:build !linux

package deploy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/deletionledger"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/sourceretirement"
)

// A ledger maintenance fence requires Linux cgroup-v2 and filesystem controls.
type RecordLedgerFence struct{}

func WithRecordLedgerMaintenance(context.Context, RecordLedgerMaintenanceRequest, func(*RecordLedgerFence) error) error {
	return ErrRecordLedgerFence
}
func (*RecordLedgerFence) WithHeldLedgerFence(context.Context, func(context.Context) error) error {
	return ErrRecordLedgerFence
}
func (*RecordLedgerFence) RecoveryBinding() (string, uint64, error) {
	return "", 0, ErrRecordLedgerFence
}
func (*RecordLedgerFence) NeverEnrolled() (bool, error) { return false, ErrRecordLedgerFence }
func (*RecordLedgerFence) Trust() (recorddeletion.LedgerVerificationTrust, error) {
	return recorddeletion.LedgerVerificationTrust{}, ErrRecordLedgerFence
}
func (*RecordLedgerFence) VerifyLedgerContinuity(context.Context, recorddeletion.LedgerContinuityObservation) error {
	return ErrRecordLedgerFence
}
func (*RecordLedgerFence) Preflight(context.Context) error { return ErrRecordLedgerFence }
func (*RecordLedgerFence) OpenAdmin(context.Context, string) (*pgxpool.Pool, error) {
	return nil, ErrRecordLedgerFence
}
func (*RecordLedgerFence) ProvisionRecordScopes(context.Context) error { return ErrRecordLedgerFence }
func (*RecordLedgerFence) Readers() (recorddeletion.LedgerSnapshotReader, recorddeletion.LedgerSnapshotReader, error) {
	return nil, nil, ErrRecordLedgerFence
}
func (*RecordLedgerFence) Clients() (*deletionledger.Primary, *deletionledger.Witness, error) {
	return nil, nil, ErrRecordLedgerFence
}
func (*RecordLedgerFence) BeginAppend(context.Context, recorddeletion.LedgerAppendRequest) (bool, error) {
	return false, ErrRecordLedgerFence
}
func (*RecordLedgerFence) Advance(context.Context) error { return ErrRecordLedgerFence }
func (*RecordLedgerFence) WithAbsentOperation(context.Context, recorddeletion.LedgerAppendRequest) (recorddeletion.DeletionLedgerEntry, error) {
	return recorddeletion.DeletionLedgerEntry{}, ErrRecordLedgerFence
}
func PrepareLegacyC80ServiceCoordinator() error { return ErrRecordLedgerFence }
func RunLegacyC80ServiceMaintenance(context.Context, RecordLedgerMaintenanceRequest, LegacyC80ServiceRequest) (LegacyC80ServiceResult, error) {
	return LegacyC80ServiceResult{}, ErrRecordLedgerFence
}
func (*RecordLedgerFence) RunLegacyC80Service(context.Context, LegacyC80ServiceRequest) (LegacyC80ServiceResult, error) {
	return LegacyC80ServiceResult{}, ErrRecordLedgerFence
}
func PrepareLegacyC81MigrationCoordinator() error { return ErrRecordLedgerFence }
func RunLegacyC81MigrationMaintenance(context.Context, RecordLedgerMaintenanceRequest) (LegacyC81MigrationResult, error) {
	return LegacyC81MigrationResult{}, ErrRecordLedgerFence
}
func WithLegacySourceLedgerMaintenance(context.Context, RecordLedgerMaintenanceRequest, func(*RecordLedgerFence) error) error {
	return ErrRecordLedgerFence
}
func (*RecordLedgerFence) PreviewLegacySourceRetirement(context.Context, string, sourceretirement.SourceKey, recordauth.VisibilityScope) (deletionledger.SourceRetirementRequest, [32]byte, error) {
	return deletionledger.SourceRetirementRequest{}, [32]byte{}, ErrRecordLedgerFence
}
func (*RecordLedgerFence) RetireLegacySourceRetirement(context.Context, deletionledger.SourceRetirementRequest, [32]byte) (LegacySourceRetirementResult, error) {
	return LegacySourceRetirementResult{}, ErrRecordLedgerFence
}
func (*RecordLedgerFence) ReconcileLegacySourceRetirement(context.Context, deletionledger.SourceRetirementRequest) (LegacySourceRetirementResult, error) {
	return LegacySourceRetirementResult{}, ErrRecordLedgerFence
}
