package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/records"
)

type comparisonRevisionParticipant struct {
	signer evidence.ComparisonIntentSigner
}

func NewComparisonRevisionParticipant(signer evidence.ComparisonIntentSigner) records.RevisionParticipant {
	return comparisonRevisionParticipant{signer: signer}
}

func (comparisonRevisionParticipant) Name() string { return "comparison" }

func (participant comparisonRevisionParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) {
		return fmt.Errorf("%w: comparison transaction", records.ErrInvalidRevisionCommand)
	}
	save := committed.EvidencePreparation.ComparisonSave()
	if save.Empty() {
		return nil
	}
	if participant.signer == nil || save.Token == "" {
		return evidence.ErrComparisonIntentInvalid
	}
	now := time.Now().UTC()
	claims, err := participant.signer.Verify(save.Token, now)
	if err != nil {
		return err
	}
	if claims.Purpose != evidence.ComparisonIntentPurpose {
		return evidence.ErrComparisonIntentInvalid
	}
	if save.Claims.Digest != "" && claims.Digest != save.Claims.Digest {
		return evidence.ErrComparisonIntentStale
	}
	allowed := make(map[string]evidence.ComparisonIntentItem, len(claims.Items))
	for _, item := range claims.Items {
		if item.SnapshotID != "" {
			allowed[item.SnapshotID] = item
		}
	}
	for _, copy := range save.Copies {
		item, ok := allowed[copy.CopiedFromSnapshotID()]
		hash := copy.Snapshot().Hash()
		var encodedHash [64]byte
		hex.Encode(encodedHash[:], hash[:])
		if !ok || copy.RecordID() != save.Result.RecordID() ||
			copy.SnapshotID() != evidence.DeriveComparisonCopySnapshotID(save.Result.RecordID(), item.SnapshotID, claims.Digest) ||
			item.Hash != string(encodedHash[:]) || item.Kind != copy.Snapshot().Envelope().Key.String() {
			return evidence.ErrComparisonIntentStale
		}
		delete(allowed, copy.CopiedFromSnapshotID())
	}
	if len(allowed) != 0 {
		return evidence.ErrComparisonIntentStale
	}
	verified, err := evidence.ComparisonDependenciesFromNativeClaims(save.Result, claims)
	if err != nil {
		return err
	}
	prepared, err := evidence.ComparisonDependenciesFromNativeClaims(save.Result, save.Claims)
	if err != nil || !slices.Equal(verified, prepared) {
		return evidence.ErrComparisonIntentStale
	}
	return nil
}

// The comparison participant verifies these claims before the evidence
// participant calls this helper after inserting the destination result. Both
// participants share the revision transaction; no index escapes a failed save.
func insertNativeComparisonDependencies(ctx context.Context, tx pgx.Tx, save evidence.ComparisonSavePreparation) error {
	dependencies, err := evidence.ComparisonDependenciesFromNativeClaims(save.Result, save.Claims)
	if err != nil {
		return err
	}
	return insertComparisonDependencyIndex(ctx, tx, save.Result.RecordID(), save.Result.SnapshotID(), save.Result.Snapshot(), dependencies, "")
}

// The database command rechecks the persisted owner and every source route,
// and installs the dependency set and completion as one fenced mutation.
// An inherited index names its already-certified source so repair cannot
// replace unrelated provenance with caller-supplied rows.
func insertComparisonDependencyIndex(ctx context.Context, tx pgx.Tx, recordID, snapshotID string, snapshot evidence.CanonicalSnapshot, dependencies []evidence.ComparisonDependency, inheritedFrom string) error {
	type dependencyCommand struct {
		RecordID   string `json:"record_id"`
		RevisionID string `json:"revision_id"`
		SnapshotID string `json:"snapshot_id"`
	}
	command := struct {
		RecordID      string              `json:"record_id"`
		SnapshotID    string              `json:"snapshot_id"`
		PayloadDigest string              `json:"payload_digest"`
		InheritedFrom string              `json:"inherited_from"`
		Dependencies  []dependencyCommand `json:"dependencies"`
	}{RecordID: recordID, SnapshotID: snapshotID, InheritedFrom: inheritedFrom}
	digest := snapshot.Hash()
	command.PayloadDigest = hex.EncodeToString(digest[:])
	command.Dependencies = make([]dependencyCommand, len(dependencies))
	for index, dependency := range dependencies {
		command.Dependencies[index] = dependencyCommand{dependency.SourceRecordID, dependency.SourceRevisionID, dependency.SourceSnapshotID}
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("encode comparison dependency index: %w", err)
	}
	if _, err := tx.Exec(ctx, `select public.record_comparison_index_write($1::bytea, $2::bytea)`, encoded, snapshot.Bytes()); err != nil {
		return fmt.Errorf("write comparison dependency index: %w", err)
	}
	return nil
}
