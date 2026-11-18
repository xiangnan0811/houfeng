package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recorddeletion"
)

// corePurgeV1Envelope adds the installed, independently verified ledger
// identity to the same caller-owned lease envelope used by V2 commands.
type corePurgeV1Envelope struct {
	clientComparisonPurgeEnvelope
	LedgerDomain       string `json:"ledger_domain"`
	LedgerSequence     uint64 `json:"ledger_sequence"`
	LedgerEntryHash    string `json:"ledger_entry_hash"`
	WitnessProofDigest string `json:"witness_proof_digest"`
}

func runRestrictedCorePurge(
	ctx context.Context,
	tx pgx.Tx,
	command recorddeletion.CorePurgeCommand,
	verify bool,
) (recorddeletion.AdapterPurgeReceipt, error) {
	operation := command.Operation
	owner := command.OwnerClaim.Owner
	expires := owner.ExpiresAt.UTC()
	if expires.Nanosecond()%1000 != 0 {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	envelope := clientComparisonPurgeEnvelope{
		OperationID: operation.OperationID, RecordID: operation.Object.ObjectID,
		ReservationID: operation.ReservationID, FenceEpoch: uint64(operation.FenceEpoch),
		OwnerID: owner.OwnerID, OwnerGeneration: owner.Generation,
		OwnerExpiresAt: expires.Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(command.SurfaceDigest[:]),
	}
	var payload any = envelope
	query := `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_core_purge_v2($1)`
	if verify {
		query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_core_purge_verify_v2($1)`
	}
	if operation.ContractVersion == recorddeletion.DeletionContractV1 {
		bindingRequest, err := encodeRecordPurgeCommand(struct {
			OperationID     string `json:"operation_id"`
			ProjectID       string `json:"project_id"`
			LedgerSequence  uint64 `json:"ledger_sequence"`
			LedgerEntryHash string `json:"ledger_entry_hash"`
		}{operation.OperationID, operation.Object.ProjectID, operation.LedgerSequence, hex.EncodeToString(operation.LedgerEntryHash[:])})
		if err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		rows, err := tx.Query(ctx, `select ledger_domain, witness_proof_digest
			from public.record_deletion_verified_binding($1)`, bindingRequest)
		if err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("read verified record deletion authority: %w", err)
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("read verified record deletion authority: %w", err)
			}
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		var domain, proof []byte
		if err := rows.Scan(&domain, &proof); err != nil {
			return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("read verified record deletion binding: %w", err)
		}
		if rows.Next() || rows.Err() != nil || len(domain) != sha256.Size || len(proof) != sha256.Size {
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		var domainDigest, proofDigest [sha256.Size]byte
		copy(domainDigest[:], domain)
		copy(proofDigest[:], proof)
		if domainDigest == ([sha256.Size]byte{}) || proofDigest == ([sha256.Size]byte{}) {
			return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
		rows.Close()
		payload = corePurgeV1Envelope{
			clientComparisonPurgeEnvelope: envelope,
			LedgerDomain:                  hex.EncodeToString(domain), LedgerSequence: operation.LedgerSequence,
			LedgerEntryHash:    hex.EncodeToString(operation.LedgerEntryHash[:]),
			WitnessProofDigest: hex.EncodeToString(proof),
		}
		query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_core_purge_v1($1)`
		if verify {
			query = `select removed_surface_digest, receipt_digest, removed_row_count, verified_absent_at from public.record_core_purge_verify_v1($1)`
		}
	}
	body, err := encodeRecordPurgeCommand(payload)
	if err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var surfaceBytes, receiptBytes []byte
	var removedRows int64
	var verifiedAt time.Time
	if err := tx.QueryRow(ctx, query, body).Scan(&surfaceBytes, &receiptBytes, &removedRows, &verifiedAt); err != nil {
		return recorddeletion.AdapterPurgeReceipt{}, fmt.Errorf("run restricted record core purge: %w", err)
	}
	if removedRows < 0 || verifiedAt.IsZero() || !equalRecordPurgeDigest(surfaceBytes, command.SurfaceDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	wantDigest := digestRecordCorePurgeReceipt(operation, command.SurfaceDigest, uint64(removedRows))
	if !equalRecordPurgeDigest(receiptBytes, wantDigest) {
		return recorddeletion.AdapterPurgeReceipt{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return recorddeletion.AdapterPurgeReceipt{
		AdapterName: recorddeletion.AdapterNameRecordCore, OperationID: operation.OperationID,
		SurfaceDigest: command.SurfaceDigest, ReceiptDigest: wantDigest,
		RemovedRowCount: uint64(removedRows), VerifiedAbsentAt: verifiedAt.UTC(),
	}, nil
}
