package store

import (
	"context"
	"fmt"
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
	if len(claims.Items) > 0 {
		allowed := make(map[string]struct{}, len(claims.Items))
		for _, item := range claims.Items {
			if item.SnapshotID != "" {
				allowed[item.SnapshotID] = struct{}{}
			}
		}
		for _, copy := range save.Copies {
			if _, ok := allowed[copy.CopiedFromSnapshotID()]; !ok {
				return evidence.ErrComparisonIntentStale
			}
		}
	}
	if save.Result.Snapshot().Envelope().Key != evidence.ComparisonResultV1Key() {
		return evidence.ErrComparisonIntentStale
	}
	return nil
}
