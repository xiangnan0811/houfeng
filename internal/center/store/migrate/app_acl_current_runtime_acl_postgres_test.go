package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"houfeng/db/migrations"
)

func testPostgresIntegrationAppACLCurrentRuntimeUpdateDrift(t *testing.T) {
	for _, name := range []string{"missing_service_update", "missing_domain_update", "extra_delete", "column_acl", "grant_option", "public_acl", "membership"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			if _, err := ConvergeAppACLCurrent(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
				t.Fatal(err)
			}
			runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
			if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
				t.Fatal(err)
			}
			role := pgx.Identifier{fixture.runtime}.Sanitize()
			var drift string
			switch name {
			case "missing_service_update":
				drift = "revoke update on public.asset_services from " + role
			case "missing_domain_update":
				drift = "revoke update on public.asset_domains from " + role
			case "extra_delete":
				drift = "grant delete on public.asset_services to " + role
			case "column_acl":
				drift = "grant update(status) on public.asset_domains to " + role
			case "grant_option":
				drift = "grant update on public.asset_services to " + role + " with grant option"
			case "public_acl":
				drift = "grant update on public.asset_domains to public"
			case "membership":
				drift = "grant " + pgx.Identifier{fixture.admin}.Sanitize() + " to " + role
			}
			if _, err := fixture.db.Exec(ctx, drift); err != nil {
				t.Fatal(err)
			}
			_, _, input := appACLCurrentPostgresContract(t, fixture, migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migrator, input)
			if err := AdmitAppACLCurrentRuntime(ctx, runtime); err == nil || errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
				t.Fatalf("runtime drift admission = %v, want concrete catalog rejection", err)
			}
			after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migrator, input)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("runtime admission mutated drifted catalog or durable state")
			}
		})
	}
}
