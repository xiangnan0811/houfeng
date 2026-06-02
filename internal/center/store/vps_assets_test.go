package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresVPSAssetMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0017_add_vps_assets.sql"))
	if err != nil {
		t.Fatalf("ReadFile(vps asset migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists vps_assets",
		"vps_id text primary key",
		"provider_id text references providers(provider_id) on delete set null",
		"display_name text not null",
		"ssh_port integer not null default 22",
		"labels text[] not null default '{}'",
		"archived_at timestamptz",
		"vps_assets_display_name_not_blank",
		"length(btrim(display_name)) > 0",
		"vps_assets_lifecycle_status_allowed",
		"'active', 'idle', 'testing', 'to_migrate', 'to_cancel', 'cancelled', 'archived'",
		"vps_assets_usage_status_allowed",
		"'in_use', 'idle', 'standby', 'testing', 'unknown'",
		"vps_assets_renewal_decision_allowed",
		"'unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced'",
		"vps_assets_ssh_port_range",
		"ssh_port between 1 and 65535",
		"idx_vps_assets_provider",
		"idx_vps_assets_status",
		"idx_vps_assets_location",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("vps asset migration missing %q", snippet)
		}
	}
}

func TestPostgresVPSAssetHistoryMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0021_create_asset_histories.sql"))
	if err != nil {
		t.Fatalf("ReadFile(asset history migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists ip_histories",
		"ip_history_id text primary key",
		"vps_id text not null references vps_assets(vps_id) on delete cascade",
		"from_ipv4 text not null default ''",
		"to_ipv4 text not null default ''",
		"from_ipv6 text not null default ''",
		"to_ipv6 text not null default ''",
		"ip_histories_changed",
		"create table if not exists vps_spec_snapshots",
		"snapshot_id text primary key",
		"product_name text not null default ''",
		"ssh_port integer not null",
		"captured_at timestamptz not null default now()",
		"vps_spec_snapshots_ssh_port_range",
		"idx_ip_histories_vps_time",
		"idx_vps_spec_snapshots_vps_time",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("asset history migration missing %q", snippet)
		}
	}
}

func TestPostgresVPSAssetCreateListGetAndPatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	providerID := "pv_001"
	archivedAt := now.Add(-time.Hour)
	var queryCalls []string
	var queryArgs [][]any
	var rowCalls []string
	var rowArgs [][]any
	repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			return &fakeVPSAssetRows{rows: []fakeVPSAssetScan{
				{scan: func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, vpsassets.Record{
						VPSID:           "vps_001",
						DisplayName:     "Akamai Edge",
						ProviderID:      &providerID,
						LifecycleStatus: vpsassets.LifecycleActive,
						UsageStatus:     vpsassets.UsageInUse,
						RenewalDecision: vpsassets.RenewalKeep,
						SSHPort:         22,
						Labels:          []string{"edge"},
						CreatedAt:       now,
						UpdatedAt:       now,
					})
					return nil
				}},
				{scan: func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, vpsassets.Record{
						VPSID:           "vps_002",
						DisplayName:     "Tokyo Lab",
						LifecycleStatus: vpsassets.LifecycleTesting,
						UsageStatus:     vpsassets.UsageTesting,
						RenewalDecision: vpsassets.RenewalObserve,
						SSHPort:         2222,
						Labels:          []string{"lab"},
						CreatedAt:       now,
						UpdatedAt:       now,
					})
					return nil
				}},
			}}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "insert into vps_assets"):
				return fakeVPSAssetRow{scan: func(dest ...any) error {
					vpsID, ok := args[0].(string)
					if !ok || !strings.HasPrefix(vpsID, "vps_") {
						t.Fatalf("generated vps id arg = %#v, want vps_ prefix", args[0])
					}
					scanVPSAssetRecordDestinations(dest, vpsassets.Record{
						VPSID:           vpsID,
						DisplayName:     "Tokyo Edge",
						ProviderID:      &providerID,
						ProviderName:    "Hetzner",
						ProductName:     "CX22",
						Country:         "JP",
						City:            "Tokyo",
						SSHPort:         22,
						LifecycleStatus: vpsassets.LifecycleActive,
						UsageStatus:     vpsassets.UsageInUse,
						RenewalDecision: vpsassets.RenewalUnreviewed,
						Importance:      "normal",
						Labels:          []string{"edge"},
						CreatedAt:       now,
						UpdatedAt:       now,
					})
					return nil
				}}
			case strings.Contains(sql, "where vps_id = $1") && strings.Contains(sql, "select"):
				return fakeVPSAssetRow{scan: func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, vpsassets.Record{
						VPSID:           "vps_001",
						DisplayName:     "Akamai Edge",
						ProviderID:      &providerID,
						SSHPort:         22,
						LifecycleStatus: vpsassets.LifecycleActive,
						UsageStatus:     vpsassets.UsageInUse,
						RenewalDecision: vpsassets.RenewalKeep,
						Importance:      "normal",
						Labels:          []string{"edge"},
						CreatedAt:       now,
						UpdatedAt:       now,
					})
					return nil
				}}
			case strings.Contains(sql, "update vps_assets"):
				return fakeVPSAssetRow{scan: func(dest ...any) error {
					scanVPSAssetRecordDestinations(dest, vpsassets.Record{
						VPSID:           "vps_001",
						DisplayName:     "Akamai Edge Archived",
						ProviderID:      nil,
						SSHPort:         22,
						LifecycleStatus: vpsassets.LifecycleArchived,
						UsageStatus:     vpsassets.UsageIdle,
						RenewalDecision: vpsassets.RenewalCancel,
						Importance:      "critical",
						Labels:          []string{"edge", "backup"},
						CreatedAt:       now.Add(-2 * time.Hour),
						UpdatedAt:       now,
						ArchivedAt:      &archivedAt,
					})
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow SQL %q", sql)
				return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
			}
		},
	}}

	created, err := repo.CreateVPSAsset(context.Background(), vpsassets.CreateInput{
		DisplayName:     " Tokyo Edge ",
		ProviderID:      stringPtr(" pv_001 "),
		ProviderName:    " Hetzner ",
		ProductName:     " CX22 ",
		Country:         " JP ",
		City:            " Tokyo ",
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
		Labels:          []string{" edge ", ""},
	})
	if err != nil {
		t.Fatalf("CreateVPSAsset() error = %v", err)
	}
	if !strings.HasPrefix(created.VPSID, "vps_") {
		t.Fatalf("VPSID = %q, want vps_ prefix", created.VPSID)
	}
	if created.DisplayName != "Tokyo Edge" {
		t.Fatalf("created.DisplayName = %q, want Tokyo Edge", created.DisplayName)
	}
	if len(rowArgs[0]) != 23 {
		t.Fatalf("create args len = %d, want 23", len(rowArgs[0]))
	}
	if rowArgs[0][2] != "pv_001" || rowArgs[0][13] != 22 || rowArgs[0][19] != string(vpsassets.RenewalUnreviewed) {
		t.Fatalf("create normalized args = %#v", rowArgs[0])
	}
	if !strings.Contains(rowCalls[0], "case when $18::text = 'archived' then now() else null end") {
		t.Fatalf("create SQL does not derive archived_at: %q", rowCalls[0])
	}

	list, err := repo.ListVPSAssets(context.Background(), vpsassets.ListFilters{
		ProviderID:      " pv_001 ",
		LifecycleStatus: " active ",
		UsageStatus:     " in_use ",
		RenewalDecision: " keep ",
	})
	if err != nil {
		t.Fatalf("ListVPSAssets() error = %v", err)
	}
	if len(list) != 2 || list[0].VPSID != "vps_001" || list[1].VPSID != "vps_002" {
		t.Fatalf("ListVPSAssets() = %#v, want two records", list)
	}
	if len(queryCalls) != 1 {
		t.Fatalf("query calls = %d, want list query", len(queryCalls))
	}
	for _, snippet := range []string{
		"provider_id = $1",
		"lifecycle_status = $2",
		"usage_status = $3",
		"renewal_decision = $4",
		"order by lower(display_name), vps_id",
	} {
		if !strings.Contains(queryCalls[0], snippet) {
			t.Fatalf("ListVPSAssets SQL missing %q in %q", snippet, queryCalls[0])
		}
	}
	if len(queryArgs[0]) != 4 || queryArgs[0][0] != "pv_001" || queryArgs[0][1] != "active" || queryArgs[0][2] != "in_use" || queryArgs[0][3] != "keep" {
		t.Fatalf("list args = %#v, want normalized filter args", queryArgs[0])
	}

	got, err := repo.GetVPSAsset(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("GetVPSAsset() error = %v", err)
	}
	if got.DisplayName != "Akamai Edge" {
		t.Fatalf("GetVPSAsset().DisplayName = %q, want Akamai Edge", got.DisplayName)
	}

	patched, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{
		DisplayName:     vpsassets.PatchString(" Akamai Edge Archived "),
		ProviderID:      vpsassets.PatchNullableString(nil),
		LifecycleStatus: vpsassets.PatchLifecycle(vpsassets.LifecycleArchived),
		UsageStatus:     vpsassets.PatchUsage(vpsassets.UsageIdle),
		Importance:      vpsassets.PatchString(" critical "),
		Labels:          vpsassets.PatchLabels([]string{" edge ", "backup", "edge"}),
	})
	if err != nil {
		t.Fatalf("PatchVPSAsset() error = %v", err)
	}
	if patched.DisplayName != "Akamai Edge Archived" || patched.ArchivedAt == nil {
		t.Fatalf("patched = %#v, want archived record", patched)
	}
	if len(rowCalls) != 3 {
		t.Fatalf("QueryRow calls = %d, want create/get/patch", len(rowCalls))
	}
	patchArgs := rowArgs[2]
	if len(patchArgs) != 45 {
		t.Fatalf("patch args len = %d, want 45", len(patchArgs))
	}
	if patchArgs[0] != "vps_001" || patchArgs[1] != true || patchArgs[2] != "Akamai Edge Archived" {
		t.Fatalf("patch name args = %#v, want vps id and trimmed name", patchArgs[:3])
	}
	if patchArgs[3] != true || patchArgs[4] != nil {
		t.Fatalf("patch provider args = set:%#v value:%#v, want explicit clear", patchArgs[3], patchArgs[4])
	}
	if patchArgs[25] != false {
		t.Fatalf("patch ssh port set arg = %#v, want false in non-history patch", patchArgs[25])
	}
	if patchArgs[33] != true || patchArgs[34] != string(vpsassets.LifecycleArchived) {
		t.Fatalf("patch lifecycle args = set:%#v value:%#v, want archived", patchArgs[33], patchArgs[34])
	}
	labels, ok := patchArgs[42].([]string)
	if !ok || len(labels) != 2 || labels[0] != "edge" || labels[1] != "backup" {
		t.Fatalf("patch labels arg = %#v, want normalized labels", patchArgs[42])
	}
	for _, snippet := range []string{
		"display_name = case when $2::boolean then $3 else display_name end",
		"provider_id = case when $4::boolean then $5::text else provider_id end",
		"ssh_port = case when $26::boolean then $27::integer else ssh_port end",
		"lifecycle_status = case when $34::boolean then $35 else lifecycle_status end",
		"labels = case when $42::boolean then $43::text[] else labels end",
		"when $34::boolean and $35::text = 'archived' then coalesce(archived_at, now())",
		"when $34::boolean and $35::text <> 'archived' then null",
		"updated_at = now()",
		"where vps_id = $1",
		"returning " + vpsAssetSelectColumns,
	} {
		if !strings.Contains(rowCalls[2], snippet) {
			t.Fatalf("PatchVPSAsset SQL missing %q in %q", snippet, rowCalls[2])
		}
	}
}

func TestPostgresVPSAssetPatchWithoutChangesReturnsExistingAsset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	queryCount := 0
	repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			if strings.Contains(sql, "update vps_assets") {
				t.Fatalf("PatchVPSAsset without changes should not update: %q", sql)
			}
			if len(args) != 1 || args[0] != "vps_001" {
				t.Fatalf("GetVPSAsset args = %#v, want vps id", args)
			}
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Akamai Edge",
					SSHPort:         22,
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now,
					UpdatedAt:       now,
				})
				return nil
			}}
		},
	}}

	record, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{})
	if err != nil {
		t.Fatalf("PatchVPSAsset() error = %v", err)
	}
	if record.VPSID != "vps_001" {
		t.Fatalf("VPSID = %q, want vps_001", record.VPSID)
	}
	if queryCount != 1 {
		t.Fatalf("QueryRow calls = %d, want only get", queryCount)
	}
}

func TestPostgresVPSAssetPatchRecordsRenewalDecisionHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	tx := &fakeVPSAssetTx{}
	var calls []string
	var args [][]any
	tx.queryRow = func(_ context.Context, sql string, callArgs ...any) pgx.Row {
		calls = append(calls, sql)
		args = append(args, append([]any(nil), callArgs...))
		switch {
		case strings.Contains(sql, "for update"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					SSHPort:         22,
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now,
					UpdatedAt:       now,
				})
				return nil
			}}
		case strings.Contains(sql, "update vps_assets"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					SSHPort:         22,
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalCancel,
					Importance:      "normal",
					CreatedAt:       now.Add(-time.Hour),
					UpdatedAt:       now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into renewal_decisions"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "rdec_001"
				*(dest[1].(*string)) = "vps_001"
				fromDecision := "keep"
				*(dest[2].(**string)) = &fromDecision
				*(dest[3].(*string)) = "cancel"
				*(dest[4].(*string)) = "too expensive"
				*(dest[5].(*time.Time)) = now
				*(dest[6].(*time.Time)) = now
				return nil
			}}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresVPSAssetRepository{
		db: fakeVPSAssetDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}

	record, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{
		RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel),
		RenewalReason:   vpsassets.PatchString(" too expensive "),
	})
	if err != nil {
		t.Fatalf("PatchVPSAsset() error = %v", err)
	}
	if record.RenewalDecision != vpsassets.RenewalCancel {
		t.Fatalf("RenewalDecision = %q, want cancel", record.RenewalDecision)
	}
	if !tx.committed || tx.rolledBack == 0 {
		t.Fatalf("transaction committed=%t rollbackCalls=%d, want committed with deferred rollback", tx.committed, tx.rolledBack)
	}
	if len(calls) != 3 {
		t.Fatalf("query row calls = %d, want lock/update/insert", len(calls))
	}
	if !strings.Contains(calls[0], "for update") || !strings.Contains(calls[2], "insert into renewal_decisions") {
		t.Fatalf("calls = %#v, want lock then history insert", calls)
	}
	historyArgs := args[2]
	if len(historyArgs) != 6 || historyArgs[1] != "vps_001" || historyArgs[2] != "keep" || historyArgs[3] != "cancel" || historyArgs[4] != "too expensive" {
		t.Fatalf("history args = %#v, want keep -> cancel with reason", historyArgs)
	}
}

func TestPostgresVPSAssetPatchCancellationDecisionCancelsSingleActiveSubscription(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC))
	tx := &fakeVPSAssetTx{}
	var subscriptionPatchArgs []any
	priceHistoryInserted := false
	tx.query = func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		if !strings.Contains(sql, "from subscriptions") || !strings.Contains(sql, "for update") {
			t.Fatalf("unexpected Query SQL %q", sql)
		}
		if len(args) != 1 || args[0] != "vps_001" {
			t.Fatalf("subscription args = %#v, want vps_001", args)
		}
		return &fakeSubscriptionRows{rows: []fakeSubscriptionScan{{scan: func(dest ...any) error {
			scanSubscriptionRecordDestinations(dest, subscriptions.Record{
				SubscriptionID:     "sub_001",
				VPSID:              "vps_001",
				Price:              120,
				Currency:           "USD",
				BillingCycle:       "annual",
				BillingMonths:      12,
				MonthlyPrice:       10,
				RenewAt:            &renewAt,
				AutoRenew:          true,
				AutoRenewCancelled: false,
				Status:             subscriptions.StatusActive,
				CreatedAt:          now.Add(-time.Hour),
				UpdatedAt:          now.Add(-time.Hour),
			})
			return nil
		}}}}, nil
	}
	tx.queryRow = func(_ context.Context, sql string, callArgs ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "for update") && strings.Contains(sql, "from vps_assets"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					SSHPort:         22,
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now.Add(-time.Hour),
					UpdatedAt:       now.Add(-time.Hour),
				})
				return nil
			}}
		case strings.Contains(sql, "update vps_assets"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					SSHPort:         22,
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalCancel,
					Importance:      "normal",
					CreatedAt:       now.Add(-time.Hour),
					UpdatedAt:       now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into renewal_decisions"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "rdec_001"
				*(dest[1].(*string)) = "vps_001"
				fromDecision := "keep"
				*(dest[2].(**string)) = &fromDecision
				*(dest[3].(*string)) = "cancel"
				*(dest[4].(*string)) = "cancel provider"
				*(dest[5].(*time.Time)) = now
				*(dest[6].(*time.Time)) = now
				return nil
			}}
		case strings.Contains(sql, "update subscriptions"):
			subscriptionPatchArgs = append([]any(nil), callArgs...)
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanSubscriptionRecordDestinations(dest, subscriptions.Record{
					SubscriptionID:     "sub_001",
					VPSID:              "vps_001",
					Price:              120,
					Currency:           "USD",
					BillingCycle:       "annual",
					BillingMonths:      12,
					MonthlyPrice:       10,
					RenewAt:            &renewAt,
					AutoRenew:          false,
					AutoRenewCancelled: true,
					Status:             subscriptions.StatusActive,
					CreatedAt:          now.Add(-time.Hour),
					UpdatedAt:          now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into price_histories"):
			priceHistoryInserted = true
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				priceHistoryID, ok := callArgs[0].(string)
				if !ok || !strings.HasPrefix(priceHistoryID, "ph_") {
					t.Fatalf("price history id arg = %#v, want ph_ prefix", callArgs[0])
				}
				scanPriceHistoryRecordDestinations(dest, priceHistoryFixture(priceHistoryID, now, renewAt, renewAt))
				return nil
			}}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresVPSAssetRepository{
		db: fakeVPSAssetDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}

	record, linkage, err := repo.PatchVPSAssetWithSubscriptionRenewalLinkage(context.Background(), "vps_001", vpsassets.PatchInput{
		RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel),
		RenewalReason:   vpsassets.PatchString(" cancel provider "),
	})
	if err != nil {
		t.Fatalf("PatchVPSAssetWithSubscriptionRenewalLinkage() error = %v", err)
	}
	if record.RenewalDecision != vpsassets.RenewalCancel {
		t.Fatalf("RenewalDecision = %q, want cancel", record.RenewalDecision)
	}
	if linkage.Status != vpsassets.RenewalSubscriptionLinkageUpdated || !linkage.Updated || linkage.SubscriptionID != "sub_001" || linkage.CandidateCount != 1 {
		t.Fatalf("linkage = %#v, want subscription update", linkage)
	}
	if !priceHistoryInserted {
		t.Fatal("expected subscription auto-renew linkage to record price history")
	}
	if len(subscriptionPatchArgs) != 41 || subscriptionPatchArgs[0] != "sub_001" || subscriptionPatchArgs[19] != true || subscriptionPatchArgs[20] != false || subscriptionPatchArgs[21] != true || subscriptionPatchArgs[22] != true || subscriptionPatchArgs[23] != true || subscriptionPatchArgs[24] != "auto_cancelled" {
		t.Fatalf("subscription patch args = %#v, want auto_renew=false and auto_renew_cancelled=true", subscriptionPatchArgs)
	}
	if !tx.committed || tx.rolledBack == 0 {
		t.Fatalf("transaction committed=%t rollbackCalls=%d, want committed with deferred rollback", tx.committed, tx.rolledBack)
	}
}

func TestPostgresVPSAssetPatchCancellationDecisionDoesNotBulkUpdateAmbiguousSubscriptions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		subscriptions []subscriptions.Record
		wantStatus    vpsassets.RenewalSubscriptionLinkageStatus
		wantCount     int
		wantMessage   string
	}{
		{name: "none", subscriptions: nil, wantStatus: vpsassets.RenewalSubscriptionLinkageNoActiveSubscription, wantCount: 0, wantMessage: "缺少生效中的订阅"},
		{name: "inactive", subscriptions: []subscriptions.Record{{SubscriptionID: "sub_expired", VPSID: "vps_001", Status: subscriptions.StatusExpired}}, wantStatus: vpsassets.RenewalSubscriptionLinkageNoActiveSubscription, wantCount: 1, wantMessage: "账单记录已无续费动作"},
		{name: "multiple", subscriptions: []subscriptions.Record{{SubscriptionID: "sub_001", VPSID: "vps_001", Status: subscriptions.StatusActive}, {SubscriptionID: "sub_002", VPSID: "vps_001", Status: subscriptions.StatusActive}, {SubscriptionID: "sub_expired", VPSID: "vps_001", Status: subscriptions.StatusExpired}}, wantStatus: vpsassets.RenewalSubscriptionLinkageMultipleActiveSubscription, wantCount: 2, wantMessage: "多条仍显示自动续费有效"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeVPSAssetTx{}
			tx.query = func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "from subscriptions") {
					t.Fatalf("unexpected Query SQL %q", sql)
				}
				rows := make([]fakeSubscriptionScan, 0, len(tt.subscriptions))
				for _, record := range tt.subscriptions {
					record := record
					if record.Price == 0 {
						record.Price = 10
						record.Currency = "USD"
						record.BillingMonths = 1
						record.MonthlyPrice = 10
					}
					rows = append(rows, fakeSubscriptionScan{scan: func(dest ...any) error {
						scanSubscriptionRecordDestinations(dest, record)
						return nil
					}})
				}
				return &fakeSubscriptionRows{rows: rows}, nil
			}
			tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "for update") && strings.Contains(sql, "from vps_assets"):
					return fakeVPSAssetRow{scan: func(dest ...any) error {
						scanVPSAssetRecordDestinations(dest, vpsassets.Record{VPSID: "vps_001", DisplayName: "Tokyo Edge", SSHPort: 22, LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse, RenewalDecision: vpsassets.RenewalKeep, Importance: "normal", CreatedAt: now, UpdatedAt: now})
						return nil
					}}
				case strings.Contains(sql, "update vps_assets"):
					return fakeVPSAssetRow{scan: func(dest ...any) error {
						scanVPSAssetRecordDestinations(dest, vpsassets.Record{VPSID: "vps_001", DisplayName: "Tokyo Edge", SSHPort: 22, LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse, RenewalDecision: vpsassets.RenewalCancel, Importance: "normal", CreatedAt: now, UpdatedAt: now})
						return nil
					}}
				case strings.Contains(sql, "insert into renewal_decisions"):
					return fakeVPSAssetRow{scan: func(dest ...any) error {
						*(dest[0].(*string)) = "rdec_001"
						*(dest[1].(*string)) = "vps_001"
						fromDecision := "keep"
						*(dest[2].(**string)) = &fromDecision
						*(dest[3].(*string)) = "cancel"
						*(dest[4].(*string)) = ""
						*(dest[5].(*time.Time)) = now
						*(dest[6].(*time.Time)) = now
						return nil
					}}
				case strings.Contains(sql, "update subscriptions"), strings.Contains(sql, "insert into price_histories"):
					t.Fatalf("ambiguous linkage should not update subscriptions or history: %q", sql)
				}
				t.Fatalf("unexpected QueryRow SQL %q", sql)
				return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
			}

			repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}
			_, linkage, err := repo.PatchVPSAssetWithSubscriptionRenewalLinkage(context.Background(), "vps_001", vpsassets.PatchInput{RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalCancel)})
			if err != nil {
				t.Fatalf("PatchVPSAssetWithSubscriptionRenewalLinkage() error = %v", err)
			}
			if linkage.Status != tt.wantStatus || linkage.Updated {
				t.Fatalf("linkage = %#v, want status %q without update", linkage, tt.wantStatus)
			}
			if linkage.CandidateCount != tt.wantCount || !strings.Contains(linkage.Message, tt.wantMessage) {
				t.Fatalf("linkage = %#v, want candidate_count %d and message containing %q", linkage, tt.wantCount, tt.wantMessage)
			}
		})
	}
}

func TestPostgresVPSAssetPatchSkipsRenewalDecisionHistoryWhenDecisionUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	tx := &fakeVPSAssetTx{}
	insertedHistory := false
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "for update"), strings.Contains(sql, "update vps_assets"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					SSHPort:         22,
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now,
					UpdatedAt:       now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into renewal_decisions"):
			insertedHistory = true
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresVPSAssetRepository{
		db: fakeVPSAssetDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}
	if _, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{RenewalDecision: vpsassets.PatchRenewal(vpsassets.RenewalKeep)}); err != nil {
		t.Fatalf("PatchVPSAsset() error = %v", err)
	}
	if insertedHistory {
		t.Fatal("PatchVPSAsset() inserted renewal history for unchanged decision")
	}
}

func TestPostgresVPSAssetPatchRecordsIPAndSpecHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	tx := &fakeVPSAssetTx{}
	var calls []string
	var args [][]any
	tx.queryRow = func(_ context.Context, sql string, callArgs ...any) pgx.Row {
		calls = append(calls, sql)
		args = append(args, append([]any(nil), callArgs...))
		switch {
		case strings.Contains(sql, "for update"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					ProductName:     "CX22",
					IPv4:            "192.0.2.1",
					IPv6:            "2001:db8::1",
					SSHHost:         "old.example",
					SSHPort:         22,
					SSHUser:         "root",
					OSName:          "Debian 12",
					Virtualization:  "kvm",
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now.Add(-time.Hour),
					UpdatedAt:       now.Add(-time.Hour),
				})
				return nil
			}}
		case strings.Contains(sql, "update vps_assets"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					ProductName:     "CPX31",
					IPv4:            "198.51.100.5",
					IPv6:            "2001:db8::5",
					SSHHost:         "new.example",
					SSHPort:         2222,
					SSHUser:         "deploy",
					OSName:          "Ubuntu 24.04",
					Virtualization:  "kvm",
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now.Add(-time.Hour),
					UpdatedAt:       now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into ip_histories"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				ipHistoryID, ok := callArgs[0].(string)
				if !ok || !strings.HasPrefix(ipHistoryID, "iph_") {
					t.Fatalf("ip history id arg = %#v, want iph_ prefix", callArgs[0])
				}
				scanIPHistoryRecordDestinations(dest, "iph_001", now)
				return nil
			}}
		case strings.Contains(sql, "insert into vps_spec_snapshots"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				snapshotID, ok := callArgs[0].(string)
				if !ok || !strings.HasPrefix(snapshotID, "vss_") {
					t.Fatalf("spec snapshot id arg = %#v, want vss_ prefix", callArgs[0])
				}
				scanSpecSnapshotRecordDestinations(dest, "vss_001", now)
				return nil
			}}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresVPSAssetRepository{
		db: fakeVPSAssetDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}
	record, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{
		ProductName: vpsassets.PatchString(" CPX31 "),
		IPv4:        vpsassets.PatchString(" 198.51.100.5 "),
		IPv6:        vpsassets.PatchString(" 2001:db8::5 "),
		SSHHost:     vpsassets.PatchString(" new.example "),
		SSHPort:     vpsassets.PatchInt(2222),
		SSHUser:     vpsassets.PatchString(" deploy "),
		OSName:      vpsassets.PatchString(" Ubuntu 24.04 "),
	})
	if err != nil {
		t.Fatalf("PatchVPSAsset() error = %v", err)
	}
	if record.IPv4 != "198.51.100.5" || record.SSHPort != 2222 {
		t.Fatalf("record = %#v, want patched IP/spec values", record)
	}
	if !tx.committed || tx.rolledBack == 0 {
		t.Fatalf("transaction committed=%t rollbackCalls=%d, want committed with deferred rollback", tx.committed, tx.rolledBack)
	}
	if len(calls) != 4 {
		t.Fatalf("query row calls = %d, want lock/update/ip/spec", len(calls))
	}
	if !strings.Contains(calls[0], "for update") || !strings.Contains(calls[2], "insert into ip_histories") || !strings.Contains(calls[3], "insert into vps_spec_snapshots") {
		t.Fatalf("calls = %#v, want lock/update/ip/spec history", calls)
	}
	ipArgs := args[2]
	if len(ipArgs) != 7 || ipArgs[1] != "vps_001" || ipArgs[2] != "192.0.2.1" || ipArgs[3] != "198.51.100.5" || ipArgs[4] != "2001:db8::1" || ipArgs[5] != "2001:db8::5" {
		t.Fatalf("ip history args = %#v", ipArgs)
	}
	specArgs := args[3]
	if len(specArgs) != 9 || specArgs[1] != "vps_001" || specArgs[2] != "CPX31" || specArgs[4] != 2222 || specArgs[5] != "deploy" || specArgs[6] != "Ubuntu 24.04" {
		t.Fatalf("spec snapshot args = %#v", specArgs)
	}
}

func TestPostgresVPSAssetPatchSkipsIPAndSpecHistoryWhenTrackedFieldsUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	tx := &fakeVPSAssetTx{}
	insertedIPHistory := false
	insertedSpecHistory := false
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "for update"), strings.Contains(sql, "update vps_assets"):
			return fakeVPSAssetRow{scan: func(dest ...any) error {
				scanVPSAssetRecordDestinations(dest, vpsassets.Record{
					VPSID:           "vps_001",
					DisplayName:     "Tokyo Edge",
					ProductName:     "CX22",
					IPv4:            "192.0.2.1",
					IPv6:            "2001:db8::1",
					SSHHost:         "edge.example",
					SSHPort:         22,
					SSHUser:         "root",
					OSName:          "Debian 12",
					Virtualization:  "kvm",
					LifecycleStatus: vpsassets.LifecycleActive,
					UsageStatus:     vpsassets.UsageInUse,
					RenewalDecision: vpsassets.RenewalKeep,
					Importance:      "normal",
					CreatedAt:       now,
					UpdatedAt:       now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into ip_histories"):
			insertedIPHistory = true
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		case strings.Contains(sql, "insert into vps_spec_snapshots"):
			insertedSpecHistory = true
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeVPSAssetRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresVPSAssetRepository{
		db: fakeVPSAssetDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}
	if _, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{IPv4: vpsassets.PatchString("192.0.2.1"), SSHPort: vpsassets.PatchInt(22)}); err != nil {
		t.Fatalf("PatchVPSAsset() error = %v", err)
	}
	if insertedIPHistory || insertedSpecHistory {
		t.Fatalf("insertedIPHistory=%t insertedSpecHistory=%t, want no history for unchanged tracked values", insertedIPHistory, insertedSpecHistory)
	}
}

func TestPostgresVPSAssetMapsNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeVPSAssetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	if _, err := repo.GetVPSAsset(context.Background(), "vps_missing"); !errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
		t.Fatalf("GetVPSAsset() error = %v, want ErrVPSAssetNotFound", err)
	}
	if _, err := repo.PatchVPSAsset(context.Background(), "vps_missing", vpsassets.PatchInput{DisplayName: vpsassets.PatchString("New")}); !errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
		t.Fatalf("PatchVPSAsset() error = %v, want ErrVPSAssetNotFound", err)
	}
}

func TestPostgresVPSAssetRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{}}
	if _, err := repo.CreateVPSAsset(context.Background(), vpsassets.CreateInput{DisplayName: " ", LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("CreateVPSAsset(blank name) error = %v, want ErrInvalidVPSAssetInput", err)
	}
	if _, err := repo.CreateVPSAsset(context.Background(), vpsassets.CreateInput{DisplayName: "Tokyo", LifecycleStatus: "online", UsageStatus: vpsassets.UsageInUse}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("CreateVPSAsset(invalid enum) error = %v, want ErrInvalidVPSAssetInput", err)
	}
	if _, err := repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{SSHPort: vpsassets.PatchInt(0)}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("PatchVPSAsset(invalid ssh port) error = %v, want ErrInvalidVPSAssetInput", err)
	}
	if _, err := repo.ListVPSAssets(context.Background(), vpsassets.ListFilters{LifecycleStatus: "online"}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("ListVPSAssets(invalid filter) error = %v, want ErrInvalidVPSAssetInput", err)
	}
}

func TestPostgresVPSAssetMapsInvalidProviderForeignKey(t *testing.T) {
	t.Parallel()

	fkErr := &pgconn.PgError{Code: "23503", ConstraintName: "vps_assets_provider_id_fkey"}
	repo := &PostgresVPSAssetRepository{db: fakeVPSAssetDB{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if !strings.Contains(sql, "vps_assets") {
				t.Fatalf("unexpected SQL %q", sql)
			}
			return fakeVPSAssetRow{scan: func(dest ...any) error { return fkErr }}
		},
	}}

	_, err := repo.CreateVPSAsset(context.Background(), vpsassets.CreateInput{
		DisplayName:     "Tokyo",
		ProviderID:      stringPtr("pv_missing"),
		LifecycleStatus: vpsassets.LifecycleActive,
		UsageStatus:     vpsassets.UsageInUse,
	})
	if !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("CreateVPSAsset() error = %v, want ErrInvalidVPSAssetInput", err)
	}

	_, err = repo.PatchVPSAsset(context.Background(), "vps_001", vpsassets.PatchInput{
		ProviderID: vpsassets.PatchNullableString(stringPtr("pv_missing")),
	})
	if !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("PatchVPSAsset() error = %v, want ErrInvalidVPSAssetInput", err)
	}
}

type fakeVPSAssetDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeVPSAssetDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeVPSAssetRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeVPSAssetDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeVPSAssetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeVPSAssetTx struct {
	queryRow   func(context.Context, string, ...any) pgx.Row
	query      func(context.Context, string, ...any) (pgx.Rows, error)
	exec       func(context.Context, string, ...any) (pgconn.CommandTag, error)
	committed  bool
	rolledBack int
}

func (f *fakeVPSAssetTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeVPSAssetTx) Commit(context.Context) error {
	f.committed = true
	return nil
}
func (f *fakeVPSAssetTx) Rollback(context.Context) error {
	f.rolledBack++
	return nil
}
func (f *fakeVPSAssetTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeVPSAssetTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeVPSAssetTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (f *fakeVPSAssetTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeVPSAssetTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec != nil {
		return f.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (f *fakeVPSAssetTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query != nil {
		return f.query(ctx, sql, args...)
	}
	return nil, nil
}
func (f *fakeVPSAssetTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return fakeVPSAssetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
}
func (f *fakeVPSAssetTx) Conn() *pgx.Conn { return nil }

type fakeVPSAssetRow struct {
	scan func(dest ...any) error
}

func (r fakeVPSAssetRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeVPSAssetScan struct {
	scan func(dest ...any) error
}

type fakeVPSAssetRows struct {
	rows []fakeVPSAssetScan
	idx  int
	err  error
}

func (f *fakeVPSAssetRows) Close()                                       {}
func (f *fakeVPSAssetRows) Err() error                                   { return f.err }
func (f *fakeVPSAssetRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeVPSAssetRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeVPSAssetRows) RawValues() [][]byte                          { return nil }
func (f *fakeVPSAssetRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeVPSAssetRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeVPSAssetRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeVPSAssetRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanVPSAssetRecordDestinations(dest []any, record vpsassets.Record) {
	*(dest[0].(*string)) = record.VPSID
	*(dest[1].(*string)) = record.DisplayName
	*(dest[2].(**string)) = cloneStringPtr(record.ProviderID)
	*(dest[3].(*string)) = record.ProviderName
	*(dest[4].(*string)) = record.ProductName
	*(dest[5].(*string)) = record.OrderRef
	*(dest[6].(*string)) = record.Country
	*(dest[7].(*string)) = record.Region
	*(dest[8].(*string)) = record.City
	*(dest[9].(*string)) = record.Datacenter
	*(dest[10].(*string)) = record.IPv4
	*(dest[11].(*string)) = record.IPv6
	*(dest[12].(*string)) = record.SSHHost
	*(dest[13].(*int)) = record.SSHPort
	*(dest[14].(*string)) = record.SSHUser
	*(dest[15].(*string)) = record.OSName
	*(dest[16].(*string)) = record.Virtualization
	*(dest[17].(*vpsassets.LifecycleStatus)) = record.LifecycleStatus
	*(dest[18].(*vpsassets.UsageStatus)) = record.UsageStatus
	*(dest[19].(*vpsassets.RenewalDecision)) = record.RenewalDecision
	*(dest[20].(*string)) = record.Importance
	*(dest[21].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[22].(*string)) = record.Note
	*(dest[23].(*int)) = record.ActiveMonitoringInstanceLinkCount
	*(dest[24].(*int)) = record.RunningMonitoringInstanceCount
	*(dest[25].(*int)) = record.RunningTargetCount
	*(dest[26].(*time.Time)) = record.CreatedAt
	*(dest[27].(*time.Time)) = record.UpdatedAt
	*(dest[28].(**time.Time)) = cloneTimePtr(record.ArchivedAt)
}

func scanIPHistoryRecordDestinations(dest []any, id string, now time.Time) {
	*(dest[0].(*string)) = id
	*(dest[1].(*string)) = "vps_001"
	*(dest[2].(*string)) = "192.0.2.1"
	*(dest[3].(*string)) = "198.51.100.5"
	*(dest[4].(*string)) = "2001:db8::1"
	*(dest[5].(*string)) = "2001:db8::5"
	*(dest[6].(*time.Time)) = now
	*(dest[7].(*time.Time)) = now
}

func scanSpecSnapshotRecordDestinations(dest []any, id string, now time.Time) {
	*(dest[0].(*string)) = id
	*(dest[1].(*string)) = "vps_001"
	*(dest[2].(*string)) = "CPX31"
	*(dest[3].(*string)) = "new.example"
	*(dest[4].(*int)) = 2222
	*(dest[5].(*string)) = "deploy"
	*(dest[6].(*string)) = "Ubuntu 24.04"
	*(dest[7].(*string)) = "kvm"
	*(dest[8].(*time.Time)) = now
	*(dest[9].(*time.Time)) = now
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func stringPtr(value string) *string {
	return &value
}
