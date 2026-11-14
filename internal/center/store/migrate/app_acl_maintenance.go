package migrate

import (
	"bytes"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

// AppACLMaintenanceRoleState is an exact state retained by the trusted offline
// coordinator's protected journal. It permits only LOGIN and database CONNECT
// changes. Password custody and backend quiescence remain coordinator checks.
type AppACLMaintenanceRoleState struct {
	Role    string
	Login   bool
	Connect bool
}

// AppACLMaintenanceSnapshot retains the actual observed catalog, including its
// fenced role facts. LegacyC80 is set only for the registered exact P80 source.
type AppACLMaintenanceSnapshot struct {
	LegacyC80 bool
	Manifest  AppACLManifestRuntimeSnapshotV1
	Catalog   AppACLEffectiveCatalogSnapshotR1
}

// ReadAppACLMaintenanceSnapshot admits only exact C80 or the compiled current
// APP. A nil role inventory requires the complete normal catalog. A nonempty
// inventory is trusted journal input, not a request to repair or relax drift.
// Manifest, migrations, role facts and managed catalog share one read snapshot.
func ReadAppACLMaintenanceSnapshot(ctx context.Context, db *pgxpool.Pool, states []AppACLMaintenanceRoleState) (AppACLMaintenanceSnapshot, error) {
	var result AppACLMaintenanceSnapshot
	if ctx == nil || db == nil {
		return result, fmt.Errorf("APP maintenance database unavailable")
	}
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		return result, err
	}
	transitions, err := compileAppACLCurrentTransitions(source, appACLCurrentTransitionDefinitions)
	if err != nil {
		return result, err
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result, err = readAppACLMaintenanceSnapshotInTransaction(ctx, tx, states, source, transitions)
	if err != nil {
		return AppACLMaintenanceSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLMaintenanceSnapshot{}, err
	}
	return result, nil
}

// ReadAppACLMaintenanceSnapshotInTransaction admits the same exact catalog in
// the caller's read-only REPEATABLE READ inventory snapshot. The caller owns
// the transaction and must discard the observation if its commit fails.
func ReadAppACLMaintenanceSnapshotInTransaction(ctx context.Context, tx pgx.Tx, states []AppACLMaintenanceRoleState) (AppACLMaintenanceSnapshot, error) {
	var result AppACLMaintenanceSnapshot
	if ctx == nil || tx == nil {
		return result, fmt.Errorf("APP maintenance transaction unavailable")
	}
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		return result, err
	}
	transitions, err := compileAppACLCurrentTransitions(source, appACLCurrentTransitionDefinitions)
	if err != nil {
		return result, err
	}
	return readAppACLMaintenanceSnapshotInTransaction(ctx, tx, states, source, transitions)
}

func readAppACLMaintenanceSnapshotInTransaction(ctx context.Context, tx pgx.Tx, states []AppACLMaintenanceRoleState, source appACLCurrentSourceContract, transitions []appACLCurrentTransition) (AppACLMaintenanceSnapshot, error) {
	var result AppACLMaintenanceSnapshot
	var isolation, access string
	if err := tx.QueryRow(ctx, `select pg_catalog.current_setting('transaction_isolation'), pg_catalog.current_setting('transaction_read_only')`).Scan(&isolation, &access); err != nil {
		return result, err
	}
	if isolation != "repeatable read" || access != "on" {
		return result, fmt.Errorf("APP maintenance requires a read-only repeatable-read transaction")
	}
	manifest, err := readAppACLManifestRuntimeSnapshotInTxV1(ctx, tx)
	if err != nil {
		return result, err
	}
	if manifest.Head == nil || ValidateAppACLManifestChainV1(manifest.Manifests, *manifest.Head) != nil {
		return result, fmt.Errorf("APP maintenance manifest chain unavailable")
	}
	latest := manifest.Manifests[len(manifest.Manifests)-1]
	privileges, _, err := parseCanonicalPrivilegeSetBodyAnyVersion(latest.CanonicalPrivilegeSet)
	if err != nil {
		return result, err
	}
	selected := source
	p80 := appACLCurrentTransitionForProfile(transitions, appACLCurrentProfileP80)
	if p80 == nil {
		return result, fmt.Errorf("APP maintenance C80 registry unavailable")
	}
	if bytes.Equal(latest.CanonicalMigrationSet, p80.predecessor.sources.canonicalSet) {
		selected = p80.predecessor
	}
	contract, err := compileAppACLCurrentCatalogContract(selected, manifest.DatabaseName, privileges.RoleBindings, latest.MigratorCatalogRole)
	if err != nil {
		return result, err
	}
	compiled, err := canonicalPrivilegeSetBodyForBindings(contract.RoleBindings, contract.Privileges)
	if err != nil {
		return result, err
	}
	shape, err := classifyAppACLCurrentManifestShape(source, transitions, manifest.AppliedMigrations, manifest.Manifests, manifest.Head, compiled, latest.MigratorCatalogRole, manifest.DatabaseName, privileges.RoleBindings)
	if err != nil {
		return result, err
	}
	if shape.kind == appACLCurrentManifestShapePredecessor {
		if shape.transition == nil || shape.transition.profile != appACLCurrentProfileP80 {
			return result, fmt.Errorf("APP maintenance requires exact C80 or current APP")
		}
		result.LegacyC80 = true
	}
	input, err := newAppACLEffectiveCatalogVerifierInput(contract, latest.MigratorCatalogRole)
	if err != nil {
		return result, err
	}
	catalog, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input)
	if err != nil {
		return result, err
	}
	if err := validateAppACLRuntimeAdmissionSnapshotIdentity(manifest, catalog); err != nil {
		return result, err
	}
	if err := verifyAppACLMaintenanceCatalog(ctx, tx, catalog, input, states); err != nil {
		return result, err
	}
	result.Manifest, result.Catalog = manifest, catalog
	return result, nil
}

func verifyAppACLMaintenanceCatalog(ctx context.Context, tx pgx.Tx, actual AppACLEffectiveCatalogSnapshotR1, input appACLEffectiveCatalogVerifierInput, states []AppACLMaintenanceRoleState) error {
	if len(states) == 0 {
		return verifyAppACLEffectiveCatalogSnapshot(actual, input)
	}
	roles := make(map[string]AppACLEffectiveCatalogRoleStateR1, len(actual.Roles))
	for _, role := range actual.Roles {
		if _, duplicate := roles[role.Name]; duplicate {
			return fmt.Errorf("APP maintenance duplicate role")
		}
		roles[role.Name] = role
	}
	journal := make(map[string]AppACLMaintenanceRoleState, len(states))
	for _, state := range states {
		role, present := roles[state.Role]
		if _, duplicate := journal[state.Role]; duplicate || !present || role.Login != state.Login {
			return fmt.Errorf("APP maintenance journal role drift")
		}
		var connected bool
		if err := tx.QueryRow(ctx, `select pg_catalog.has_database_privilege($1::name,pg_catalog.current_database(),'CONNECT')`, state.Role).Scan(&connected); err != nil || connected != state.Connect {
			return fmt.Errorf("APP maintenance journal CONNECT drift")
		}
		journal[state.Role] = state
	}
	// Only after matching the exact journal state, discharge the LOGIN checks
	// separately and reuse the normal verifier for every other role attribute.
	// Keep the actual snapshot intact for the protected baseline and evidence.
	checked := actual
	checked.Roles = append([]AppACLEffectiveCatalogRoleStateR1(nil), actual.Roles...)
	for i := range checked.Roles {
		if _, ok := journal[checked.Roles[i].Name]; ok {
			checked.Roles[i].Login = true
		}
	}
	bindings := make(map[AppACLSubject]string, len(input.Contract.RoleBindings))
	for _, binding := range input.Contract.RoleBindings {
		bindings[binding.Subject] = binding.CatalogRole
	}
	privileges := make([]AppACLPrivilege, 0, len(input.Contract.Privileges))
	for _, privilege := range input.Contract.Privileges {
		state, fenced := journal[bindings[privilege.Subject]]
		if fenced && !state.Connect && privilege.ObjectClass == AppACLObjectClassDatabase && privilege.Privilege == AppACLPrivilegeConnect {
			continue
		}
		privileges = append(privileges, privilege)
	}
	auxiliary := make([]AppACLCurrentAuxiliaryPrivilege, 0, len(input.Contract.AuxiliaryPrivileges))
	for _, privilege := range input.Contract.AuxiliaryPrivileges {
		state, fenced := journal[privilege.CatalogRole]
		if fenced && !state.Connect && privilege.ObjectClass == AppACLObjectClassDatabase && privilege.Privilege == AppACLPrivilegeConnect {
			continue
		}
		auxiliary = append(auxiliary, privilege)
	}
	input.Contract.Privileges, input.Contract.AuxiliaryPrivileges = privileges, auxiliary
	return verifyAppACLEffectiveCatalogSnapshot(checked, input)
}
