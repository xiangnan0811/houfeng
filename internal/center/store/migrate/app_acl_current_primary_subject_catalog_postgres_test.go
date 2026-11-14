package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/db/migrations"
)

// The tampering stays inside a rolled-back transaction in an isolated database.
// The same catalog reader is used by both direct-role admission and convergence.
func TestPostgresIntegrationPrimarySubjectCatalogTampering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatal(err)
	}
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker)
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("baseline runtime admission: %v", err)
	}
	if err := AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatalf("baseline worker admission: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture, migrations.FS, appACLCurrentMigrationFragments)

	type tamper struct{ name, sql string }
	validator := `record_platform_internal.validate_record_revision_primary_subject()`
	tampers := []tamper{
		{"function_security_invoker", `alter function ` + validator + ` security invoker`},
		{"function_owner", `alter function ` + validator + ` owner to ` + pgx.Identifier{fixture.bootstrapOwner}.Sanitize()},
		{"function_public_execute", `grant execute on function ` + validator + ` to public`},
		{"function_runtime_execute", `grant execute on function ` + validator + ` to ` + pgx.Identifier{fixture.runtime}.Sanitize()},
		{"function_admin_execute", `grant execute on function ` + validator + ` to ` + pgx.Identifier{fixture.admin}.Sanitize()},
		{"function_worker_execute", `grant execute on function ` + validator + ` to ` + pgx.Identifier{fixture.deletionWorker}.Sanitize()},
		{"function_search_path", `alter function ` + validator + ` set search_path=public`},
		{"function_search_path_reset", `alter function ` + validator + ` reset search_path`},
	}
	for _, table := range []string{"record_revisions", "record_revision_subjects"} {
		qualified := `public.` + pgx.Identifier{table}.Sanitize()
		tampers = append(tampers,
			tamper{table + "_owner", `alter table ` + qualified + ` owner to ` + pgx.Identifier{fixture.bootstrapOwner}.Sanitize()},
			tamper{table + "_rls_enabled", `alter table ` + qualified + ` enable row level security`},
			tamper{table + "_rls_forced", `alter table ` + qualified + ` force row level security`},
		)
	}
	for _, trigger := range []struct{ table, name, events, wrongEvents string }{
		{"record_revisions", "record_revisions_require_primary_subject", "insert", "update"},
		{"record_revision_subjects", "record_revision_subjects_require_primary_subject", "insert or delete", "insert"},
	} {
		qualified := `public.` + pgx.Identifier{trigger.table}.Sanitize()
		name := pgx.Identifier{trigger.name}.Sanitize()
		otherTable := "record_revisions"
		if trigger.table == otherTable {
			otherTable = "record_revision_subjects"
		}
		otherQualified := `public.` + pgx.Identifier{otherTable}.Sanitize()
		drop := `drop trigger ` + name + ` on ` + qualified + `; `
		create := func(events, deferral, when, function, args string) string {
			return `create constraint trigger ` + name + ` after ` + events + ` on ` + qualified + ` ` + deferral + ` for each row ` + when + ` execute function ` + function + `(` + args + `)`
		}
		for _, tc := range []struct{ name, sql string }{
			{"missing", drop[:len(drop)-2]},
			{"disabled", `alter table ` + qualified + ` disable trigger ` + name},
			{"replica", `alter table ` + qualified + ` enable replica trigger ` + name},
			{"always", `alter table ` + qualified + ` enable always trigger ` + name},
			{"events", drop + create(trigger.wrongEvents, "deferrable initially deferred", "", "record_platform_internal.validate_record_revision_primary_subject", "")},
			{"timing", drop + `create trigger ` + name + ` before ` + trigger.events + ` on ` + qualified + ` for each row execute function record_platform_internal.validate_record_revision_primary_subject()`},
			{"wrong_attachment", drop + `create constraint trigger ` + name + ` after ` + trigger.events + ` on ` + otherQualified + ` deferrable initially deferred for each row execute function record_platform_internal.validate_record_revision_primary_subject()`},
			{"not_deferrable", drop + create(trigger.events, "not deferrable", "", "record_platform_internal.validate_record_revision_primary_subject", "")},
			{"initially_immediate", drop + create(trigger.events, "deferrable initially immediate", "", "record_platform_internal.validate_record_revision_primary_subject", "")},
			{"arguments", drop + create(trigger.events, "deferrable initially deferred", "", "record_platform_internal.validate_record_revision_primary_subject", "'unexpected'")},
			{"condition", drop + create(trigger.events, "deferrable initially deferred", "when (true)", "record_platform_internal.validate_record_revision_primary_subject", "")},
			{"wrong_function", drop + create(trigger.events, "deferrable initially deferred", "", "record_platform_internal.reject_immutable_mutation", "")},
		} {
			tampers = append(tampers, tamper{trigger.table + "_" + tc.name, tc.sql})
		}
	}
	tampers = append(tampers, tamper{"extra_attachment", `create constraint trigger primary_subject_unexpected_attachment after insert on public.record_revisions deferrable initially deferred for each row execute function record_platform_internal.validate_record_revision_primary_subject()`})

	for _, tc := range tampers {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if err := verifyRecordDeletionAuthorityCatalog(ctx, tx, fixture.migrator); err != nil {
				t.Fatalf("baseline authority catalog: %v", err)
			}
			if _, err := tx.Exec(ctx, tc.sql); err != nil {
				t.Fatalf("apply %s: %v", tc.name, err)
			}
			if err := verifyRecordDeletionAuthorityCatalog(ctx, tx, fixture.migrator); err == nil {
				t.Fatal("authority verifier accepted drift")
			}
			if _, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input); err == nil {
				t.Fatal("admission/convergence catalog reader accepted drift")
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("roll back tampering: %v", err)
			}
			check, err := migrator.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = check.Rollback(ctx) }()
			if err := verifyRecordDeletionAuthorityCatalog(ctx, check, fixture.migrator); err != nil {
				t.Fatalf("rollback failed to restore authority catalog: %v", err)
			}
		})
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("runtime admission after rollback: %v", err)
	}
	if err := AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatalf("worker admission after rollback: %v", err)
	}
	if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatalf("convergence after rollback: %v", err)
	}

}
