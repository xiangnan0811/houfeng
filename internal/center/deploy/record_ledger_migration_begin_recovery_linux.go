//go:build linux

package deploy

import (
	"context"
	"reflect"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sys/unix"
	"houfeng/internal/center/store/migrate"
)

func ledgerMigrationBeginCheckpoint(ctx context.Context, f *RecordLedgerFence, begin ledgerMigrationBegin, kind string, value any) error {
	if kind == "begin-settled" {
		return ledgerMigrationSettlementCheckpoint(ctx, f, begin.RecoveryID, begin.Application, begin.PhaseGeneration, kind, value)
	}
	if kind != "begin" || f.maintenanceCheckpointer == nil {
		return ErrRecordLedgerFence
	}
	body, found, err := fenceReadFile(f.control, ledgerMigrationName(kind, begin.PhaseGeneration))
	if err != nil || !found || ledgerReadExact(f.control, ledgerMigrationName(kind, begin.PhaseGeneration), value) != nil {
		return ErrRecordLedgerFence
	}
	applicationDigest, err := ledgerMigrationDigest(begin.Application)
	if err != nil {
		return err
	}
	ack, err := f.maintenanceCheckpointer.Checkpoint(ctx, ledgerMaintenanceCheckpoint{Family: "migration", RecoveryID: begin.RecoveryID, ApplicationDigest: applicationDigest, PhaseGeneration: begin.PhaseGeneration, Kind: kind, RecordSHA256: ledgerPredecessorHash(body)})
	if err != nil || ack.WitnessSequence == 0 {
		return ErrRecordLedgerFence
	}
	if _, ok := ledgerC80DigestBytes(ack.WitnessDigest); !ok {
		return ErrRecordLedgerFence
	}
	return nil
}

// A begin-only cancellation records a new observation, not an invented intent.
func ledgerMigrationResolveBegin(ctx context.Context, f *RecordLedgerFence, begin ledgerMigrationBegin, original ledgerAppInventory, continuity func(context.Context) error) error {
	if ledgerMigrationScope(begin.Cgroup, unix.Getpid()) != nil || continuity == nil || ledgerMigrationBeginCheckpoint(ctx, f, begin, "begin", begin) != nil {
		return ErrRecordLedgerFence
	}
	current, err := ledgerCurrentAppSession(ctx, f.app)
	if err != nil || current.Role != original.Bootstrap {
		return ErrRecordLedgerFence
	}
	effective := original
	effective.Previous = append(append([]string{}, original.Previous...), composeDeletionWorkerRole)
	drain, err := f.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer drain.Rollback(ctx)
	if ledgerDrainAppSessions(ctx, drain, effective) != nil || drain.Commit(ctx) != nil || continuity(ctx) != nil {
		return ErrRecordLedgerFence
	}
	tx, err := f.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	var actual ledgerPredecessorBackend
	if tx.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&actual.PID, &actual.Started, &actual.Role) != nil || actual.PID != current.PID || actual.Role != current.Role || !actual.Started.Equal(current.Started) || ledgerAppIdentity(ctx, tx, begin.Application) != nil || ledgerMigrationWorkerAbsent(ctx, tx) != nil {
		return ErrRecordLedgerFence
	}
	roles, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(original))
	if err != nil || !reflect.DeepEqual(roles, ledgerAppExpectedRoles(original, ledgerAppTarget)) {
		return ErrRecordLedgerFence
	}
	observed, err := migrate.ReadAppACLMaintenanceSnapshotInTransaction(ctx, tx, ledgerAppTrustedStates(original, ledgerAppTarget))
	if err != nil || !observed.LegacyC80 || !reflect.DeepEqual(observed.Manifest, original.Baseline.Manifest) || !reflect.DeepEqual(observed.Catalog, ledgerAppExpectedCatalog(original, ledgerAppTarget)) || ledgerAppPrincipalMatches(ctx, tx, original, observed) != nil {
		return ErrRecordLedgerFence
	}
	managed := append(append([]string{}, effective.Previous...), effective.Admin)
	var clients, prepared int
	if tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where backend_type='client backend' and (datname=pg_catalog.current_database() or usename=any($1::name[])) and pid<>pg_catalog.pg_backend_pid()`, managed).Scan(&clients) != nil || clients != 0 || tx.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, managed).Scan(&prepared) != nil || prepared != 0 || tx.Commit(ctx) != nil {
		return ErrRecordLedgerFence
	}
	beginBody, found, err := fenceReadFile(f.control, ledgerMigrationName("begin", begin.PhaseGeneration))
	if err != nil || !found {
		return ErrRecordLedgerFence
	}
	catalogDigest, err := ledgerMigrationDigest(observed)
	if err != nil {
		return err
	}
	rolesDigest, err := ledgerMigrationDigest(roles)
	if err != nil {
		return err
	}
	var settled ledgerMigrationBeginSettled
	found, err = ledgerMigrationRead(f.control, "begin-settled", begin.PhaseGeneration, &settled)
	if err != nil {
		return err
	}
	if !found {
		self, err := ledgerPredecessorSelf()
		if err != nil {
			return err
		}
		settled = ledgerMigrationBeginSettled{Version: 1, PhaseGeneration: begin.PhaseGeneration, RecoveryID: begin.RecoveryID, Application: begin.Application, BeginDigest: ledgerPredecessorHash(beginBody), Coordinator: self, Bootstrap: actual, CatalogDigest: catalogDigest, RolesDigest: rolesDigest, Outcome: "no_effect_c80"}
		if _, err := ledgerPublishExact(f.control, ledgerMigrationName("begin-settled", begin.PhaseGeneration), settled); err != nil {
			return err
		}
	} else if settled.BeginDigest != ledgerPredecessorHash(beginBody) || settled.CatalogDigest != catalogDigest || settled.RolesDigest != rolesDigest {
		return ErrRecordLedgerFence
	}
	if ledgerMigrationBeginCheckpoint(ctx, f, begin, "begin-settled", settled) != nil {
		return ErrRecordLedgerFence
	}
	if f.phase.Generation+1 == begin.PhaseGeneration && f.phase.Phase == ledgerPhaseQuiescent {
		if err := f.transitionMaintenancePhase(ledgerPhaseRefencing); err != nil {
			return err
		}
	} else if f.phase.Generation == begin.PhaseGeneration && f.phase.Phase == ledgerPhaseRefencing {
		if err := f.checkpointMaintenancePhase(ctx, f.phase); err != nil {
			return err
		}
	} else {
		return ErrRecordLedgerFence
	}
	return f.transitionMaintenancePhase(ledgerPhaseQuiescent)
}
