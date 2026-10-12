package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/db/migrations"
)

const appACLCurrentP74Migration = "0074_add_subscription_exchange_rate_month_end.sql"

func testPostgresIntegrationAppACLCurrentP73Upgrade(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessorProfile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP73Migration)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, predecessorProfile)
	// C73 已有实时汇率缓存：升级后它仍是实时汇率（month_end_for 为空），实时唯一键不变。
	if _, err := migratorDB.Exec(ctx, `
		insert into public.subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date, fetched_at)
		values ('sub_rate_p73_live', 'frankfurter', 'CNY', 'USD', 7.1, '2026-09-30', '2026-10-01 00:00:00+00');
	`); err != nil {
		t.Fatalf("seed C73 live exchange rate: %v", err)
	}

	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if len(before.Ledger) == 0 || before.Ledger[len(before.Ledger)-1].Name != appACLCurrentP73Migration {
		t.Fatalf("C73 predecessor ledger tail = %#v, want 0073", before.Ledger)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C73 to C74 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C73 to C74 successor = %#v, want revision %d linked to predecessor", successor, predecessor.ManifestRevision+1)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit C74 runtime: %v", err)
	}

	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("C74 manifest history length = %d, want %d", len(after.Manifest.Manifests), len(before.Manifest.Manifests)+1)
	}
	if len(after.Ledger) != len(before.Ledger)+1 ||
		!reflect.DeepEqual(after.Ledger[:len(before.Ledger)], before.Ledger) ||
		after.Ledger[len(after.Ledger)-1].Name != appACLCurrentP74Migration {
		t.Fatalf("C74 ledger tail = %#v, want predecessor prefix plus one 0074 row", after.Ledger)
	}
	if !reflect.DeepEqual(after.Catalog.DirectPrivileges, before.Catalog.DirectPrivileges) ||
		!reflect.DeepEqual(after.Catalog.EffectivePrivileges, before.Catalog.EffectivePrivileges) ||
		!reflect.DeepEqual(after.Catalog.ColumnACLs, before.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(after.Catalog.DefaultACLs, before.Catalog.DefaultACLs) {
		t.Fatalf("C73 to C74 migration changed ACL state\nbefore direct/effective/column/default: %#v/%#v/%#v/%#v\nafter: %#v/%#v/%#v/%#v",
			before.Catalog.DirectPrivileges, before.Catalog.EffectivePrivileges, before.Catalog.ColumnACLs, before.Catalog.DefaultACLs,
			after.Catalog.DirectPrivileges, after.Catalog.EffectivePrivileges, after.Catalog.ColumnACLs, after.Catalog.DefaultACLs)
	}

	var existingIsLive bool
	if err := migratorDB.QueryRow(ctx, `
		select month_end_for is null from public.subscription_exchange_rates where rate_id = 'sub_rate_p73_live'
	`).Scan(&existingIsLive); err != nil || !existingIsLive {
		t.Fatalf("C73 live rate after upgrade is live = %t (err %v), want true", existingIsLive, err)
	}
	// 月末汇率与同一 rate_date 的实时汇率共存；同一月份只能有一条月末汇率。
	if _, err := migratorDB.Exec(ctx, `
		insert into public.subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date, month_end_for)
		values ('sub_rate_p74_month_end', 'frankfurter', 'CNY', 'USD', 7.09, '2026-09-30', '2026-09-30');
	`); err != nil {
		t.Fatalf("insert month-end rate sharing the live rate date: %v", err)
	}
	for name, statement := range map[string]string{
		"second month-end row for the same month": `insert into public.subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date, month_end_for)
			values ('sub_rate_p74_dup', 'frankfurter', 'CNY', 'USD', 7.0, '2026-09-29', '2026-09-30')`,
		"second live row for the same rate date": `insert into public.subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date)
			values ('sub_rate_p74_live_dup', 'frankfurter', 'CNY', 'USD', 7.0, '2026-09-30')`,
		"month_end_for not the last day of a month": `insert into public.subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date, month_end_for)
			values ('sub_rate_p74_mid', 'frankfurter', 'CNY', 'USD', 7.0, '2026-08-14', '2026-08-15')`,
		"rate_date outside the month": `insert into public.subscription_exchange_rates (rate_id, provider, base_currency, quote_currency, rate, rate_date, month_end_for)
			values ('sub_rate_p74_outside', 'frankfurter', 'CNY', 'USD', 7.0, '2026-07-31', '2026-08-31')`,
	} {
		_, err := migratorDB.Exec(ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "23505" && pgErr.Code != "23514") {
			t.Fatalf("%s error = %v, want unique or check violation", name, err)
		}
	}
	if _, err := migratorDB.Exec(ctx, `delete from public.subscription_exchange_rates where rate_id in ('sub_rate_p73_live', 'sub_rate_p74_month_end')`); err != nil {
		t.Fatalf("clean C74 exchange rate probes: %v", err)
	}
	after = readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)

	beforeRepeat := after
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("repeat C73 to C74 convergence: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("C74 repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
	}
}
