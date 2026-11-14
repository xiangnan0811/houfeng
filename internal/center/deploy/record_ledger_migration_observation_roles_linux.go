//go:build linux

package deploy

import (
	"context"
	"reflect"
	"sort"

	"github.com/jackc/pgx/v5"
	"houfeng/internal/center/store/migrate"
)

// The successor migration adds exactly this worker CONNECT grant.
func ledgerMigrationConvergedRoles(preparation ledgerMigrationPreparation) (ledgerAppRoleSnapshot, error) {
	normal := preparation.Normal
	normal.Roles = append([]ledgerAppRole(nil), normal.Roles...)
	normal.DatabaseACL = append([]ledgerAppDatabaseACL(nil), normal.DatabaseACL...)
	worker, ok := ledgerAppRoleByName(normal, composeDeletionWorkerRole)
	if !ok || worker.Connect {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	normal.DatabaseACL = append(normal.DatabaseACL, ledgerAppDatabaseACL{GrantorOID: normal.DatabaseOwnerOID, GranteeOID: worker.OID, Privilege: "CONNECT"})
	sort.Slice(normal.DatabaseACL, func(i, j int) bool {
		a, b := normal.DatabaseACL[i], normal.DatabaseACL[j]
		if a.GrantorOID != b.GrantorOID {
			return a.GrantorOID < b.GrantorOID
		}
		if a.GranteeOID != b.GranteeOID {
			return a.GranteeOID < b.GranteeOID
		}
		if a.Privilege != b.Privilege {
			return a.Privilege < b.Privilege
		}
		return !a.Grantable && b.Grantable
	})
	for i := range normal.Roles {
		if normal.Roles[i].Name == composeDeletionWorkerRole {
			normal.Roles[i].Connect = true
		}
	}
	if ledgerMigrationPostRoles(preparation.Normal, normal) != nil {
		return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
	}
	return normal, nil
}

func ledgerMigrationFreshRoles(preparation ledgerMigrationPreparation, original ledgerAppInventory, previous []ledgerMigrationObservationIntent) (ledgerAppRoleSnapshot, error) {
	normal, err := ledgerMigrationConvergedRoles(preparation)
	if err != nil {
		return ledgerAppRoleSnapshot{}, err
	}
	seenVerifiers := []string{}
	for i := range normal.Roles {
		role := &normal.Roles[i]
		if role.Name == original.Bootstrap {
			continue
		}
		secret, verifier, err := ledgerMigrationSecret()
		if err != nil {
			return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
		}
		for _, old := range seenVerifiers {
			if verifier == old || ledgerMatchesSCRAM(old, secret) {
				return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
			}
		}
		for _, snapshot := range append([]ledgerAppRoleSnapshot{original.RoleBaseline, preparation.Normal, preparation.FinalTarget, preparation.EmergencyTarget}, observationSnapshots(previous)...) {
			for _, old := range snapshot.Roles {
				if old.Verifier != nil && (verifier == *old.Verifier || ledgerMatchesOriginalVerifier(old, secret)) {
					return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
				}
			}
		}
		for _, target := range append(append([]ledgerAppRoleTarget{}, original.Targets...), preparation.WorkerTarget) {
			if secret == target.Secret || verifier == target.Verifier {
				return ledgerAppRoleSnapshot{}, ErrRecordLedgerFence
			}
		}
		seenVerifiers = append(seenVerifiers, verifier)
		secret = ""
		role.Verifier = &verifier
	}
	return normal, nil
}

func observationSnapshots(previous []ledgerMigrationObservationIntent) []ledgerAppRoleSnapshot {
	snapshots := make([]ledgerAppRoleSnapshot, 0, len(previous))
	for _, item := range previous {
		snapshots = append(snapshots, item.To)
	}
	return snapshots
}

func ledgerMigrationPublishObservation(ctx context.Context, f *RecordLedgerFence, intent ledgerMigrationIntent, attempt uint64, kind string, value any) error {
	name := ledgerMigrationObservationName(kind, intent.PhaseGeneration, attempt)
	if _, err := ledgerPublishExact(f.control, name, value); err != nil {
		return err
	}
	if ledgerReadExact(f.control, name, value) != nil {
		return ErrRecordLedgerFence
	}
	return ledgerMigrationCheckpointRecord(ctx, f.maintenanceCheckpointer, intent, attempt, "observe-"+kind, value)
}

// The caller has already fenced all sessions and verified the C81 catalog in a
// single snapshot. The transaction rechecks the exact role state and head.
func ledgerMigrationObservationGrant(ctx context.Context, f *RecordLedgerFence, original ledgerAppInventory, preparation ledgerMigrationPreparation, observation ledgerMigrationObservationIntent, catalog migrate.AppACLMaintenanceSnapshot) error {
	tx, err := f.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	if ledgerAppIdentity(ctx, tx, observation.Application) != nil {
		return ErrRecordLedgerFence
	}
	inventory := ledgerMigrationRestrictionInventory(original, preparation, observation.To)
	actual, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, observation.From) {
		return ErrRecordLedgerFence
	}
	var current ledgerPredecessorBackend
	if tx.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&current.PID, &current.Started, &current.Role) != nil || current.PID != observation.Bootstrap.PID || !current.Started.Equal(observation.Bootstrap.Started) || current.Role != observation.Bootstrap.Role {
		return ErrRecordLedgerFence
	}
	var head []byte
	if tx.QueryRow(ctx, `select manifest_digest from public.app_acl_manifest_head where singleton`).Scan(&head) != nil || catalog.Manifest.Head == nil || !reflect.DeepEqual(head, catalog.Manifest.Head.ManifestDigest[:]) {
		return ErrRecordLedgerFence
	}
	if ledgerDrainAppSessions(ctx, tx, inventory) != nil || ledgerMigrationSetRoles(ctx, tx, inventory, observation.From, observation.To) != nil {
		return ErrRecordLedgerFence
	}
	actual, err = ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(actual, observation.To) || tx.Commit(ctx) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}
