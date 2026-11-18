//go:build linux

package deploy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"houfeng/internal/center/store"
)

// Recovery runs before normal APP admission, but must still establish the
// original APP/W1/W2 continuity before changing any role or publishing an
// activation. It reads the retained anchor and real chains without creating
// a new anchor or granting the generic quiescent capability.
func ledgerMigrationCheckContinuity(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, control int, primary, witness *ledgerDatabase) error {
	anchors, err := ledgerReadAnchors(control)
	if err != nil || len(anchors) == 0 || ledgerAppIdentity(ctx, app, request.AppIdentity) != nil || primary.verifyFenced(ctx) != nil || witness.verifyFenced(ctx) != nil {
		return ErrRecordLedgerFence
	}
	f := &RecordLedgerFence{request: request, control: control, primary: primary, witness: witness, anchors: anchors}
	p, w, err := f.bootstrapReaders()
	if err != nil {
		return err
	}
	anchor := anchors[len(anchors)-1]
	first, second, err := f.pair(ctx, p, w, anchor)
	if err != nil || f.validateTail(first, second, anchor) != nil {
		return ErrRecordLedgerFence
	}
	items, err := f.sourceJournalPair(first, second, anchor)
	if err != nil {
		return err
	}
	for _, item := range items {
		anchored, err := f.sourceAnchored(item)
		if err != nil || !anchored || !item.found || item.entry.Sequence > second.Head.Sequence ||
			store.ReadLegacySourceRetirementProjection(ctx, app, item.preview.Request) != nil {
			return ErrRecordLedgerFence
		}
	}
	intents, err := ledgerReadIntents(control)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if _, done, err := ledgerDone(control, intent); err != nil || !done {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func ledgerMigrationCheckOriginalContinuity(ctx context.Context, app *pgxpool.Pool, request RecordLedgerMaintenanceRequest, control, session int, cfg SourceMaintenanceConfig, primary, witness *ledgerDatabase) error {
	generation, err := ledgerRecoveryBinding(ctx, app, witness, cfg, request.Compose.RecoveryID)
	if err != nil || ledgerReadExact(session, "binding.json", ledgerBinding{Version: 1, RecoveryID: request.Compose.RecoveryID, Generation: generation}) != nil {
		return ErrRecordLedgerFence
	}
	return ledgerMigrationCheckContinuity(ctx, app, request, control, primary, witness)
}
