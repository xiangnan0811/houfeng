package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"houfeng/db/migrations"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationRecordDeletionSuccessorAuthority(t *testing.T) {
	t.Run("worker_unmanaged_ownership", func(t *testing.T) {
		ctx := context.Background()
		fixture := newAppACLConvergencePostgresFixture(t, ctx)
		db := fixture.openDirectRolePool(t, ctx, fixture.migrator)
		if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
			t.Fatal(err)
		}
		workerDB := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker)
		runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
		if _, err := fixture.db.Exec(ctx, `create schema worker_extra; create type worker_extra.probe as enum ('value'); alter type worker_extra.probe owner to `+pgx.Identifier{fixture.deletionWorker}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		if err := AdmitAppACLCurrentDeletionWorker(ctx, workerDB); err == nil {
			t.Error("worker admission accepted ownership of an unmanaged type")
		}
		if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil {
			t.Error("runtime admission accepted worker-owned unmanaged type")
		}
		if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtime, fixture.admin, fixture.deletionWorker); err == nil {
			t.Error("convergence accepted worker-owned unmanaged type")
		}
	})
	t.Run("c80_upgrade_and_rollback", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP80Migration)
		if !bytes.Equal(profile.source.sources.canonicalSet, appACLCurrentC80MigrationGolden) || !bytes.Equal(profile.privilegeGolden, appACLCurrentC80PrivilegeGolden) {
			t.Fatal("C80 fixture must match the independently captured source and privilege bodies")
		}
		fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
		db := fixture.openRolePool(t, ctx, fixture.migratorRole)
		predecessor, _, oldInput := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, db, profile)
		before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, oldInput)
		runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
		assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
		cutpoint := errors.New("C81 DCL completed before publication")
		dependencies := defaultAppACLCurrentConvergenceDependencies()
		applyDCL := dependencies.applyDCL
		dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract appACLEffectiveCatalogContract) error {
			if err := applyDCL(ctx, tx, contract); err != nil {
				return err
			}
			return cutpoint
		}
		_, err := convergeAppACLCurrentWithDependencies(ctx, db.BeginTx, fixture.runtimeRole, fixture.adminRole, fixture.deletionWorkerRole, migrations.FS, appACLCurrentMigrationFragments, dependencies)
		if !errors.Is(err, cutpoint) {
			t.Fatalf("C80 upgrade cutpoint = %v", err)
		}
		if after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, oldInput); !reflect.DeepEqual(after, before) {
			t.Fatal("failed C81 publication changed the C80 ledger, catalog or manifest")
		}
		var gateExists bool
		if err := db.QueryRow(ctx, `select to_regclass('public.record_deletion_gates') is not null`).Scan(&gateExists); err != nil || gateExists {
			t.Fatalf("rolled-back gate schema exists = %t, error = %v", gateExists, err)
		}
		successor, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole, fixture.deletionWorkerRole)
		if err != nil {
			t.Fatal(err)
		}
		if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
			t.Fatal("C81 lost C80 manifest linkage")
		}
		_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
		after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, currentInput)
		assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, recordDeletionSuccessorMigration)
		assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
		if !reflect.DeepEqual(appACLCurrentCatalogBeforeDeletion(t, before.Catalog, after.Catalog), before.Catalog) {
			t.Fatal("C81 changed catalog outside its exact delta")
		}
		if _, err := ParseCanonicalPrivilegeSetBodyV1(predecessor.CanonicalPrivilegeSet); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseCanonicalPrivilegeSetBodyV2(successor.CanonicalPrivilegeSet); err != nil {
			t.Fatal(err)
		}
		if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
			t.Fatal(err)
		}
		workerDB := fixture.openRolePool(t, ctx, fixture.deletionWorkerRole)
		if err := AdmitAppACLCurrentDeletionWorker(ctx, workerDB); err != nil {
			t.Fatal(err)
		}
		repeated, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole, fixture.deletionWorkerRole)
		if err != nil || repeated.ManifestDigest != successor.ManifestDigest {
			t.Fatalf("C81 exact repeat = %v", err)
		}
		if repeatedState := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, currentInput); !reflect.DeepEqual(repeatedState, after) {
			t.Fatal("C81 exact repeat changed durable state")
		}
	})
	t.Run("mutation_row_locks", testPostgresRecordMutationRowLocks)
	t.Run("comparison_trigger_admission", testPostgresComparisonTriggerAdmission)
	t.Run("external_delivery_facts", testPostgresExternalDeliveryFacts)
}

func testPostgresComparisonTriggerAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker)
	assertComparisonInputTriggerCatalog(t, ctx, migrator)
	assertV2DeletionTriggerCatalog(t, ctx, migrator)
	var publicExecute bool
	if err := migrator.QueryRow(ctx, `select exists (
		select 1 from pg_catalog.pg_proc p
		cross join lateral pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) a
		where p.oid='record_platform_internal.record_draft_launch_guard()'::regprocedure
		  and a.grantee=0 and a.privilege_type='EXECUTE'
	)`).Scan(&publicExecute); err != nil || publicExecute {
		t.Fatalf("draft launch guard PUBLIC EXECUTE = %t, error=%v", publicExecute, err)
	}
	for _, role := range []string{fixture.runtime, fixture.admin, fixture.deletionWorker} {
		var execute bool
		if err := migrator.QueryRow(ctx, `select pg_catalog.has_function_privilege(
			$1::name, 'record_platform_internal.record_draft_launch_guard()'::regprocedure, 'EXECUTE')`, role).Scan(&execute); err != nil || execute {
			t.Fatalf("role %s draft launch guard EXECUTE = %t, error=%v", role, execute, err)
		}
	}
	const create = `create trigger comparison_snapshot_writer_guard before insert or delete on public.evidence_snapshots
		for each row execute function record_platform_internal.comparison_evidence_row_guard()`
	for _, tc := range []struct{ name, damage, restore string }{
		{"missing", `drop trigger comparison_snapshot_writer_guard on public.evidence_snapshots`, create},
		{"disabled", `alter table public.evidence_snapshots disable trigger comparison_snapshot_writer_guard`,
			`alter table public.evidence_snapshots enable trigger comparison_snapshot_writer_guard`},
		{"wrong events", `drop trigger comparison_snapshot_writer_guard on public.evidence_snapshots;
			create trigger comparison_snapshot_writer_guard before insert on public.evidence_snapshots
			for each row execute function record_platform_internal.comparison_evidence_row_guard()`,
			`drop trigger comparison_snapshot_writer_guard on public.evidence_snapshots; ` + create},
		{"wrong function", `drop trigger comparison_snapshot_writer_guard on public.evidence_snapshots;
			create trigger comparison_snapshot_writer_guard before insert or delete on public.evidence_snapshots
			for each row execute function record_platform_internal.comparison_root_epoch_guard()`,
			`drop trigger comparison_snapshot_writer_guard on public.evidence_snapshots; ` + create},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := migrator.Exec(ctx, tc.damage); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := migrator.Exec(context.Background(), tc.restore); err != nil {
					t.Errorf("restore comparison trigger: %v", err)
				}
			})
			for _, admission := range []struct {
				name  string
				pool  *pgxpool.Pool
				admit func(context.Context, *pgxpool.Pool) error
			}{{"runtime", runtime, AdmitAppACLCurrentRuntime}, {"worker", worker, AdmitAppACLCurrentDeletionWorker}} {
				if err := admission.admit(ctx, admission.pool); err == nil || !strings.Contains(err.Error(), "comparison input trigger") {
					t.Fatalf("%s admission accepted %s trigger drift: %v", admission.name, tc.name, err)
				}
			}
			if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err == nil ||
				!strings.Contains(err.Error(), "comparison input trigger") {
				t.Fatalf("convergence accepted %s trigger drift: %v", tc.name, err)
			}
		})
	}

	for _, guard := range []struct {
		name, table, trigger, events, wrongEvents string
	}{
		{"draft", "record_drafts", "record_draft_launch_guard", "insert or update or delete", "insert or delete"},
		{"draft_checkpoint", "record_draft_checkpoints", "record_draft_checkpoint_launch_guard", "insert or delete", "insert or update or delete"},
	} {
		drop := `drop trigger ` + guard.trigger + ` on public.` + guard.table
		before := `create trigger ` + guard.trigger + ` before `
		onTable := ` on public.` + guard.table + ` for each row `
		function := `execute function record_platform_internal.record_draft_launch_guard()`
		create := before + guard.events + onTable + function
		alter := `alter table public.` + guard.table
		for _, tc := range []struct{ name, damage, restore string }{
			{"missing", drop, create},
			{"disabled", alter + ` disable trigger ` + guard.trigger, alter + ` enable trigger ` + guard.trigger},
			{"replica_only", alter + ` enable replica trigger ` + guard.trigger, alter + ` enable trigger ` + guard.trigger},
			{"wrong_events", drop + `; ` + before + guard.wrongEvents + onTable + function, drop + `; ` + create},
			{"wrong_timing", drop + `; create trigger ` + guard.trigger + ` after ` + guard.events + onTable + function, drop + `; ` + create},
			{"wrong_level", drop + `; ` + before + guard.events + ` on public.` + guard.table + ` for each statement ` + function, drop + `; ` + create},
			{"wrong_function", drop + `; ` + before + guard.events + onTable + `execute function record_platform_internal.comparison_root_epoch_guard()`, drop + `; ` + create},
			{"qualified", drop + `; ` + before + guard.events + onTable + `when (false) ` + function, drop + `; ` + create},
			{"arguments", drop + `; ` + before + guard.events + onTable + `execute function record_platform_internal.record_draft_launch_guard('unexpected')`, drop + `; ` + create},
		} {
			t.Run(guard.name+"_"+tc.name, func(t *testing.T) {
				if _, err := migrator.Exec(ctx, tc.damage); err != nil {
					t.Fatalf("damage %s trigger: %v", guard.name, err)
				}
				t.Cleanup(func() {
					if _, err := migrator.Exec(context.Background(), tc.restore); err != nil {
						t.Errorf("restore %s trigger: %v", guard.name, err)
					}
				})
				for _, admission := range []struct {
					name  string
					pool  *pgxpool.Pool
					admit func(context.Context, *pgxpool.Pool) error
				}{{"runtime", runtime, AdmitAppACLCurrentRuntime}, {"worker", worker, AdmitAppACLCurrentDeletionWorker}} {
					if err := admission.admit(ctx, admission.pool); err == nil || !strings.Contains(err.Error(), guard.table+"."+guard.trigger) {
						t.Fatalf("%s admission accepted %s trigger drift: %v", admission.name, tc.name, err)
					}
				}
				if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err == nil ||
					!strings.Contains(err.Error(), guard.table+"."+guard.trigger) {
					t.Fatalf("convergence accepted %s trigger drift: %v", tc.name, err)
				}
			})
		}
	}

	// Independently inspect and damage the private checkpoint guard. The
	// expected event bits are statement-level BEFORE UPDATE/DELETE/TRUNCATE.
	var bits int
	var enabled, tableOwner, functionSchema, functionName, functionOwner string
	var definer bool
	if err := migrator.QueryRow(ctx, `select t.tgtype::int,t.tgenabled::text,pg_catalog.pg_get_userbyid(c.relowner),
		fn.nspname,p.proname,pg_catalog.pg_get_userbyid(p.proowner),p.prosecdef
		from pg_catalog.pg_trigger t join pg_catalog.pg_class c on c.oid=t.tgrelid
		join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		join pg_catalog.pg_proc p on p.oid=t.tgfoid join pg_catalog.pg_namespace fn on fn.oid=p.pronamespace
		where n.nspname='record_platform_internal' and c.relname='record_birth_checkpoints'
		  and t.tgname='record_birth_checkpoints_immutable'`).Scan(
		&bits, &enabled, &tableOwner, &functionSchema, &functionName, &functionOwner, &definer); err != nil {
		t.Fatalf("read independent checkpoint trigger catalog: %v", err)
	}
	if bits != 58 || enabled != "O" || tableOwner != fixture.migrator ||
		functionSchema != "record_platform_internal" || functionName != "reject_source_retirement_mutation" ||
		functionOwner != fixture.migrator || !definer {
		t.Fatalf("unexpected checkpoint trigger: bits=%d enabled=%s table owner=%s function=%s.%s owner=%s definer=%t",
			bits, enabled, tableOwner, functionSchema, functionName, functionOwner, definer)
	}
	const checkpointCreate = `create trigger record_birth_checkpoints_immutable before update or delete or truncate
		on record_platform_internal.record_birth_checkpoints for each statement
		execute function record_platform_internal.reject_source_retirement_mutation()`
	const checkpointDrop = `drop trigger record_birth_checkpoints_immutable on record_platform_internal.record_birth_checkpoints`
	for _, tc := range []struct{ name, damage, restore string }{
		{"missing", checkpointDrop, checkpointCreate},
		{"disabled", `alter table record_platform_internal.record_birth_checkpoints disable trigger record_birth_checkpoints_immutable`,
			`alter table record_platform_internal.record_birth_checkpoints enable trigger record_birth_checkpoints_immutable`},
		{"replica_only", `alter table record_platform_internal.record_birth_checkpoints enable replica trigger record_birth_checkpoints_immutable`,
			`alter table record_platform_internal.record_birth_checkpoints enable trigger record_birth_checkpoints_immutable`},
		{"missing_truncate", checkpointDrop + `; create trigger record_birth_checkpoints_immutable before update or delete
			on record_platform_internal.record_birth_checkpoints for each statement
			execute function record_platform_internal.reject_source_retirement_mutation()`, checkpointDrop + `; ` + checkpointCreate},
		{"wrong_timing", checkpointDrop + `; create trigger record_birth_checkpoints_immutable after update or delete or truncate
			on record_platform_internal.record_birth_checkpoints for each statement
			execute function record_platform_internal.reject_source_retirement_mutation()`, checkpointDrop + `; ` + checkpointCreate},
		{"wrong_function", checkpointDrop + `; create trigger record_birth_checkpoints_immutable before update or delete or truncate
			on record_platform_internal.record_birth_checkpoints for each statement
			execute function record_platform_internal.record_birth_root_tombstone()`, checkpointDrop + `; ` + checkpointCreate},
	} {
		t.Run("checkpoint_"+tc.name, func(t *testing.T) {
			if _, err := migrator.Exec(ctx, tc.damage); err != nil {
				t.Fatalf("damage checkpoint trigger: %v", err)
			}
			t.Cleanup(func() {
				if _, err := migrator.Exec(context.Background(), tc.restore); err != nil {
					t.Errorf("restore checkpoint trigger: %v", err)
				}
			})
			for _, admission := range []struct {
				name  string
				pool  *pgxpool.Pool
				admit func(context.Context, *pgxpool.Pool) error
			}{{"runtime", runtime, AdmitAppACLCurrentRuntime}, {"worker", worker, AdmitAppACLCurrentDeletionWorker}} {
				if err := admission.admit(ctx, admission.pool); err == nil || !strings.Contains(err.Error(), "birth checkpoint trigger") {
					t.Fatalf("%s admission accepted %s checkpoint trigger: %v", admission.name, tc.name, err)
				}
			}
			if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err == nil ||
				!strings.Contains(err.Error(), "birth checkpoint trigger") {
				t.Fatalf("convergence accepted %s checkpoint trigger: %v", tc.name, err)
			}
		})
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("admission after exact trigger restoration: %v", err)
	}
	if err := AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatalf("worker admission after exact trigger restoration: %v", err)
	}
}

// Strip only the independently enumerated C81 additions after checking every
// owner, function, role and grant. The older migration assertions then continue
// checking their original deltas; this never filters arbitrary current objects.
func appACLCurrentCatalogBeforeDeletion(t *testing.T, before, after AppACLEffectiveCatalogSnapshotR1) AppACLEffectiveCatalogSnapshotR1 {
	t.Helper()
	present := false
	for _, owner := range after.Owners {
		present = present || owner.SchemaName == "public" && owner.ObjectIdentity == "record_deletion_gates"
	}
	if !present {
		return after
	}
	for _, owner := range before.Owners {
		if owner.SchemaName == "public" && owner.ObjectIdentity == "record_deletion_gates" {
			return after
		}
	}
	var runtimeRole, adminRole, migratorRole, workerRole string
	for _, p := range before.DirectPrivileges {
		if p.ObjectClass == AppACLObjectClassTable && p.SchemaName == "public" && p.ObjectIdentity == "records" && p.Privilege == AppACLPrivilegeInsert {
			runtimeRole = p.Grantee
		}
	}
	for _, owner := range before.Owners {
		if owner.ObjectClass == AppACLObjectClassTable && owner.SchemaName == "public" {
			migratorRole = owner.OwnerRole
			break
		}
	}
	for _, role := range before.Roles {
		if role.Name != runtimeRole && role.Name != migratorRole && role.Name != "houfeng_records_authority" {
			if adminRole != "" {
				t.Fatal("ambiguous historical administrator role")
			}
			adminRole = role.Name
		}
	}
	oldRoles := make(map[string]bool)
	for _, role := range before.Roles {
		oldRoles[role.Name] = true
	}
	result := after
	result.Roles = nil
	for _, role := range after.Roles {
		if oldRoles[role.Name] {
			result.Roles = append(result.Roles, role)
			continue
		}
		if workerRole != "" || role != (AppACLEffectiveCatalogRoleStateR1{Name: role.Name, Login: true}) {
			t.Fatalf("unexpected C81 role %#v", role)
		}
		workerRole = role.Name
	}
	if runtimeRole == "" || adminRole == "" || migratorRole == "" || workerRole == "" {
		t.Fatal("cannot identify exact C81 role delta")
	}
	if !reflect.DeepEqual(result.Roles, before.Roles) || !reflect.DeepEqual(after.Memberships, before.Memberships) || !reflect.DeepEqual(after.ColumnACLs, before.ColumnACLs) || !reflect.DeepEqual(after.DefaultACLs, before.DefaultACLs) {
		t.Fatal("C81 changed role attributes, memberships or column/default ACLs")
	}
	objects := make(map[AppACLManagedObjectR1]bool)
	tableGrants := map[string][]AppACLPrivilegeKind{
		"record_deletion_gates":                          {AppACLPrivilegeSelect},
		"record_deletion_previews":                       {AppACLPrivilegeSelect},
		"record_markdown_client_purge_receipts":          {AppACLPrivilegeSelect},
		"record_comparison_purge_receipts":               {AppACLPrivilegeSelect},
		"comparison_result_dependencies":                 {AppACLPrivilegeSelect},
		"comparison_result_dependency_index_completions": {AppACLPrivilegeSelect},
		"browser_record_cleanup_events":                  {AppACLPrivilegeSelect},
		"browser_record_cleanup_receipts":                {AppACLPrivilegeSelect, AppACLPrivilegeInsert},
		"browser_record_cleanup_targets":                 {AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete},
		"markdown_client_draft_buffers":                  {AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete},
		"markdown_client_workspaces":                     {AppACLPrivilegeSelect, AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete},
		"comparison_client_workbenches":                  {AppACLPrivilegeSelect},
		"comparison_client_save_attempts":                {AppACLPrivilegeSelect},
	}
	grants := make(map[AppACLEffectiveCatalogPrivilegeObservationR1]bool)
	addGrant := func(role string, class AppACLObjectClass, schema, name string, kind AppACLPrivilegeKind) {
		grants[AppACLEffectiveCatalogPrivilegeObservationR1{Grantee: role, ObjectClass: class, SchemaName: schema, ObjectIdentity: name, Privilege: kind}] = true
	}
	for name, kinds := range tableGrants {
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: name}] = true
		for _, kind := range kinds {
			addGrant(runtimeRole, AppACLObjectClassTable, "public", name, kind)
		}
	}
	for _, name := range []string{"record_markdown_client_purge_receipts", "record_comparison_purge_receipts"} {
		addGrant(adminRole, AppACLObjectClassTable, "public", name, AppACLPrivilegeSelect)
	}
	for _, name := range []string{"app_acl_manifest_revisions", "app_acl_manifest_head", "schema_migrations", "deployment_membership", "deployment_contract_state"} {
		addGrant(workerRole, AppACLObjectClassTable, "public", name, AppACLPrivilegeSelect)
	}
	addGrant(workerRole, AppACLObjectClassDatabase, "", after.DatabaseName, AppACLPrivilegeConnect)
	addGrant(workerRole, AppACLObjectClassSchema, "", "public", AppACLPrivilegeUsage)
	addGrant(runtimeRole, AppACLObjectClassFunction, "", "public.record_text_set_is_canonical(text[])", AppACLPrivilegeExecute)
	// Independent inventory: do not derive this oracle from the production fragment.
	for _, name := range []string{
		"source_authority_admission", "source_application_principals", "source_retirement_barriers",
		"source_retirement_pins", "source_retirement_coverage_unknown",
		"local_blob_namespaces", "local_blob_namespace_incarnations", "local_blob_key_anchors",
		"record_write_obligations", "record_write_obligation_targets",
	} {
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: name}] = true
		for _, role := range []string{runtimeRole, adminRole, workerRole} {
			addGrant(role, AppACLObjectClassTable, "public", name, AppACLPrivilegeSelect)
		}
	}
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "source_legacy_retirement_bindings"}] = true
	for _, name := range []string{"public.source_legacy_retirement_install(bytea)", "public.source_legacy_retirement_verify(bytea)"} {
		addGrant(adminRole, AppACLObjectClassFunction, "", name, AppACLPrivilegeExecute)
	}
	for _, role := range []string{runtimeRole, adminRole, workerRole} {
		addGrant(role, AppACLObjectClassFunction, "", "public.source_legacy_retirement_blocked(text, text)", AppACLPrivilegeExecute)
	}
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "source_retirement_outbox"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "local_blob_gc_obligations"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "attachment_purge_preparations"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "attachment_purge_targets"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "attachment_purge_workspace_targets"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_portability_cleanup_preparations"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_portability_cleanup_targets"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_notification_delivery_starts"}] = true
	for _, name := range []string{"record_notification_delivery_facts", "record_notification_delivery_completions", "record_notification_delivery_handoffs"} {
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: name}] = true
	}
	for _, name := range []string{"record_birth_lineage", "record_birth_keys", "record_birth_bindings", "record_birth_enabled", "record_birth_allocations"} {
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "record_platform_internal", ObjectIdentity: name}] = true
	}
	for _, name := range []string{"record_birth_checkpoint_head", "record_birth_checkpoints"} {
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "record_platform_internal", ObjectIdentity: name}] = true
	}
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "record_platform_internal", ObjectIdentity: "record_external_delivery_commit_checks"}] = true
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "record_platform_internal", ObjectIdentity: "record_birth_launch_state"}] = true
	for _, name := range []string{"record_deletion_verified_entries", "record_deletion_effect_scopes", "record_deletion_recovery_sessions", "record_deletion_owner_commit_checks"} {
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassTable, SchemaName: "record_platform_internal", ObjectIdentity: name}] = true
	}
	for _, name := range []string{"record_deletion_verify_legacy_entry", "record_deletion_legacy_identity", "record_deletion_recovery_begin", "record_deletion_replay_v1"} {
		addGrant(adminRole, AppACLObjectClassFunction, "", "public."+name+"(bytea)", AppACLPrivilegeExecute)
	}
	for _, name := range []string{
		"record_deletion_verified_binding", "record_core_purge_v1", "record_core_purge_verify_v1",
		"record_core_purge_v2", "record_core_purge_verify_v2", "record_deletion_claim_v1",
		"record_deletion_claim_v2", "record_deletion_renew_v2", "record_deletion_finish_v2",
		"record_activity_purge_v1", "record_activity_purge_verify_v1", "record_search_purge_v1", "record_search_purge_verify_v1",
		"record_evidence_purge_prepare_v1", "record_evidence_purge_payload_v1", "record_evidence_purge_v1", "record_evidence_purge_verify_v1",
		"record_activity_purge_v2", "record_activity_purge_verify_v2", "record_search_purge_v2", "record_search_purge_verify_v2",
		"record_evidence_purge_prepare_v2", "record_evidence_purge_payload_v2", "record_evidence_purge_v2", "record_evidence_purge_verify_v2",
		"record_attachment_purge_prepare_v1", "record_attachment_purge_verify_v1",
		"record_attachment_gc_claim_v1", "record_attachment_gc_complete_v1", "record_attachment_gc_retry_v1", "record_attachment_gc_resolve_v1",
		"record_deletion_mark_delete_commit_unknown_v1", "record_deletion_record_delete_entry_v1",
		"record_deletion_mark_delete_witnessed_v1", "record_deletion_mark_outcome_commit_unknown_v1",
		"record_deletion_record_outcome_entry_v1", "record_deletion_finalize_not_committed_v1",
		"record_deletion_promote_permanent_fence_v1", "record_deletion_mark_read_fenced_v1",
		"record_deletion_begin_online_purge_v1", "record_deletion_complete_online_purge_v1",
		"record_deletion_mark_retry_required_v1", "record_deletion_resume_retry_v1",
	} {
		addGrant(workerRole, AppACLObjectClassFunction, "", "public."+name+"(bytea)", AppACLPrivilegeExecute)
	}
	addGrant(adminRole, AppACLObjectClassTable, "public", "source_retirement_outbox", AppACLPrivilegeSelect)
	for _, role := range []string{runtimeRole, adminRole, workerRole} {
		for _, name := range []string{"public.source_application_admit()", "public.source_maintenance_status()"} {
			addGrant(role, AppACLObjectClassFunction, "", name, AppACLPrivilegeExecute)
		}
	}
	if oldRoles["houfeng_records_authority"] {
		addGrant("houfeng_records_authority", AppACLObjectClassFunction, "", "public.source_maintenance_status()", AppACLPrivilegeExecute)
		addGrant("houfeng_records_authority", AppACLObjectClassFunction, "", "public.record_birth_launch_inspect(bigint, bytea)", AppACLPrivilegeExecute)
	}
	for _, name := range []string{
		"public.source_retirement_preview(text, text, text)",
		"public.source_retirement_commit(bigint, text, text, text, text, text, bytea, bytea, bytea, bytea)",
		"public.source_retirement_install_pin(bigint, text, bigint, bytea, bytea, bytea, bigint)",
		"public.local_blob_namespace_provision(bytea)", "public.local_blob_namespace_admit(bytea)",
	} {
		addGrant(adminRole, AppACLObjectClassFunction, "", name, AppACLPrivilegeExecute)
	}
	for _, role := range []string{runtimeRole, workerRole} {
		addGrant(role, AppACLObjectClassFunction, "", "public.local_blob_anchor_register(bytea)", AppACLPrivilegeExecute)
	}
	for _, role := range []string{runtimeRole, adminRole, workerRole} {
		addGrant(role, AppACLObjectClassFunction, "", "public.local_blob_recovery_inventory()", AppACLPrivilegeExecute)
	}
	for _, name := range []string{
		"public.record_birth_allocation_find(bytea)", "public.record_birth_current_binding()",
		"public.record_birth_checkpoint_proof(bytea)",
		"public.record_birth_continuity_binding()", "public.record_birth_continuity_assert(bytea)",
		"public.record_birth_allocation_get(bytea)", "public.record_birth_publication_bind(bytea)", "public.record_birth_publication_lookup(bytea)",
		"public.record_evidence_intent_insert(bytea)",
		"public.record_evidence_payload_insert(bytea)",
		"public.record_comparison_index_write(bytea, bytea)",
		"public.record_export_job_claim(bytea)", "public.record_export_job_advance(bytea)",
		"public.record_birth_allocation_begin(bytea)", "public.record_birth_receipt_register(bytea)",
		"public.record_birth_prepare_create(bytea)", "public.record_birth_committed(bytea)",
		"public.record_birth_certification(bytea)", "public.record_birth_consume(bytea)", "public.record_birth_finish(bytea)",
		"public.record_deletion_preview_create(bytea)", "public.record_deletion_preview_lock(bytea)",
		"public.record_deletion_issue(bytea)",
		"public.record_deletion_inventory_lock(bytea)",
		"public.record_mutation_rows_lock(bytea)",
		"public.record_serving_deletion_fence_lock(bytea)",
		"public.record_outbox_enqueue(bytea)", "public.record_outbox_claim(bytea)",
		"public.record_outbox_retry(bytea)", "public.record_outbox_sent(bytea)",
		"public.record_outbox_cancel(bytea)", "public.record_outbox_cleanup(bytea)",
		"public.record_external_delivery_outcome(bytea)",
		"public.record_external_delivery_start(bytea)", "public.record_external_delivery_cancel(bytea)",
		"public.record_external_delivery_reconcile(bytea)", "public.record_external_delivery_finalize(bytea)",
		"public.record_collaboration_delivery_inventory(text, bigint)",
		"public.record_collaboration_start_inventory(text, bigint)",
		"public.record_write_register(bytea)", "public.record_write_dispatch(bytea)",
		"public.record_write_resolve(bytea)", "public.record_write_temporary_created(bytea)",
		"public.record_write_return(bytea)",
		"public.record_blob_publication_bind(bytea)",
		"public.record_blob_publication_prepare(bytea)",
		"public.record_upload_part_publish(bytea)",
		"public.record_processor_publication_lock(bytea)", "public.record_processor_publication_finish(bytea)",
		"public.record_blob_pin_create(bytea)", "public.record_blob_pin_release(bytea)",
		"public.record_attachment_copy_source(bytea)", "public.record_attachment_copy_publish(bytea)",
		"public.record_attachment_draft_lock(bytea)", "public.record_attachment_reserve(bytea)", "public.record_attachment_state_set(bytea)",
		"public.record_draft_upload_parts_handoff(bytea)",
		"public.record_revision_namespace_lock(bytea)", "public.record_revision_blob_fence()",
		"public.record_import_attachment_publish(bytea)",
		"public.record_revision_attachment_publish(bytea)",
		"public.record_birth_allocation_lock(bytea)", "public.record_import_plan_lock(bytea)", "public.record_import_mapping_insert(bytea)",
		"public.record_export_artifact_publish(bytea)", "public.record_import_artifact_publish(bytea)",
		"public.record_export_artifact_revoke(bytea)",
		"public.local_blob_cleanup_claim(bytea)", "public.local_blob_cleanup_complete(bytea)",
		"public.record_write_bind_record(bytea)",
		"public.local_blob_gc_prepare(bytea)", "public.local_blob_gc_claim(bytea)", "public.local_blob_gc_resolve(bytea)",
		"public.local_blob_gc_bind_final(bytea)", "public.local_blob_gc_complete(bytea)",
	} {
		addGrant(runtimeRole, AppACLObjectClassFunction, "", name, AppACLPrivilegeExecute)
	}
	for _, name := range []string{"public.local_blob_purge_claim(bytea)", "public.local_blob_purge_complete(bytea)",
		"public.local_blob_purge_prepare(bytea)", "public.local_blob_purge_task_claim(bytea)",
		"public.local_attachment_purge_prepare(bytea)", "public.local_attachment_purge_complete(bytea)", "public.local_attachment_purge_verify(bytea)",
		"public.local_portability_purge_prepare(bytea)", "public.local_portability_purge_complete(bytea)", "public.local_portability_purge_verify(bytea)",
		"public.record_markdown_client_purge(bytea)", "public.record_markdown_client_purge_verify(bytea)",
		"public.record_comparison_purge(bytea)", "public.record_comparison_purge_verify(bytea)",
		"public.record_collaboration_purge_v2(bytea)", "public.record_collaboration_purge_verify_v2(bytea)",
		"public.record_outbox_drain_v2(bytea)",
		"public.local_blob_purge_task_resolve(bytea)", "public.local_blob_purge_bind_final(bytea)", "public.local_blob_purge_task_complete(bytea)"} {
		addGrant(workerRole, AppACLObjectClassFunction, "", name, AppACLPrivilegeExecute)
	}
	objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassSequence, SchemaName: "public", ObjectIdentity: "browser_record_cleanup_events_cursor_seq"}] = true
	functions := make(map[string]AppACLEffectiveCatalogFunctionR1)
	for _, name := range []string{"enforce_record_purge_operation_contract_version", "enforce_record_deletion_gate", "enforce_record_deletion_preview", "enforce_browser_record_cleanup_immutable", "record_text_set_is_canonical"} {
		schema, arguments := "record_platform_internal", ""
		if name == "record_text_set_is_canonical" {
			schema, arguments = "public", "text[]"
		}
		identity := schema + "." + name + "(" + arguments + ")"
		functions[identity] = AppACLEffectiveCatalogFunctionR1{SchemaName: schema, Name: name, IdentityArguments: arguments, Identity: identity, OwnerRole: migratorRole, Kind: "f", Config: []string{"search_path=pg_catalog"}}
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: schema, ObjectIdentity: name + "(" + arguments + ")"}] = true
	}
	for _, function := range []struct {
		schema, name, arguments string
		definer                 bool
	}{
		{"record_platform_internal", "source_legacy_retirement_command", "body bytea", false},
		{"record_platform_internal", "source_legacy_retirement_binding", "body bytea, install boolean", true},
		{"public", "source_legacy_retirement_install", "bytea", true},
		{"public", "source_legacy_retirement_verify", "bytea", true},
		{"public", "source_legacy_retirement_blocked", "text, text", true},
		{"record_platform_internal", "record_deletion_effect_scope", "target text, kinds text[]", true},
		{"record_platform_internal", "record_deletion_command", "body bytea, fields text[], numbers text[]", false},
		{"record_platform_internal", "record_deletion_canonical", "v jsonb", false},
		{"record_platform_internal", "record_core_surface_digest", "", false},
		{"record_platform_internal", "record_core_purge_effects", "op text, target text, project text, verify_only boolean", true},
		{"record_platform_internal", "record_core_purge", "bytea, bigint, boolean", true},
		{"record_platform_internal", "record_deletion_management_admit", "deployment text, rid text, generation bigint", true},
		{"record_platform_internal", "record_deletion_v1_continue", "body bytea, kind text", true},
		{"record_platform_internal", "record_deletion_verify_online_receipts_v1", "op text, expected bytea", true},
		{"record_platform_internal", "record_deletion_verify_online_receipts", "op text, expected bytea, version bigint", true},
		{"record_platform_internal", "record_deletion_verify_online_receipts_v2", "op text, expected bytea", true},
		{"record_platform_internal", "record_deletion_responsibilities_v2", "op text, project text, target text", true},
		{"record_platform_internal", "record_deletion_completed_obligation_v2", "id text", true},
		{"record_platform_internal", "workspace_purge_receipt_valid", "id text, surface bytea, receipt bytea, removed bigint, verified timestamp with time zone", false},
		{"record_platform_internal", "workspace_purge_manifest_valid", "op text", true},
		{"record_platform_internal", "record_deletion_recovery_surface_digest", "kind text", false},
		{"record_platform_internal", "record_deletion_recovery_id", "prefix text, domain text, deployment text, project text, operation text, kind text", false},
		{"record_platform_internal", "record_deletion_owner_commit_trigger", "", true},
		{"record_platform_internal", "record_deletion_queue_owner_commit_check", "op text, caller text, generation bigint, expires timestamp with time zone", true},
		{"record_platform_internal", "record_deletion_worker_admit_v2", "", true},
		{"record_platform_internal", "legacy_adapter_surface", "kind text", false},
		{"record_platform_internal", "legacy_adapter_receipt_digest", "kind text, op text, project text, target text, surface bytea, removed bigint", false},
		{"record_platform_internal", "legacy_adapter_lock", "body bytea, kind text", true},
		{"record_platform_internal", "adapter_lock_v2", "body bytea, kind text", true},
		{"record_platform_internal", "adapter_purge", "body bytea, kind text, verify_only boolean, version bigint", true},
		{"record_platform_internal", "evidence_purge_prepare", "body bytea, version bigint", true},
		{"record_platform_internal", "evidence_purge_payload", "body bytea, version bigint", true},
		{"record_platform_internal", "legacy_evidence_metadata", "target text", true},
		{"record_platform_internal", "legacy_adapter_purge", "body bytea, kind text, verify_only boolean", true},
		{"record_platform_internal", "attachment_v1_time", "t timestamp with time zone", true},
		{"record_platform_internal", "attachment_v1_deletion_id", "op text, backend text, key text, version text", true},
		{"record_platform_internal", "attachment_v1_gc_digest", "g blob_gc_deletions, result text", true},
		{"record_platform_internal", "attachment_v1_gc_document", "g blob_gc_deletions", true},
		{"record_platform_internal", "attachment_v1_live", "v jsonb", true},
		{"record_platform_internal", "attachment_v1_queue", "v jsonb", true},
		{"record_platform_internal", "attachment_v1_projection", "v jsonb", true},
		{"record_platform_internal", "record_attachment_purge_v1", "body bytea, verify_only boolean", true},
		{"record_platform_internal", "record_attachment_gc_v1", "body bytea, kind text", true},
		{"public", "record_attachment_purge_prepare_v1", "bytea", true},
		{"public", "record_attachment_purge_verify_v1", "bytea", true},
		{"public", "record_attachment_gc_claim_v1", "bytea", true},
		{"public", "record_attachment_gc_complete_v1", "bytea", true},
		{"public", "record_attachment_gc_retry_v1", "bytea", true},
		{"public", "record_attachment_gc_resolve_v1", "bytea", true},
		{"public", "record_deletion_verify_legacy_entry", "bytea", true},
		{"public", "record_deletion_verified_binding", "bytea", true},
		{"public", "record_deletion_legacy_identity", "bytea", true},
		{"public", "record_deletion_recovery_begin", "bytea", true},
		{"public", "record_deletion_replay_v1", "bytea", true},
		{"public", "record_core_purge_v1", "bytea", true},
		{"public", "record_core_purge_verify_v1", "bytea", true},
		{"public", "record_core_purge_v2", "bytea", true},
		{"public", "record_core_purge_verify_v2", "bytea", true},
		{"public", "record_activity_purge_v1", "bytea", true},
		{"public", "record_activity_purge_verify_v1", "bytea", true},
		{"public", "record_search_purge_v1", "bytea", true},
		{"public", "record_search_purge_verify_v1", "bytea", true},
		{"public", "record_evidence_purge_prepare_v1", "bytea", true},
		{"public", "record_evidence_purge_payload_v1", "bytea", true},
		{"public", "record_evidence_purge_v1", "bytea", true},
		{"public", "record_evidence_purge_verify_v1", "bytea", true},
		{"public", "record_activity_purge_v2", "bytea", true},
		{"public", "record_activity_purge_verify_v2", "bytea", true},
		{"public", "record_search_purge_v2", "bytea", true},
		{"public", "record_search_purge_verify_v2", "bytea", true},
		{"public", "record_evidence_purge_prepare_v2", "bytea", true},
		{"public", "record_evidence_purge_payload_v2", "bytea", true},
		{"public", "record_evidence_purge_v2", "bytea", true},
		{"public", "record_evidence_purge_verify_v2", "bytea", true},
		{"public", "record_deletion_claim_v1", "bytea", true},
		{"public", "record_deletion_claim_v2", "bytea", true},
		{"public", "record_deletion_renew_v2", "bytea", true},
		{"public", "record_deletion_finish_v2", "bytea", true},
		{"public", "record_deletion_mark_delete_commit_unknown_v1", "bytea", true},
		{"public", "record_deletion_record_delete_entry_v1", "bytea", true},
		{"public", "record_deletion_mark_delete_witnessed_v1", "bytea", true},
		{"public", "record_deletion_mark_outcome_commit_unknown_v1", "bytea", true},
		{"public", "record_deletion_record_outcome_entry_v1", "bytea", true},
		{"public", "record_deletion_finalize_not_committed_v1", "bytea", true},
		{"public", "record_deletion_promote_permanent_fence_v1", "bytea", true},
		{"public", "record_deletion_mark_read_fenced_v1", "bytea", true},
		{"public", "record_deletion_begin_online_purge_v1", "bytea", true},
		{"public", "record_deletion_complete_online_purge_v1", "bytea", true},
		{"public", "record_deletion_mark_retry_required_v1", "bytea", true},
		{"public", "record_deletion_resume_retry_v1", "bytea", true},
		{"record_platform_internal", "record_birth_command", "bytea, text[], text[]", false},
		{"record_platform_internal", "record_birth_validate_request", "jsonb", false},
		{"record_platform_internal", "record_birth_admit", "", true},
		{"record_platform_internal", "record_birth_binding", "", true},
		{"record_platform_internal", "record_birth_installed_binding", "", true},
		{"record_platform_internal", "record_birth_launch_validate", "raw bytea, kind text, source source_authority_admission", true},
		{"record_platform_internal", "record_birth_launch_stage", "center_body bytea, processor_body bytea, runtime_role name, worker_role name, expected_revision bigint", true},
		{"record_platform_internal", "record_birth_launch_close", "expected_revision bigint", true},
		{"record_platform_internal", "record_birth_launch_open", "expected_revision bigint", true},
		{"record_platform_internal", "record_birth_launch_admit", "source source_authority_admission", true},
		{"public", "record_birth_launch_inspect", "bigint, bytea", true},
		{"record_platform_internal", "record_birth_continuity_validate", "raw bytea", true},
		{"record_platform_internal", "record_birth_continuity_check", "raw bytea, exclusive_head boolean", true},
		{"record_platform_internal", "record_birth_continuity_guard", "", true},
		{"record_platform_internal", "record_birth_binding_bytes", "b record_platform_internal.record_birth_bindings", false},
		{"record_platform_internal", "record_birth_checkpoint_bytes", "c record_platform_internal.record_birth_checkpoints", true},
		{"record_platform_internal", "record_birth_checkpoint_append", "a record_platform_internal.record_birth_allocations", true},
		{"record_platform_internal", "record_birth_project", "a record_platform_internal.record_birth_allocations", true},
		{"record_platform_internal", "record_birth_request_row", "v jsonb", true},
		{"record_platform_internal", "record_birth_initial_commitment", "a record_platform_internal.record_birth_allocations", false},
		{"record_platform_internal", "record_birth_receipt_check", "v jsonb", true},
		{"record_platform_internal", "record_birth_actual_first", "a record_platform_internal.record_birth_allocations", true},
		{"record_platform_internal", "record_birth_events_valid", "a record_platform_internal.record_birth_allocations", true},
		{"record_platform_internal", "record_birth_writer_guard", "target text", true},
		{"record_platform_internal", "evidence_current_actor", "text, text", true},
		{"record_platform_internal", "evidence_live_source", "text, text", true},
		{"record_platform_internal", "evidence_target_guard", "text, text, text, text", true},
		{"public", "record_evidence_intent_insert", "bytea", true},
		{"public", "record_evidence_payload_insert", "bytea", true},
		{"public", "record_comparison_index_write", "bytea, bytea", true},
		{"record_platform_internal", "record_export_job_write", "bytea, text", true},
		{"public", "record_export_job_claim", "bytea", true},
		{"public", "record_export_job_advance", "bytea", true},
		{"record_platform_internal", "comparison_input_admit", "", true},
		{"record_platform_internal", "record_draft_launch_guard", "", true},
		{"record_platform_internal", "comparison_v1_cleanup_guard", "target text", true},
		{"record_platform_internal", "comparison_evidence_row_guard", "", true},
		{"record_platform_internal", "comparison_root_epoch_guard", "", true},
		{"record_platform_internal", "record_birth_allocation_guard", "", true},
		{"record_platform_internal", "record_birth_root_insert_guard", "", false},
		{"record_platform_internal", "record_birth_root_identity_guard", "", false},
		{"record_platform_internal", "record_birth_root_tombstone", "", true},
		{"record_platform_internal", "record_birth_root_complete", "", true},
		{"public", "record_birth_allocation_find", "bytea", true},
		{"public", "record_birth_allocation_get", "bytea", true},
		{"public", "record_birth_publication_bind", "bytea", true},
		{"public", "record_birth_publication_lookup", "bytea", true},
		{"public", "record_birth_current_binding", "", true},
		{"public", "record_birth_checkpoint_proof", "bytea", true},
		{"public", "record_birth_continuity_binding", "", true},
		{"public", "record_birth_continuity_assert", "bytea", true},
		{"public", "record_birth_allocation_begin", "bytea", true},
		{"public", "record_birth_receipt_register", "bytea", true},
		{"public", "record_birth_prepare_create", "bytea", true},
		{"public", "record_birth_committed", "bytea", true},
		{"public", "record_birth_certification", "bytea", true},
		{"public", "record_birth_consume", "bytea", true},
		{"public", "record_birth_finish", "bytea", true},
		{"record_platform_internal", "record_deletion_gate_cleanup_event", "", true},
		{"record_platform_internal", "record_deletion_issuer_admit", "", true},
		{"record_platform_internal", "record_comparison_writer_fence", "", true},
		{"public", "record_mutation_rows_lock", "bytea", true},
		{"record_platform_internal", "record_outbox_route_immutable", "", false},
		{"record_platform_internal", "record_outbox_transition", "bytea, text", true},
		{"public", "record_outbox_enqueue", "bytea", true},
		{"public", "record_outbox_claim", "bytea", true},
		{"public", "record_outbox_retry", "bytea", true},
		{"public", "record_outbox_sent", "bytea", true},
		{"public", "record_outbox_cancel", "bytea", true},
		{"public", "record_outbox_cleanup", "bytea", true},
		{"record_platform_internal", "record_delivery_retained_immutable", "", true},
		{"record_platform_internal", "record_external_delivery_command", "bytea, text", false},
		{"record_platform_internal", "record_external_delivery_correlation", "jsonb, bigint, timestamp with time zone", false},
		{"record_platform_internal", "record_external_delivery_fact_digest", "bytea, text, text, text", false},
		{"record_platform_internal", "record_external_delivery_scope", "jsonb, boolean", true},
		{"record_platform_internal", "record_external_delivery_lock_claim", "jsonb", true},
		{"record_platform_internal", "record_external_delivery_begin", "bytea, boolean", true},
		{"record_platform_internal", "record_external_delivery_finish", "bytea, boolean", true},
		{"record_platform_internal", "record_outbox_observed_route", "record_outbox", true},
		{"record_platform_internal", "record_outbox_handoff_complete", "text, text", true},
		{"record_platform_internal", "record_external_delivery_insert_guard", "", true},
		{"record_platform_internal", "record_external_delivery_commit_trigger", "", true},
		{"record_platform_internal", "record_external_delivery_queue_commit_check", "jsonb, text, bigint, timestamp with time zone", true},
		{"public", "record_external_delivery_start", "bytea", true},
		{"public", "record_external_delivery_cancel", "bytea", true},
		{"public", "record_external_delivery_reconcile", "bytea", true},
		{"public", "record_external_delivery_outcome", "bytea", true},
		{"public", "record_external_delivery_finalize", "bytea", true},
		{"public", "record_outbox_drain_v2", "bytea", true},
		{"public", "record_collaboration_delivery_inventory", "text, bigint", true},
		{"public", "record_collaboration_start_inventory", "text, bigint", true},
		{"public", "record_collaboration_purge_v2", "bytea", true},
		{"public", "record_collaboration_purge_verify_v2", "bytea", true},
		{"record_platform_internal", "record_deletion_reservation_gate_guard", "", true},
		{"record_platform_internal", "record_deletion_issuer_command", "bytea, text[]", false},
		{"record_platform_internal", "record_deletion_issuer_snapshot", "text", true},
		{"public", "record_deletion_preview_create", "bytea", true},
		{"public", "record_deletion_preview_lock", "bytea", true},
		{"public", "record_deletion_issue", "bytea", true},
		{"public", "record_deletion_inventory_lock", "bytea", true},
		{"record_platform_internal", "enforce_client_purge_receipt_immutable", "", false},
		{"record_platform_internal", "client_purge_field", "bytea", false},
		{"record_platform_internal", "client_metadata_purge", "bytea, text, boolean", true},
		{"public", "record_markdown_client_purge", "bytea", true},
		{"public", "record_markdown_client_purge_verify", "bytea", true},
		{"public", "record_comparison_purge", "bytea", true},
		{"public", "record_comparison_purge_verify", "bytea", true},
		{"record_platform_internal", "reject_source_retirement_mutation", "", true},
		{"record_platform_internal", "source_prepare_set_is_canonical", "text[]", false},
		{"record_platform_internal", "source_retirement_field", "bytea", false},
		{"record_platform_internal", "source_retirement_revision", "text, text", true},
		{"record_platform_internal", "source_membership_effect_guard", "", true},
		{"public", "source_application_admit", "", true},
		{"public", "source_maintenance_status", "", true},
		{"public", "source_retirement_preview", "text, text, text", true},
		{"public", "source_retirement_commit", "bigint, text, text, text, text, text, bytea, bytea, bytea, bytea", true},
		{"public", "source_retirement_install_pin", "bigint, text, bigint, bytea, bytea, bytea, bigint", true},
		{"record_platform_internal", "local_blob_immutable_identity", "", false},
		{"record_platform_internal", "record_write_obligation_identity_guard", "", false},
		{"record_platform_internal", "blob_gc_operation_identity_guard", "", false},
		{"record_platform_internal", "record_attachment_publication_guard", "", false},
		{"record_platform_internal", "local_blob_direct_login", "", true},
		{"record_platform_internal", "local_blob_command", "bytea, text[]", false},
		{"record_platform_internal", "local_blob_admission", "uuid, uuid", true},
		{"public", "local_blob_namespace_provision", "bytea", true},
		{"public", "local_blob_namespace_admit", "bytea", true},
		{"public", "local_blob_anchor_register", "bytea", true},
		{"record_platform_internal", "local_blob_record_epoch", "text", true},
		{"record_platform_internal", "record_blob_strong_fence", "", true},
		{"record_platform_internal", "record_write_origin", "jsonb", true},
		{"record_platform_internal", "local_blob_write_fence", "text, uuid, text, text", true},
		{"public", "record_write_register", "bytea", true},
		{"public", "record_write_dispatch", "bytea", true},
		{"public", "record_write_resolve", "bytea", true},
		{"public", "record_write_temporary_created", "bytea", true},
		{"public", "record_write_return", "bytea", true},
		{"public", "record_blob_publication_bind", "bytea", true},
		{"public", "record_blob_publication_prepare", "bytea", true},
		{"public", "record_upload_part_publish", "bytea", true},
		{"record_platform_internal", "record_processor_publication_command", "bytea", false},
		{"record_platform_internal", "record_processor_publication_lock", "jsonb", true},
		{"record_platform_internal", "record_blob_metadata_publish", "text, bytea, text, bigint, text, uuid", true},
		{"public", "record_processor_publication_lock", "bytea", true},
		{"public", "record_processor_publication_finish", "bytea", true},
		{"record_platform_internal", "record_blob_reference_fence", "text, text", true},
		{"public", "record_blob_pin_create", "bytea", true},
		{"public", "record_blob_pin_release", "bytea", true},
		{"record_platform_internal", "record_attachment_copy_source", "jsonb", true},
		{"public", "record_attachment_copy_source", "bytea", true},
		{"public", "record_attachment_copy_publish", "bytea", true},
		{"record_platform_internal", "record_attachment_draft_lock", "jsonb", true},
		{"public", "record_attachment_draft_lock", "bytea", true},
		{"public", "record_attachment_reserve", "bytea", true},
		{"public", "record_attachment_state_set", "bytea", true},
		{"public", "record_revision_namespace_lock", "bytea", true},
		{"public", "record_revision_blob_fence", "", true},
		{"public", "record_import_attachment_publish", "bytea", true},
		{"public", "record_revision_attachment_publish", "bytea", true},
		{"record_platform_internal", "record_import_plan_lock", "p_job text, p_namespace_ids text", true},
		{"public", "record_birth_allocation_lock", "bytea", true},
		{"public", "record_import_plan_lock", "bytea", true},
		{"public", "record_import_mapping_insert", "bytea", true},
		{"record_platform_internal", "record_artifact_publication_origin", "jsonb, text", true},
		{"public", "record_export_artifact_publish", "bytea", true},
		{"public", "record_import_artifact_publish", "bytea", true},
		{"public", "record_export_artifact_revoke", "bytea", true},
		{"record_platform_internal", "local_blob_cleanup_operation", "text, text, bigint, timestamp with time zone, text", true},
		{"record_platform_internal", "local_blob_origin_closed", "text", true},
		{"record_platform_internal", "local_blob_key_referenced", "uuid, text", true},
		{"record_platform_internal", "local_blob_cleanup_claim", "bytea, boolean", true},
		{"record_platform_internal", "local_blob_cleanup_complete", "bytea, boolean", true},
		{"public", "local_blob_cleanup_claim", "bytea", true},
		{"public", "local_blob_cleanup_complete", "bytea", true},
		{"public", "local_blob_purge_claim", "bytea", true},
		{"public", "local_blob_purge_complete", "bytea", true},
		{"record_platform_internal", "record_write_origin_handoff", "", true},
		{"record_platform_internal", "record_write_import_target_register", "", true},
		{"public", "record_write_bind_record", "bytea", true},
		{"record_platform_internal", "local_blob_gc_authority", "jsonb, boolean", true},
		{"record_platform_internal", "local_blob_gc_prepare", "bytea, boolean", true},
		{"record_platform_internal", "local_blob_gc_snapshot", "text", true},
		{"record_platform_internal", "local_blob_gc_claim", "bytea, boolean", true},
		{"record_platform_internal", "local_blob_gc_current", "jsonb, boolean", true},
		{"record_platform_internal", "local_blob_gc_resolve", "bytea, boolean", true},
		{"record_platform_internal", "local_blob_gc_bind_final", "bytea, boolean", true},
		{"record_platform_internal", "local_blob_gc_complete", "bytea, boolean", true},
		{"public", "local_blob_gc_prepare", "bytea", true},
		{"public", "local_blob_purge_prepare", "bytea", true},
		{"public", "local_blob_gc_claim", "bytea", true},
		{"public", "local_blob_gc_resolve", "bytea", true},
		{"public", "local_blob_gc_bind_final", "bytea", true},
		{"public", "local_blob_gc_complete", "bytea", true},
		{"public", "local_blob_purge_task_claim", "bytea", true},
		{"public", "local_blob_purge_task_resolve", "bytea", true},
		{"public", "local_blob_purge_bind_final", "bytea", true},
		{"public", "local_blob_purge_task_complete", "bytea", true},
		{"record_platform_internal", "local_blob_adapter_purge_authority", "bytea", true},
		{"record_platform_internal", "local_attachment_purge_finish", "bytea, boolean", true},
		{"public", "local_attachment_purge_prepare", "bytea", true},
		{"public", "local_attachment_purge_complete", "bytea", true},
		{"public", "local_attachment_purge_verify", "bytea", true},
		{"record_platform_internal", "local_portability_purge_finish", "bytea, boolean", true},
		{"public", "local_portability_purge_prepare", "bytea", true},
		{"public", "local_portability_purge_complete", "bytea", true},
		{"public", "local_portability_purge_verify", "bytea", true},
		{"public", "local_blob_recovery_inventory", "", true},
		{"public", "record_draft_upload_parts_handoff", "bytea", true},
		{"public", "record_serving_deletion_fence_lock", "bytea", true},
	} {
		name := function.name + "(" + function.arguments + ")"
		identity := function.schema + "." + name
		functions[identity] = AppACLEffectiveCatalogFunctionR1{
			SchemaName: function.schema, Name: function.name, IdentityArguments: function.arguments,
			Identity: identity, OwnerRole: migratorRole, Kind: "f", SecurityDefiner: function.definer,
			Config: []string{"search_path=pg_catalog"},
		}
		objects[AppACLManagedObjectR1{ObjectClass: AppACLObjectClassFunction, SchemaName: function.schema, ObjectIdentity: name}] = true
	}
	result.Owners = nil
	seen := make(map[AppACLManagedObjectR1]bool)
	for _, owner := range after.Owners {
		object := AppACLManagedObjectR1{ObjectClass: owner.ObjectClass, SchemaName: owner.SchemaName, ObjectIdentity: owner.ObjectIdentity}
		if !objects[object] {
			result.Owners = append(result.Owners, owner)
			continue
		}
		if owner.OwnerRole != migratorRole || seen[object] {
			t.Fatalf("unexpected C81 owner %#v", owner)
		}
		seen[object] = true
	}
	if len(seen) != len(objects) {
		t.Fatal("missing C81 managed object")
	}
	result.Functions = nil
	seenFunctions := make(map[string]bool)
	validatorSeen := false
	for _, function := range after.Functions {
		if function.Identity == "record_platform_internal.validate_record_revision_primary_subject()" {
			want := AppACLEffectiveCatalogFunctionR1{SchemaName: "record_platform_internal", Name: "validate_record_revision_primary_subject",
				IdentityArguments: "", Identity: function.Identity, OwnerRole: migratorRole, Kind: "f", SecurityDefiner: true,
				Config: []string{"search_path=pg_catalog"}}
			if validatorSeen || !reflect.DeepEqual(function, want) {
				t.Fatalf("unexpected changed-existing C81 validator: %#v", function)
			}
			want.SecurityDefiner = false
			predecessorSeen := false
			for _, previous := range before.Functions {
				if previous.Identity == want.Identity {
					if predecessorSeen || !reflect.DeepEqual(previous, want) {
						t.Fatalf("unexpected predecessor validator: %#v", previous)
					}
					predecessorSeen = true
				}
			}
			if !predecessorSeen {
				t.Fatal("missing predecessor validator")
			}
			result.Functions = append(result.Functions, want)
			validatorSeen = true
			continue
		}
		expected, ok := functions[function.Identity]
		if !ok {
			result.Functions = append(result.Functions, function)
			continue
		}
		if !reflect.DeepEqual(function, expected) || seenFunctions[function.Identity] {
			t.Fatalf("unexpected C81 function %#v", function)
		}
		seenFunctions[function.Identity] = true
	}
	if !validatorSeen {
		t.Fatal("missing changed-existing C81 validator")
	}
	if len(seenFunctions) != len(functions) {
		t.Fatal("missing C81 function")
	}
	revoked := map[AppACLEffectiveCatalogPrivilegeObservationR1]bool{
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "records", Privilege: AppACLPrivilegeInsert}:                               true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "records", Privilege: AppACLPrivilegeDelete}:                               true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_outbox", Privilege: AppACLPrivilegeInsert}:                         true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_outbox", Privilege: AppACLPrivilegeUpdate}:                         true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_outbox", Privilege: AppACLPrivilegeDelete}:                         true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "blob_objects", Privilege: AppACLPrivilegeInsert}:                          true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_export_jobs", Privilege: AppACLPrivilegeInsert}:                    true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_export_jobs", Privilege: AppACLPrivilegeUpdate}:                    true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_export_artifacts", Privilege: AppACLPrivilegeInsert}:               true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_export_artifacts", Privilege: AppACLPrivilegeUpdate}:               true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_import_artifacts", Privilege: AppACLPrivilegeInsert}:               true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "blob_publication_intents", Privilege: AppACLPrivilegeInsert}:              true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "attachment_upload_parts", Privilege: AppACLPrivilegeInsert}:               true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "blob_gc_pins", Privilege: AppACLPrivilegeInsert}:                          true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_attachments", Privilege: AppACLPrivilegeInsert}:                    true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_revision_attachments", Privilege: AppACLPrivilegeInsert}:           true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_import_entity_mappings", Privilege: AppACLPrivilegeInsert}:         true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "evidence_capture_intents", Privilege: AppACLPrivilegeInsert}:              true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "evidence_payloads", Privilege: AppACLPrivilegeInsert}:                     true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_notification_deliveries", Privilege: AppACLPrivilegeUpdate}:        true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_notification_delivery_attempts", Privilege: AppACLPrivilegeInsert}: true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "record_collaboration_purge_receipts", Privilege: AppACLPrivilegeInsert}:   true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassFunction, ObjectIdentity: "public.record_collaboration_purge(bytea)", Privilege: AppACLPrivilegeExecute}:                true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassFunction, ObjectIdentity: "public.record_activity_purge(bytea)", Privilege: AppACLPrivilegeExecute}:                     true,
		{Grantee: runtimeRole, ObjectClass: AppACLObjectClassFunction, ObjectIdentity: "public.record_search_purge(bytea)", Privilege: AppACLPrivilegeExecute}:                       true,
	}
	for _, table := range []string{"deletion_reservations", "deletion_fence_leases"} {
		for _, kind := range []AppACLPrivilegeKind{AppACLPrivilegeInsert, AppACLPrivilegeUpdate, AppACLPrivilegeDelete} {
			revoked[AppACLEffectiveCatalogPrivilegeObservationR1{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: table, Privilege: kind}] = true
		}
	}
	for _, table := range []string{"record_purge_operations", "deletion_replay_state"} {
		for _, kind := range []AppACLPrivilegeKind{AppACLPrivilegeInsert, AppACLPrivilegeUpdate} {
			revoked[AppACLEffectiveCatalogPrivilegeObservationR1{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: table, Privilege: kind}] = true
		}
	}
	for _, table := range []string{"record_deletion_audits", "record_core_purge_receipts", "record_activity_purge_receipts", "record_search_purge_receipts", "evidence_purge_receipts"} {
		revoked[AppACLEffectiveCatalogPrivilegeObservationR1{Grantee: runtimeRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: table, Privilege: AppACLPrivilegeInsert}] = true
	}
	for _, kind := range []AppACLPrivilegeKind{AppACLPrivilegeInsert, AppACLPrivilegeUpdate} {
		revoked[AppACLEffectiveCatalogPrivilegeObservationR1{Grantee: adminRole, ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "deletion_replay_state", Privilege: kind}] = true
	}
	project := func(old, current []AppACLEffectiveCatalogPrivilegeObservationR1, effective bool) []AppACLEffectiveCatalogPrivilegeObservationR1 {
		var projected []AppACLEffectiveCatalogPrivilegeObservationR1
		found := make(map[AppACLEffectiveCatalogPrivilegeObservationR1]bool)
		for _, privilege := range current {
			if revoked[privilege] {
				t.Fatalf("C81 retained revoked artifact privilege %#v", privilege)
			}
			if grants[privilege] {
				if found[privilege] {
					t.Fatalf("duplicate C81 grant %#v", privilege)
				}
				found[privilege] = true
				continue
			}
			// Unexpected grants are retained so the historical comparison rejects them.
			projected = append(projected, privilege)
		}
		if len(found) != len(grants) {
			t.Fatalf("C81 grants present %d, want %d", len(found), len(grants))
		}
		restored := make(map[AppACLEffectiveCatalogPrivilegeObservationR1]bool)
		for _, privilege := range old {
			if revoked[privilege] {
				if restored[privilege] {
					t.Fatalf("duplicate predecessor artifact grant %#v", privilege)
				}
				restored[privilege] = true
				projected = append(projected, privilege)
			}
		}
		if len(restored) != len(revoked) {
			t.Fatal("missing predecessor artifact grants")
		}
		sort.Slice(projected, func(i, j int) bool {
			a, b := projected[i], projected[j]
			left := [6]string{string(a.ObjectClass), a.SchemaName, a.ObjectIdentity, a.ColumnName, a.Grantee, string(a.Privilege)}
			right := [6]string{string(b.ObjectClass), b.SchemaName, b.ObjectIdentity, b.ColumnName, b.Grantee, string(b.Privilege)}
			if effective {
				left = [6]string{a.Grantee, string(a.ObjectClass), a.SchemaName, a.ObjectIdentity, a.ColumnName, string(a.Privilege)}
				right = [6]string{b.Grantee, string(b.ObjectClass), b.SchemaName, b.ObjectIdentity, b.ColumnName, string(b.Privilege)}
			}
			for k := range left {
				if left[k] != right[k] {
					return left[k] < right[k]
				}
			}
			return !a.GrantOption && b.GrantOption
		})
		return projected
	}
	result.DirectPrivileges = project(before.DirectPrivileges, after.DirectPrivileges, false)
	result.EffectivePrivileges = project(before.EffectivePrivileges, after.EffectivePrivileges, true)
	return result
}

func testPostgresRecordMutationRowLocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP80Migration)
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migrator := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migrator, profile)
	seedPostgresRecordMutationRows(t, ctx, migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtimeRole, fixture.adminRole, fixture.deletionWorkerRole); err != nil {
		t.Fatal(err)
	}
	runtime := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	admin := fixture.openRolePool(t, ctx, fixture.adminRole)
	worker := fixture.openRolePool(t, ctx, fixture.deletionWorkerRole)
	assertComparisonInputTriggerCatalog(t, ctx, migrator)
	const lockSQL = `select public.record_mutation_rows_lock($1::bytea)`
	const emptyGate = `{"family":"gate","project_id":"default","record_id":"rec_absent"}`

	for _, db := range []*pgxpool.Pool{admin, worker} {
		_, err := db.Exec(ctx, lockSQL, []byte(emptyGate))
		requirePostgresSQLState(t, err, "42501")
		_, err = db.Exec(ctx, `select public.record_comparison_index_write($1::bytea,$2::bytea)`, []byte(`{}`), []byte(`{}`))
		requirePostgresSQLState(t, err, "42501")
	}
	_, err := runtime.Exec(ctx, `select record_platform_internal.record_comparison_writer_fence()`)
	requirePostgresSQLState(t, err, "42501")
	_, err = runtime.Exec(ctx, lockSQL, []byte(emptyGate))
	requirePostgresSQLState(t, err, "42501")
	assumed, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assumed.Exec(ctx, `set local role `+pgx.Identifier{fixture.runtimeRole}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	_, err = assumed.Exec(ctx, lockSQL, []byte(emptyGate))
	requirePostgresSQLState(t, err, "42501")
	_ = assumed.Rollback(ctx)
	for _, statement := range []string{
		`update public.record_access_group_members set user_id = user_id where false`,
		`update public.record_revisions set title = title where false`,
		`update public.comparison_result_dependencies set source_record_id = source_record_id where false`,
		`update public.record_search_subjects set source_id = source_id where false`,
	} {
		_, err := runtime.Exec(ctx, statement)
		requirePostgresSQLState(t, err, "42501")
	}

	// The bigint advisory namespace has the same class/object words but a
	// different objsubid; it must not satisfy the two-integer fence contract.
	wrongKey, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongKey.Exec(ctx, `select pg_advisory_xact_lock_shared((1213154899::bigint << 32) | 81::bigint)`); err != nil {
		t.Fatal(err)
	}
	_, err = wrongKey.Exec(ctx, lockSQL, []byte(emptyGate))
	requirePostgresSQLState(t, err, "42501")
	_ = wrongKey.Rollback(ctx)

	// Observe a session lock, call the lock-only command, then explicitly drop
	// the session lock. The transaction pin must still exclude another backend.
	tx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select pg_advisory_lock_shared(1213154899, 81)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, lockSQL, []byte(emptyGate)); err != nil {
		t.Fatalf("lock absent gate with session fence: %v", err)
	}
	var unlocked bool
	if err := tx.QueryRow(ctx, `select pg_advisory_unlock_shared(1213154899, 81)`).Scan(&unlocked); err != nil || !unlocked {
		t.Fatalf("release session fence = %t, %v", unlocked, err)
	}
	other, err := worker.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback(ctx) }()
	var exclusive bool
	if err := other.QueryRow(ctx, `select pg_try_advisory_xact_lock(1213154899, 81)`).Scan(&exclusive); err != nil || exclusive {
		t.Fatalf("exclusive fence while transaction remains open = %t, %v", exclusive, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := other.QueryRow(ctx, `select pg_try_advisory_xact_lock(1213154899, 81)`).Scan(&exclusive); err != nil || !exclusive {
		t.Fatalf("exclusive fence after transaction commit = %t, %v", exclusive, err)
	}
	if err := other.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	exclusiveHolder, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exclusiveHolder.Exec(ctx, `select pg_advisory_xact_lock(1213154899, 81)`); err != nil {
		t.Fatal(err)
	}
	if _, err := exclusiveHolder.Exec(ctx, lockSQL, []byte(emptyGate)); err != nil {
		t.Fatalf("lock rows under exclusive comparison fence: %v", err)
	}
	if err := exclusiveHolder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	repeatable, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repeatable.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899, 81)`); err != nil {
		t.Fatal(err)
	}
	_, err = repeatable.Exec(ctx, lockSQL, []byte(emptyGate))
	requirePostgresSQLState(t, err, "25000")
	_ = repeatable.Rollback(ctx)

	for _, malformed := range []string{
		`null`, `[]`, `{}`, `{"family":"gate","family":"gate","project_id":"default","record_id":"rec_x"}`,
		`{"family":"gate","fa\u006dily":"gate","project_id":"default","record_id":"rec_x"}`,
		`{"family":"gate","project_id":"default"}`,
		`{"family":"gate","project_id":"default","record_id":"rec_x","extra":true}`,
		`{"family":"gate","project_id":"other","record_id":"rec_x"}`,
		`{"family":"gate","project_id":"default","record_id":null}`,
		`{"family":"record_gate","project_id":"default","record_id":"rec_x"}`,
		`{"family":"deletion_fence","project_id":"other","record_id":"rec_x"}`,
		`{"family":"deletion_fence","project_id":"default","record_id":"rec_x","expires_at":"later"}`,
		`{"family":"reservation","project_id":"default","record_id":"rec_x"}`,
		`{"family":"reservation","project_id":"other","record_id":"rec_x","reservation_id":"drs_x"}`,
		`{"family":"evidence_payload","payload_digest":"AB"}`,
		`{"family":"evidence_payload","payload_digest":"` + strings.Repeat("7A", 32) + `"}`,
		`{"family":"write_obligation","obligation_id":"rwo_` + strings.Repeat("AB", 16) + `"}`,
		`{"family":"write_obligation","obligation_id":"rwo_01"}`,
		`{"family":"write_obligation","obligation_id":null}`,
		`{"family":"search_subject","generation":"1","record_id":"rec_x","subject_kind":"vps","source_id":"vps_x","relation_role":"affected"}`,
		`{"family":"search_subject","generation":1.0,"record_id":"rec_x","subject_kind":"vps","source_id":"vps_x","relation_role":"affected"}`,
		`{"family":"search_subject","generation":0,"record_id":"rec_x","subject_kind":"vps","source_id":"vps_x","relation_role":"affected"}`,
		`{"family":"search_subject","generation":1,"project_id":"default","record_id":"rec_x","subject_kind":"vps","source_id":"vps_x","relation_role":"affected"}`,
	} {
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899, 81)`); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, lockSQL, []byte(malformed))
		if err == nil {
			t.Fatalf("accepted malformed row lock union %s", malformed)
		}
		requirePostgresSQLState(t, err, "22023")
		_ = tx.Rollback(ctx)
	}
	assertComparisonPurgeFencePrefix(t, ctx, runtime, worker, migrator)

	seedPostgresSuccessorMutationLockRows(t, ctx, migrator, runtime)
	missing, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899, 81)`); err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Exec(ctx, lockSQL, []byte(`{"family":"revision","record_id":"rec_other","revision_id":"rrv_rowlock"}`)); err != nil {
		t.Fatalf("lock absent revision scope: %v", err)
	}
	var available int
	if err := migrator.QueryRow(ctx, `select 1 from public.record_revisions where revision_id = 'rrv_rowlock' for update nowait`).Scan(&available); err != nil || available != 1 {
		t.Fatalf("absent scope locked another record's revision: value=%d error=%v", available, err)
	}
	if err := missing.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		command string
		probes  []string
	}{
		{"deletion_fence", `{"family":"deletion_fence","project_id":"default","record_id":"rec_rowlock"}`, []string{`select 1 from public.deletion_fence_leases where project_id='default' and object_kind='record' and object_id='rec_rowlock' for update nowait`}},
		{"reservation", `{"family":"reservation","project_id":"default","record_id":"rec_rowlock","reservation_id":"drs_rowlock"}`, []string{`select 1 from public.deletion_reservations where reservation_id='drs_rowlock' for update nowait`}},
		{"membership", `{"family":"membership","group_id":"rag_rowlock","user_id":"usr_rowlock"}`, []string{`select 1 from public.record_access_group_members where group_id = 'rag_rowlock' and user_id = 'usr_rowlock' for update nowait`}},
		{"revision", `{"family":"revision","record_id":"rec_rowlock","revision_id":"rrv_rowlock"}`, []string{`select 1 from public.record_revisions where revision_id = 'rrv_rowlock' for update nowait`}},
		{"revision_participant", `{"family":"revision_participant","revision_id":"rrv_rowlock","user_id":"usr_rowlock"}`, []string{`select 1 from public.record_revision_participants where revision_id = 'rrv_rowlock' and participant_id = 'usr_rowlock' for update nowait`}},
		{"evidence_payload", `{"family":"evidence_payload","payload_digest":"` + strings.Repeat("7a", 32) + `"}`, []string{`select 1 from public.evidence_payloads where payload_digest = decode(repeat('7a', 32), 'hex') for update nowait`}},
		{"evidence_intent", `{"family":"evidence_intent","intent_id":"evi_0123456789abcdef01234567"}`, []string{`select 1 from public.evidence_capture_intents where intent_id = 'evi_0123456789abcdef01234567' for update nowait`}},
		{"evidence_snapshot", `{"family":"evidence_snapshot","snapshot_id":"evs_rowlock"}`, []string{`select 1 from public.evidence_snapshots where snapshot_id = 'evs_rowlock' for update nowait`}},
		{"evidence_lineage", `{"family":"evidence_lineage","snapshot_id":"evs_rowlock"}`, []string{`select 1 from public.evidence_copy_lineage where snapshot_id = 'evs_rowlock' for update nowait`}},
		{"comparison_dependencies", `{"family":"comparison_dependencies","result_snapshot_id":"evs_rowlock"}`, []string{`select 1 from public.comparison_result_dependencies where result_snapshot_id = 'evs_rowlock' and source_record_id = 'rec_sourcea' for update nowait`, `select 1 from public.comparison_result_dependencies where result_snapshot_id = 'evs_rowlock' and source_record_id = 'rec_sourceb' for update nowait`}},
		{"comparison_completion", `{"family":"comparison_completion","result_snapshot_id":"evs_rowlock"}`, []string{`select 1 from public.comparison_result_dependency_index_completions where result_snapshot_id = 'evs_rowlock' for update nowait`}},
		{"export_job", `{"family":"export_job","project_id":"default","export_job_id":"rej_rowlock"}`, []string{`select 1 from public.record_export_jobs where export_job_id = 'rej_rowlock' for update nowait`}},
		{"write_obligation", `{"family":"write_obligation","obligation_id":"rwo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, []string{`select 1 from public.record_write_obligations where obligation_id = 'rwo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' for update nowait`}},
		{"search_subject", `{"family":"search_subject","generation":1,"record_id":"rec_rowlock","subject_kind":"vps","source_id":"vps_rowlock","relation_role":"affected"}`, []string{`select 1 from public.record_search_subjects where generation = 1 and record_id = 'rec_rowlock' and subject_kind = 'vps' and source_id = 'vps_rowlock' and relation_role = 'affected' for update nowait`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := runtime.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899, 81)`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, lockSQL, []byte(tc.command)); err != nil {
				t.Fatalf("acquire row lock: %v", err)
			}
			for _, probe := range tc.probes {
				var value int
				err := migrator.QueryRow(ctx, probe).Scan(&value)
				requirePostgresSQLState(t, err, "55P03")
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			for _, probe := range tc.probes {
				var value int
				if err := migrator.QueryRow(ctx, probe).Scan(&value); err != nil || value != 1 {
					t.Fatalf("row absent or still locked after rollback: value=%d error=%v", value, err)
				}
			}
		})
	}
}

// This independent oracle reads the complete row-trigger bindings, including
// the pre-existing immutable UPDATE guard on draft checkpoints.
func assertComparisonInputTriggerCatalog(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	for _, expected := range []struct {
		table, name, function string
		typeBits              int
		columns               []string
	}{
		{"evidence_snapshots", "comparison_snapshot_writer_guard", "comparison_evidence_row_guard", 15, []string{}},
		{"evidence_copy_lineage", "comparison_lineage_writer_guard", "comparison_evidence_row_guard", 15, []string{}},
		{"record_revision_evidence", "comparison_reference_writer_guard", "comparison_evidence_row_guard", 15, []string{}},
		{"evidence_payloads", "comparison_payload_cleanup_guard", "comparison_evidence_row_guard", 11, []string{}},
		{"records", "comparison_record_update_guard", "comparison_root_epoch_guard", 19, []string{}},
		{"records", "comparison_record_delete_guard", "comparison_root_epoch_guard", 11, []string{}},
		{"content_delivery_epochs", "comparison_epoch_insert_delete_guard", "comparison_root_epoch_guard", 15, []string{}},
		{"content_delivery_epochs", "comparison_epoch_update_guard", "comparison_root_epoch_guard", 19, []string{"delivery_epoch", "object_id", "object_kind", "project_id"}},
		{"record_drafts", "record_draft_launch_guard", "record_draft_launch_guard", 31, []string{}},
		{"record_draft_checkpoints", "record_draft_checkpoint_launch_guard", "record_draft_launch_guard", 15, []string{}},
		{"record_draft_checkpoints", "record_draft_checkpoints_reject_update", "reject_immutable_mutation", 19, []string{}},
	} {
		var enabled, schema, function string
		var typeBits int
		var ordinary, unqualified, triggerMetadata, functionMetadata, ownerMatches bool
		var columns, config []string
		err := db.QueryRow(ctx, `select t.tgenabled::text,t.tgtype::int,n.nspname,p.proname,
			not t.tgisinternal,t.tgqual is null,
			t.tgconstraint=0 and t.tgparentid=0 and not t.tgdeferrable and not t.tginitdeferred
			  and t.tgnargs=0 and octet_length(t.tgargs)=0 and t.tgoldtable is null and t.tgnewtable is null,
			p.pronargs=0 and p.prokind='f' and p.prosecdef and p.prorettype='pg_catalog.trigger'::regtype,
			pg_catalog.pg_get_userbyid(c.relowner)=current_user and pg_catalog.pg_get_userbyid(p.proowner)=current_user,
			coalesce(p.proconfig, array[]::text[]),
			coalesce((select array_agg(a.attname::text order by a.attname::text)
			  from unnest(t.tgattr::smallint[]) x(attnum)
			  join pg_catalog.pg_attribute a on a.attrelid=t.tgrelid and a.attnum=x.attnum),'{}'::text[])
			from pg_catalog.pg_trigger t join pg_catalog.pg_class c on c.oid=t.tgrelid
			join pg_catalog.pg_proc p on p.oid=t.tgfoid
			join pg_catalog.pg_namespace n on n.oid=p.pronamespace
			where t.tgrelid=pg_catalog.to_regclass('public.'||$1) and t.tgname=$2`, expected.table, expected.name,
		).Scan(&enabled, &typeBits, &schema, &function, &ordinary, &unqualified, &triggerMetadata,
			&functionMetadata, &ownerMatches, &config, &columns)
		if err != nil || enabled != "O" || typeBits != expected.typeBits || schema != "record_platform_internal" ||
			function != expected.function || !ordinary || !unqualified || !triggerMetadata || !functionMetadata || !ownerMatches ||
			!reflect.DeepEqual(config, []string{"search_path=pg_catalog"}) || !reflect.DeepEqual(columns, expected.columns) {
			t.Fatalf("comparison input trigger %s.%s: enabled=%q type=%d function=%s.%s ordinary=%t unqualified=%t trigger_metadata=%t function_metadata=%t owner=%t config=%q columns=%q error=%v",
				expected.table, expected.name, enabled, typeBits, schema, function, ordinary, unqualified, triggerMetadata,
				functionMetadata, ownerMatches, config, columns, err)
		}
	}
}

func assertV2DeletionTriggerCatalog(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	for _, expected := range []struct {
		schema, table, trigger, function string
		bits                             int
		deferred, definer                bool
	}{
		{"public", "attachment_purge_workspace_targets", "attachment_purge_workspace_target_immutable", "local_blob_immutable_identity", 27, false, false},
	} {
		var valid bool
		err := db.QueryRow(ctx, `select exists (
			select 1 from pg_trigger t join pg_class c on c.oid=t.tgrelid join pg_namespace n on n.oid=c.relnamespace
			join pg_proc f on f.oid=t.tgfoid join pg_namespace fn on fn.oid=f.pronamespace
			left join pg_constraint k on k.oid=t.tgconstraint
			where n.nspname=$1 and c.relname=$2 and t.tgname=$3 and t.tgtype=$5
			  and c.relkind='r' and pg_get_userbyid(c.relowner)=current_user and not t.tgisinternal and t.tgenabled='O'
			  and t.tgparentid=0 and t.tgdeferrable=$6 and t.tginitdeferred=$6 and t.tgqual is null
			  and t.tgnargs=0 and octet_length(t.tgargs)=0 and t.tgattr=''::int2vector
			  and t.tgoldtable is null and t.tgnewtable is null
			  and (($6 and k.contype='t' and k.conrelid=c.oid and k.condeferrable and k.condeferred) or (not $6 and t.tgconstraint=0))
			  and fn.nspname='record_platform_internal' and f.proname=$4 and f.prokind='f' and f.pronargs=0
			  and f.prosecdef=$7 and pg_get_userbyid(f.proowner)=current_user
		)`, expected.schema, expected.table, expected.trigger, expected.function, expected.bits, expected.deferred, expected.definer).Scan(&valid)
		if err != nil || !valid {
			t.Fatalf("V2 deletion trigger %s.%s is invalid: %v", expected.table, expected.trigger, err)
		}
	}
	var helperValid bool
	err := db.QueryRow(ctx, `select exists (
		select 1 from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid=p.pronamespace
		where n.nspname='record_platform_internal' and p.proname='record_deletion_completed_obligation_v2'
		  and pg_catalog.pg_get_function_identity_arguments(p.oid)='id text'
		  and pg_catalog.pg_get_function_result(p.oid)='boolean' and not p.proretset
		  and p.prokind='f' and p.prosecdef and pg_catalog.pg_get_userbyid(p.proowner)=current_user
		  and p.proconfig=array['search_path=pg_catalog']::text[]
		  and not exists(select 1 from pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
		    where a.privilege_type='EXECUTE' and a.grantee<>p.proowner)
	)`).Scan(&helperValid)
	if err != nil || !helperValid {
		t.Fatalf("completed obligation helper return type or authority is invalid: %v", err)
	}
}

// A malformed command cannot mutate anything; it still has to wait at the
// comparison prefix before reaching parsing or any record-rank lock.
func assertComparisonPurgeFencePrefix(t *testing.T, parent context.Context, runtime, worker, observer *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	holder, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `select pg_advisory_xact_lock(1213154899, 81)`); err != nil {
		t.Fatal(err)
	}
	var workerPID int32
	if err := worker.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := worker.Exec(ctx, `select * from public.record_comparison_purge($1::bytea)`, []byte(`{}`))
		result <- err
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting, graphHeld, recordHeld bool
		err := observer.QueryRow(ctx, `
			select exists (select 1 from pg_locks where pid = $1 and locktype = 'advisory'
			  and classid = 1213154899::oid and objid = 81::oid and objsubid = 2 and not granted),
			 exists (select 1 from pg_locks where pid = $1 and locktype = 'advisory'
			  and classid = 1213154899::oid and objid = 1::oid and objsubid = 2 and granted and mode = 'ShareLock'),
			 exists (select 1 from pg_locks where pid = $1 and locktype = 'relation' and granted
			  and relation in ('public.deletion_reservations'::regclass, 'public.content_delivery_epochs'::regclass,
			    'public.deletion_fence_leases'::regclass, 'public.record_deletion_gates'::regclass, 'public.records'::regclass)
			  and mode <> 'AccessShareLock')`, workerPID).Scan(&waiting, &graphHeld, &recordHeld)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			if !graphHeld || recordHeld {
				t.Fatalf("comparison fence wait: graph held=%t record lock held=%t", graphHeld, recordHeld)
			}
			break
		}
		select {
		case err := <-result:
			t.Fatalf("comparison purge returned before waiting at fence: %v", err)
		case <-ctx.Done():
			t.Fatalf("comparison purge did not reach fence wait: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		requirePostgresSQLState(t, err, "22023")
	case <-ctx.Done():
		t.Fatalf("comparison purge did not resume after fence release: %v", ctx.Err())
	}
}

func seedPostgresRecordMutationRows(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// These rows are lock targets seeded before C81; they are not a claim of
	// canonical evidence or comparison-index readiness.
	for _, statement := range []string{
		`insert into public.users (user_id, username, password_hash, display_name, role) values ('usr_rowlock','rowlock','fixture','Row lock','admin')`,
		`insert into public.records (record_id, lock_version, authorization_epoch) values ('rec_rowlock', 1, 1)`,
		`insert into public.record_revisions (revision_id, record_id, revision_no, title, body_markdown, markdown_dialect_version, record_type, impact_level, visibility_scope, visibility_digest, author_id, canonical_hash)
		 values ('rrv_rowlock', 'rec_rowlock', 1, 'row lock', '', 1, 'note', 'informational', '{}'::jsonb, decode(repeat('41',32),'hex'), 'usr_rowlock', decode(repeat('42',32),'hex'))`,
		`insert into public.record_revision_subjects (revision_id, ordinal, registry_version, subject_kind, relation_role, source_id, is_primary, identity_snapshot, capture_authorization, capture_authorization_digest)
		 values ('rrv_rowlock', 0, 1, 'vps', 'affected', 'vps_rowlock', true, '{}'::jsonb, '{}'::jsonb, decode(repeat('43',32),'hex'))`,
		`insert into public.record_revision_participants (revision_id, ordinal, participant_id, identity_snapshot) values ('rrv_rowlock', 0, 'usr_rowlock', '{}'::jsonb)`,
		`update public.records set current_revision_id='rrv_rowlock',current_title='row lock',current_record_type='note',current_impact_level='informational',current_visibility_scope='{}'::jsonb,current_visibility_digest=decode(repeat('41',32),'hex') where record_id='rec_rowlock'`,
		`insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values ('default','record','rec_rowlock',0)`,
		`insert into public.deletion_fence_leases(project_id,object_kind,object_id,owner_id,owner_generation,created_at,expires_at)
		 values ('default','record','rec_rowlock','rowlock_owner',1,transaction_timestamp()-interval '2 hours',transaction_timestamp()-interval '1 hour')`,
		`insert into public.deletion_reservations(reservation_id,project_id,object_kind,object_id,deletion_token_commitment,request_fingerprint,
		 actor_scope_digest,preview_binding_digest,preview_current_revision_id,preview_lock_version,preview_authorization_epoch,
		 preview_content_delivery_epoch,preview_dependency_graph_digest,preview_backup_inventory_digest,preview_processor_inventory_digest,
		 adapter_readiness_digest,adapter_preview_digest,preview_witness_sequence,preview_witness_entry_hash,state,expires_at)
		 values ('drs_rowlock','default','record','rec_rowlock',decode(repeat('51',32),'hex'),decode(repeat('52',32),'hex'),
		 decode(repeat('53',32),'hex'),decode(repeat('54',32),'hex'),'rrv_rowlock',1,1,0,
		 decode(repeat('55',32),'hex'),decode(repeat('56',32),'hex'),decode(repeat('57',32),'hex'),
		 decode(repeat('58',32),'hex'),decode(repeat('59',32),'hex'),1,decode(repeat('5a',32),'hex'),'previewed',transaction_timestamp()+interval '1 hour')`,
		`insert into public.record_access_groups (group_id, display_name) values ('rag_rowlock', 'Row lock')`,
		`insert into public.record_access_group_members (group_id, user_id) values ('rag_rowlock', 'usr_rowlock')`,
		`insert into public.evidence_payloads (payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload) values (decode(repeat('7a',32),'hex'), 1024, 1, '\x00'::bytea)`,
		`insert into public.evidence_snapshots (snapshot_id, record_id, kind, schema_version, source_kind, source_id, subject_identity_snapshot, source_identity_snapshot, capture_authorization, capture_authorization_digest, requested_started_at, requested_ended_at, actual_started_at, actual_ended_at, observed_at, captured_at, referenced_at, source_revision, source_digest, producer_version, calculation_version, actual_precision, bucket_width, unit_semantics, quality, quota_outcome, retention, sensitivity_level, redaction, canonical_hash, logical_size_bytes, payload_digest)
		 values ('evs_rowlock', 'rec_rowlock', 'monitoring.host.v1', 1, 'vps', 'vps_rowlock', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, decode(repeat('41',32),'hex'), transaction_timestamp(), transaction_timestamp(), transaction_timestamp(), transaction_timestamp(), transaction_timestamp(), transaction_timestamp(), transaction_timestamp(), 'rev-1', decode(repeat('42',32),'hex'), 'producer', 'calc', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, 'normal', '[]'::jsonb, decode(repeat('7a',32),'hex'), 1024, decode(repeat('7a',32),'hex'))`,
		`insert into public.evidence_copy_lineage (snapshot_id, copied_from_snapshot_id) values ('evs_rowlock', 'evs_source')`,
		`insert into public.evidence_capture_intents (intent_id, record_id, kind, schema_version, preview_digest, source_digest, selection, preview, snapshot_id, estimated_size_bytes, created_at, valid_until)
		 values ('evi_0123456789abcdef01234567', 'rec_rowlock', 'monitoring.host.v1', 1, decode(repeat('44',32),'hex'), decode(repeat('45',32),'hex'), '{}'::jsonb, '{}'::jsonb, 'evs_intent', 1024, transaction_timestamp(), transaction_timestamp() + interval '15 minutes')`,
		`insert into public.record_export_jobs(export_job_id,actor_id,idempotency_key,export_kind,export_mode,job_state,
		 request_fingerprint,inventory_digest,authorization_epoch,record_id,revision_id,owner_id,owner_generation,lease_expires_at,expires_at)
		 values ('rej_rowlock','usr_rowlock','rowlock','archive','safe','staging',decode(repeat('48',32),'hex'),
		 decode(repeat('49',32),'hex'),1,'rec_rowlock','rrv_rowlock','rowlock_worker',1,transaction_timestamp()+interval '30 minutes',transaction_timestamp()+interval '1 hour')`,
		`insert into public.record_search_generations (generation) values (1)`,
		`insert into public.record_search_documents (generation, record_id, current_revision_id, record_lock_version, authorization_epoch, record_fence_epoch, lifecycle, record_type, impact_level, title, visibility_kind, visibility_digest, record_created_at, record_updated_at, document_digest)
		 values (1, 'rec_rowlock', 'rrv_rowlock', 1, 1, 0, 'active', 'note', 'informational', 'row lock', 'project', decode(repeat('46',32),'hex'), transaction_timestamp(), transaction_timestamp(), decode(repeat('47',32),'hex'))`,
		`insert into public.record_search_subjects (generation, record_id, subject_kind, relation_role, source_id, is_primary) values (1, 'rec_rowlock', 'vps', 'affected', 'vps_rowlock', true)`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("seed row lock fixture: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit row lock fixture: %v", err)
	}
}

func seedPostgresSuccessorMutationLockRows(t *testing.T, ctx context.Context, migrator, runtime *pgxpool.Pool) {
	t.Helper()
	// The comparison rows are corruption/lock targets only. They do not
	// establish canonical index coverage or deletion-issuer authority.
	for _, statement := range []string{
		`insert into public.comparison_result_dependencies (result_snapshot_id, source_record_id) values ('evs_rowlock', 'rec_sourcea'), ('evs_rowlock', 'rec_sourceb')`,
		`insert into public.comparison_result_dependency_index_completions (result_snapshot_id, payload_digest) values ('evs_rowlock', decode(repeat('7a',32),'hex'))`,
	} {
		if _, err := migrator.Exec(ctx, statement); err != nil {
			t.Fatalf("seed comparison lock target: %v", err)
		}
	}
	content := []byte("row lock export")
	digest := sha256.Sum256(content)
	encodedDigest := hex.EncodeToString(digest[:])
	registration, err := json.Marshal(map[string]any{
		"obligation_id": "rwo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "writer_instance": "c0000000-0000-4000-8000-000000000001", "request_seq": 1,
		"project_id": "default", "origin_kind": "export_artifact", "origin_id": "rej_rowlock", "origin_generation": 1,
		"origin_owner_id": nil, "origin_owner_generation": nil, "origin_owner_expires_at": nil,
		"draft_id": nil, "plan_digest": nil, "archive_digest": nil,
		"namespace_id": nil, "incarnation_id": nil, "backend_kind": "s3",
		"key": "sha256/" + encodedDigest, "sha256": encodedDigest, "size": len(content),
		"publication_id": nil, "target_record_id": nil, "target_attachment_id": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Exec(ctx, `select public.record_write_register($1::bytea)`, registration); err != nil {
		t.Fatalf("register export write lock target: %v", err)
	}
}
