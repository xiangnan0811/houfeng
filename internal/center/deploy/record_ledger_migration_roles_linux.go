//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ledgerMigrationSecret() (string, string, error) {
	var random [32]byte
	var salt [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", ErrRecordLedgerFence
	}
	if _, err := rand.Read(salt[:]); err != nil {
		return "", "", ErrRecordLedgerFence
	}
	secret := base64.RawURLEncoding.EncodeToString(random[:])
	clear(random[:])
	return secret, ledgerSCRAM(secret, salt[:], 4096), nil
}

func ledgerMigrationRuntimeRoles(inventory ledgerAppInventory) (string, string, error) {
	runtime, err := ledgerPredecessorRuntime(inventory)
	if err != nil || len(inventory.Baseline.Manifest.Manifests) == 0 {
		return "", "", ErrRecordLedgerFence
	}
	migrator := inventory.Baseline.Manifest.Manifests[len(inventory.Baseline.Manifest.Manifests)-1].MigratorCatalogRole
	if !ledgerSafeRole(migrator) || migrator == inventory.Bootstrap || migrator == inventory.Admin || migrator == runtime ||
		migrator == composeDeletionWorkerRole || runtime == composeDeletionWorkerRole || inventory.Admin == composeDeletionWorkerRole {
		return "", "", ErrRecordLedgerFence
	}
	if _, ok := ledgerAppRoleByName(inventory.RoleBaseline, migrator); !ok {
		return "", "", ErrRecordLedgerFence
	}
	return runtime, migrator, nil
}

// Apply a complete role snapshot in an already-owned transaction. Both sides
// are protected journal facts, and no transaction survives the physical child.
func ledgerMigrationSetRoles(ctx context.Context, tx pgx.Tx, inventory ledgerAppInventory, from, to ledgerAppRoleSnapshot) error {
	for _, role := range to.Roles {
		if role.Name == inventory.Bootstrap {
			continue
		}
		old, found := ledgerAppRoleByName(from, role.Name)
		if !found || role.Verifier == nil || old.OID != role.OID {
			return ErrRecordLedgerFence
		}
		var ddl string
		if tx.QueryRow(ctx, `select pg_catalog.format('alter role %I password %L',$1::text,$2::text)`, role.Name, *role.Verifier).Scan(&ddl) != nil {
			return ErrRecordLedgerFence
		}
		if _, err := tx.Exec(ctx, ddl); err != nil {
			return ErrRecordLedgerFence
		}
		login := " nologin"
		if role.Login {
			login = " login"
		}
		if _, err := tx.Exec(ctx, "alter role "+pgx.Identifier{role.Name}.Sanitize()+login); err != nil {
			return ErrRecordLedgerFence
		}
		if old.Connect == role.Connect {
			continue
		}
		prefix := "revoke connect on database "
		suffix := " from "
		if role.Connect {
			prefix, suffix = "grant connect on database ", " to "
		}
		statement := prefix + pgx.Identifier{inventory.Application.DatabaseName}.Sanitize() + suffix + pgx.Identifier{role.Name}.Sanitize()
		if role.Connect {
			found := false
			for _, grant := range to.DatabaseACL {
				if grant.GranteeOID == role.OID && grant.Privilege == "CONNECT" {
					if found || grant.GrantorOID != to.DatabaseOwnerOID {
						return ErrRecordLedgerFence
					}
					found = true
					if grant.Grantable {
						statement += " with grant option"
					}
				}
			}
			if !found {
				return ErrRecordLedgerFence
			}
		}
		if _, err := tx.Exec(ctx, statement); err != nil {
			return ErrRecordLedgerFence
		}
	}
	return nil
}

func ledgerMigrationWorkerAbsent(ctx context.Context, q ledgerAppQueryer) error {
	var exists bool
	if q.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&exists) != nil || exists {
		return ErrRecordLedgerFence
	}
	return nil
}

// Migration emergency cleanup also removes management CONNECT.
func ledgerMigrationEmergencyTarget(final ledgerAppRoleSnapshot, admin string) ledgerAppRoleSnapshot {
	closed := final
	closed.Roles = append([]ledgerAppRole(nil), final.Roles...)
	var adminOID uint32
	for i := range closed.Roles {
		if closed.Roles[i].Name == admin {
			closed.Roles[i].Login = false
			closed.Roles[i].Connect = false
			adminOID = closed.Roles[i].OID
		}
	}
	closed.DatabaseACL = make([]ledgerAppDatabaseACL, 0, len(final.DatabaseACL))
	for _, grant := range final.DatabaseACL {
		if grant.GranteeOID != adminOID || grant.Privilege != "CONNECT" {
			closed.DatabaseACL = append(closed.DatabaseACL, grant)
		}
	}
	return closed
}

func ledgerMigrationPrepareRoles(ctx context.Context, app *pgxpool.Pool, control int, intent ledgerMigrationIntent, inventory ledgerAppInventory, address string, checkpointer ledgerMaintenanceCheckpointer) (ledgerMigrationPreparation, string, error) {
	var empty ledgerMigrationPreparation
	intentDigest, err := ledgerMigrationDigest(intent)
	if err != nil {
		return empty, "", err
	}
	normal := inventory.RoleBaseline
	normal.Roles = append([]ledgerAppRole(nil), normal.Roles...)
	seen := map[string]bool{}
	var migratorSecret string
	for i := range normal.Roles {
		role := &normal.Roles[i]
		if role.Name == inventory.Bootstrap {
			continue
		}
		secret, verifier, err := ledgerMigrationSecret()
		if err != nil || seen[secret] {
			return empty, "", ErrRecordLedgerFence
		}
		for _, old := range inventory.RoleBaseline.Roles {
			if ledgerMatchesOriginalVerifier(old, secret) {
				return empty, "", ErrRecordLedgerFence
			}
		}
		for _, target := range inventory.Targets {
			if target.Secret == secret || target.Verifier == verifier {
				return empty, "", ErrRecordLedgerFence
			}
		}
		seen[secret] = true
		role.Verifier = &verifier
		if role.Name == intent.MigratorRole {
			migratorSecret = secret
		}
	}
	workerSecret, workerVerifier, err := ledgerMigrationSecret()
	if err != nil || seen[workerSecret] || migratorSecret == "" {
		return empty, "", ErrRecordLedgerFence
	}
	seen[workerSecret] = true
	finalSecret, finalVerifier, err := ledgerMigrationSecret()
	if err != nil || seen[finalSecret] {
		return empty, "", ErrRecordLedgerFence
	}
	for _, old := range inventory.RoleBaseline.Roles {
		if ledgerMatchesOriginalVerifier(old, workerSecret) || ledgerMatchesOriginalVerifier(old, finalSecret) {
			return empty, "", ErrRecordLedgerFence
		}
	}
	for _, target := range inventory.Targets {
		if target.Secret == workerSecret || target.Secret == finalSecret || target.Verifier == workerVerifier || target.Verifier == finalVerifier {
			return empty, "", ErrRecordLedgerFence
		}
	}
	workerTarget := ledgerAppRoleTarget{Role: composeDeletionWorkerRole, Secret: finalSecret, Verifier: finalVerifier}
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, "", ErrRecordLedgerFence
	}
	defer tx.Rollback(ctx)
	var backend ledgerPredecessorBackend
	if tx.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&backend.PID, &backend.Started, &backend.Role) != nil ||
		backend.PID != intent.Bootstrap.PID || !backend.Started.Equal(intent.Bootstrap.Started) || backend.Role != intent.Bootstrap.Role {
		return empty, "", ErrRecordLedgerFence
	}
	var transactionID string
	if tx.QueryRow(ctx, `select pg_catalog.pg_current_xact_id()::text`).Scan(&transactionID) != nil || transactionID == "" {
		return empty, "", ErrRecordLedgerFence
	}
	current, err := ledgerReadAppRoles(ctx, tx, ledgerAppRoleNames(inventory))
	fenced := ledgerAppExpectedRoles(inventory, ledgerAppTarget)
	if err != nil || !reflect.DeepEqual(current, fenced) || ledgerMigrationWorkerAbsent(ctx, tx) != nil {
		return empty, "", ErrRecordLedgerFence
	}
	if err := ledgerDrainAppSessions(ctx, tx, inventory); err != nil {
		return empty, "", err
	}
	if err := ledgerMigrationSetRoles(ctx, tx, inventory, fenced, normal); err != nil {
		return empty, "", err
	}
	var ddl string
	if tx.QueryRow(ctx, `select pg_catalog.format('create role %I login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L',$1::text,$2::text)`, composeDeletionWorkerRole, workerVerifier).Scan(&ddl) != nil {
		return empty, "", ErrRecordLedgerFence
	}
	if _, err := tx.Exec(ctx, ddl); err != nil {
		return empty, "", ErrRecordLedgerFence
	}
	names := append(ledgerAppRoleNames(inventory), composeDeletionWorkerRole)
	sort.Strings(names)
	observed, err := ledgerReadAppRoles(ctx, tx, names)
	if err != nil || len(observed.Roles) != len(normal.Roles)+1 {
		return empty, "", ErrRecordLedgerFence
	}
	worker, ok := ledgerAppRoleByName(observed, composeDeletionWorkerRole)
	if !ok || worker.OID == 0 || !worker.Login || worker.Inherit || worker.Superuser || worker.CreateRole || worker.CreateDB || worker.Replication || worker.BypassRLS || worker.Connect || worker.Verifier == nil || *worker.Verifier != workerVerifier {
		return empty, "", ErrRecordLedgerFence
	}
	withoutWorker := observed
	withoutWorker.Roles = make([]ledgerAppRole, 0, len(normal.Roles))
	for _, role := range observed.Roles {
		if role.Name != composeDeletionWorkerRole {
			withoutWorker.Roles = append(withoutWorker.Roles, role)
		}
	}
	if !reflect.DeepEqual(withoutWorker, normal) {
		return empty, "", ErrRecordLedgerFence
	}
	cleanupInventory := ledgerMigrationRestrictionInventory(inventory, ledgerMigrationPreparation{WorkerTarget: workerTarget}, observed)
	finalTarget := ledgerAppExpectedRoles(cleanupInventory, ledgerAppTarget)
	preparation := ledgerMigrationPreparation{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest,
		From: fenced, Normal: observed, WorkerTarget: workerTarget, Bootstrap: backend, TransactionID: transactionID,
		FinalTarget: finalTarget, EmergencyTarget: ledgerMigrationEmergencyTarget(finalTarget, inventory.Admin)}
	if ledgerMigrationValidatePreparation(preparation, intent, inventory) != nil {
		return empty, "", ErrRecordLedgerFence
	}
	// The worker's allocated OID and the complete prospective role snapshot are
	// durable before COMMIT. A rollback never authorizes adopting a later OID.
	if _, err := ledgerPublishExact(control, ledgerMigrationName("preparation", intent.PhaseGeneration), preparation); err != nil {
		return empty, "", err
	}
	if ledgerReadExact(control, ledgerMigrationName("preparation", intent.PhaseGeneration), preparation) != nil {
		return empty, "", ErrRecordLedgerFence
	}
	if err := ledgerMigrationCheckpointRecord(ctx, checkpointer, intent, 0, "preparation", preparation); err != nil {
		return empty, "", err
	}
	if tx.Commit(ctx) != nil {
		return empty, "", ErrRecordLedgerFence
	}
	endpoint := url.URL{Scheme: "postgres", User: url.UserPassword(intent.MigratorRole, migratorSecret), Host: address + ":5432", Path: "/" + inventory.Application.DatabaseName, RawQuery: "sslmode=disable"}
	return preparation, endpoint.String(), nil
}

func ledgerMigrationPostRoles(prepared, current ledgerAppRoleSnapshot) error {
	worker, exists := ledgerAppRoleByName(prepared, composeDeletionWorkerRole)
	actual, found := ledgerAppRoleByName(current, composeDeletionWorkerRole)
	if !exists || !found || !actual.Connect {
		return ErrRecordLedgerFence
	}
	worker.Connect = true
	if !reflect.DeepEqual(worker, actual) {
		return ErrRecordLedgerFence
	}
	fixed := current
	fixed.Roles = append([]ledgerAppRole(nil), current.Roles...)
	for i := range fixed.Roles {
		if fixed.Roles[i].Name == composeDeletionWorkerRole {
			fixed.Roles[i].Connect = false
		}
	}
	fixed.DatabaseACL = make([]ledgerAppDatabaseACL, 0, len(current.DatabaseACL))
	grants := 0
	for _, grant := range current.DatabaseACL {
		if grant.GranteeOID == worker.OID && grant.Privilege == "CONNECT" {
			if grant.GrantorOID != prepared.DatabaseOwnerOID || grant.Grantable {
				return ErrRecordLedgerFence
			}
			grants++
			continue
		}
		fixed.DatabaseACL = append(fixed.DatabaseACL, grant)
	}
	if grants != 1 || !reflect.DeepEqual(fixed, prepared) {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerMigrationValidatePreparation(preparation ledgerMigrationPreparation, intent ledgerMigrationIntent, original ledgerAppInventory) error {
	intentDigest, err := ledgerMigrationDigest(intent)
	runtime, migrator, roleErr := ledgerMigrationRuntimeRoles(original)
	if err != nil || roleErr != nil || preparation.Version != 1 || preparation.PhaseGeneration != intent.PhaseGeneration || preparation.IntentDigest != intentDigest ||
		intent.RuntimeRole != runtime || intent.MigratorRole != migrator || intent.AdminRole != original.Admin ||
		preparation.WorkerTarget.Role != composeDeletionWorkerRole || len(preparation.WorkerTarget.Secret) < 32 ||
		!ledgerMatchesSCRAM(preparation.WorkerTarget.Verifier, preparation.WorkerTarget.Secret) {
		return ErrRecordLedgerFence
	}
	xid, xidErr := strconv.ParseUint(preparation.TransactionID, 10, 64)
	if xidErr != nil || xid == 0 || strconv.FormatUint(xid, 10) != preparation.TransactionID ||
		preparation.Bootstrap.PID != intent.Bootstrap.PID || preparation.Bootstrap.Role != intent.Bootstrap.Role || !preparation.Bootstrap.Started.Equal(intent.Bootstrap.Started) ||
		!reflect.DeepEqual(preparation.From, ledgerAppExpectedRoles(original, ledgerAppTarget)) {
		return ErrRecordLedgerFence
	}
	cleanupInventory := ledgerMigrationRestrictionInventory(original, preparation, preparation.Normal)
	if !reflect.DeepEqual(preparation.FinalTarget, ledgerAppExpectedRoles(cleanupInventory, ledgerAppTarget)) ||
		!reflect.DeepEqual(preparation.EmergencyTarget, ledgerMigrationEmergencyTarget(preparation.FinalTarget, original.Admin)) {
		return ErrRecordLedgerFence
	}
	worker, exists := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
	if !exists || worker.OID == 0 || !worker.Login || worker.Inherit || worker.Superuser || worker.CreateRole || worker.CreateDB || worker.Replication || worker.BypassRLS ||
		worker.Connect || worker.ConnLimit != -1 || worker.ValidUntil != nil || worker.Verifier == nil {
		return ErrRecordLedgerFence
	}
	expected := original.RoleBaseline
	expected.Roles = append([]ledgerAppRole(nil), expected.Roles...)
	actual := preparation.Normal
	actual.Roles = make([]ledgerAppRole, 0, len(preparation.Normal.Roles)-1)
	seen := map[string]bool{}
	for _, role := range preparation.Normal.Roles {
		if role.Name == composeDeletionWorkerRole {
			continue
		}
		actual.Roles = append(actual.Roles, role)
	}
	if len(actual.Roles) != len(expected.Roles) {
		return ErrRecordLedgerFence
	}
	for i := range expected.Roles {
		role := actual.Roles[i]
		if role.Name != expected.Roles[i].Name {
			return ErrRecordLedgerFence
		}
		if role.Name != original.Bootstrap {
			if role.Verifier == nil || !strings.HasPrefix(*role.Verifier, "SCRAM-SHA-256$") || seen[*role.Verifier] || reflect.DeepEqual(role.Verifier, expected.Roles[i].Verifier) ||
				ledgerMatchesSCRAM(*role.Verifier, preparation.WorkerTarget.Secret) {
				return ErrRecordLedgerFence
			}
			for _, target := range original.Targets {
				if target.Verifier == *role.Verifier {
					return ErrRecordLedgerFence
				}
			}
			seen[*role.Verifier] = true
			expected.Roles[i].Verifier = role.Verifier
		}
	}
	if !strings.HasPrefix(*worker.Verifier, "SCRAM-SHA-256$") || seen[*worker.Verifier] || ledgerMatchesSCRAM(*worker.Verifier, preparation.WorkerTarget.Secret) ||
		!reflect.DeepEqual(actual, expected) {
		return ErrRecordLedgerFence
	}
	for _, target := range original.Targets {
		if target.Secret == preparation.WorkerTarget.Secret || target.Verifier == preparation.WorkerTarget.Verifier || target.Verifier == *worker.Verifier {
			return ErrRecordLedgerFence
		}
	}
	for _, role := range original.RoleBaseline.Roles {
		if ledgerMatchesOriginalVerifier(role, preparation.WorkerTarget.Secret) || reflect.DeepEqual(role.Verifier, worker.Verifier) {
			return ErrRecordLedgerFence
		}
	}
	return nil
}
