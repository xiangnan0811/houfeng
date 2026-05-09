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
						SSHPort:         2200,
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
		SSHPort:         vpsassets.PatchInt(2200),
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
	if patchArgs[25] != true || patchArgs[26] != 2200 {
		t.Fatalf("patch ssh port args = set:%#v value:%#v, want 2200", patchArgs[25], patchArgs[26])
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
	*(dest[23].(*time.Time)) = record.CreatedAt
	*(dest[24].(*time.Time)) = record.UpdatedAt
	*(dest[25].(**time.Time)) = cloneTimePtr(record.ArchivedAt)
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
