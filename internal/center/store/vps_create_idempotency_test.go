package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/createidempotency"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/renewals"
)

var errCreateIdempotencyCutPoint = errors.New("create idempotency cut point")

func TestVPSCreateDigestsUseNormalizedScopedRequestIdentity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 8, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	experienceRaw := testExperienceLogInput(now)
	experienceNormalized := renewals.NormalizeCreateExperienceLogInput(experienceRaw)
	experienceEquivalent := experienceNormalized
	experienceEquivalent.VPSID = " vps_001 "
	experienceEquivalent.Summary = " packet loss "
	first, err := experienceLogCreateDigest(renewals.NormalizeCreateExperienceLogInput(experienceRaw))
	if err != nil {
		t.Fatalf("first experience digest error type = %T", err)
	}
	second, err := experienceLogCreateDigest(renewals.NormalizeCreateExperienceLogInput(experienceEquivalent))
	if err != nil {
		t.Fatalf("second experience digest error type = %T", err)
	}
	if first != second {
		t.Fatal("equivalent normalized experience requests produced different digests")
	}
	experienceEquivalent.Details = "changed private detail"
	changed, err := experienceLogCreateDigest(renewals.NormalizeCreateExperienceLogInput(experienceEquivalent))
	if err != nil {
		t.Fatalf("changed experience digest error type = %T", err)
	}
	if changed == first {
		t.Fatal("experience request field change did not change digest")
	}

	serviceRaw := testAssetServiceInput()
	serviceEquivalent := serviceRaw
	serviceEquivalent.VPSID = "vps_001"
	serviceEquivalent.Name = "API"
	serviceEquivalent.Labels = []string{"prod"}
	first, _ = assetServiceCreateDigest(assetservices.NormalizeCreateInput(serviceRaw))
	second, _ = assetServiceCreateDigest(assetservices.NormalizeCreateInput(serviceEquivalent))
	if first != second {
		t.Fatal("equivalent normalized service requests produced different digests")
	}

	domainRaw := testAssetDomainInput()
	domainEquivalent := domainRaw
	domainEquivalent.VPSID = "vps_001"
	domainEquivalent.DomainName = "api.example.com"
	domainEquivalent.Labels = []string{"prod"}
	first, _ = assetDomainCreateDigest(assetdomains.NormalizeCreateInput(domainRaw))
	second, _ = assetDomainCreateDigest(assetdomains.NormalizeCreateInput(domainEquivalent))
	if first != second {
		t.Fatal("equivalent normalized domain requests produced different digests")
	}

	monitoringRaw := testMonitoringInstanceInput()
	monitoringEquivalent := monitoringRaw
	monitoringEquivalent.DisplayName = "Tokyo Edge"
	monitoringEquivalent.Group = "edge"
	monitoringEquivalent.Region = "Tokyo"
	monitoringEquivalent.City = "Tokyo"
	monitoringEquivalent.Provider = "Acme"
	monitoringEquivalent.Labels = []string{"prod"}
	monitoringEquivalent.Note = "private monitoring note"
	firstWire := monitoringinstances.NormalizeLinkedCreateWireIdentity(linkedCreateWireIdentityFromPersistence(monitoringRaw, "private link note"))
	secondWire := monitoringinstances.NormalizeLinkedCreateWireIdentity(linkedCreateWireIdentityFromPersistence(monitoringEquivalent, "private link note"))
	first, _ = linkedMonitoringInstanceCreateDigest("vps_001", firstWire)
	second, _ = linkedMonitoringInstanceCreateDigest("vps_001", secondWire)
	if first != second {
		t.Fatal("equivalent normalized monitoring requests produced different digests")
	}
	changedWire := secondWire
	changedWire.Note = "changed operator note"
	changed, _ = linkedMonitoringInstanceCreateDigest("vps_001", changedWire)
	if changed == first {
		t.Fatal("monitoring wire field change did not change digest")
	}
	otherScope, _ := linkedMonitoringInstanceCreateDigest("vps_002", secondWire)
	if otherScope == first {
		t.Fatal("monitoring digest omitted VPS scope")
	}
}

func TestVPSCreateAdvisoryLockOperationsAreDistinct(t *testing.T) {
	t.Parallel()

	key := "idempotency-key-0001"
	locks := map[string]struct{}{}
	for _, operation := range []string{
		experienceLogCreateOperation,
		assetServiceCreateOperation,
		assetDomainCreateOperation,
		linkedMonitoringInstanceCreateOperation,
	} {
		locks[createidempotency.NamespacedLockKey(operation, key)] = struct{}{}
	}
	if len(locks) != 4 {
		t.Fatalf("operation lock namespaces = %d, want 4", len(locks))
	}
}

func TestDeriveLinkedMonitoringInstanceCreateInputPreservesWireContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		wire         monitoringinstances.LinkedCreateWireIdentity
		defaults     linkedMonitoringInstanceVPSDefaults
		want         monitoringinstances.CreateInput
		wantLinkNote string
	}{
		{
			name: "explicit wire values win",
			wire: monitoringinstances.LinkedCreateWireIdentity{
				DisplayName: " Explicit display ", Group: " Explicit group ", Region: " Explicit region ", City: " Explicit city ",
				Provider: " Explicit provider ", Labels: []string{" explicit ", "explicit"}, Note: " Explicit note ", LinkNote: " Explicit link note ",
			},
			defaults: linkedMonitoringInstanceVPSDefaults{
				DisplayName: "VPS display", Region: "VPS region", Country: "VPS country", City: "VPS city", Datacenter: "VPS datacenter",
				ProviderName: "VPS provider", Labels: []string{"vps"}, Note: "VPS note",
			},
			want: monitoringinstances.CreateInput{
				DisplayName: "Explicit display", Group: "Explicit group", Region: "Explicit region", City: "Explicit city",
				Provider: "Explicit provider", LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment, Labels: []string{"explicit"}, Note: "Explicit note",
			},
			wantLinkNote: "Explicit link note",
		},
		{
			name: "VPS primary defaults",
			wire: monitoringinstances.LinkedCreateWireIdentity{Group: "group"},
			defaults: linkedMonitoringInstanceVPSDefaults{
				DisplayName: "VPS display", Region: "VPS region", Country: "VPS country", City: "VPS city", Datacenter: "VPS datacenter",
				ProviderName: "VPS provider", Labels: []string{"vps"}, Note: "VPS note",
			},
			want: monitoringinstances.CreateInput{
				DisplayName: "VPS display", Group: "group", Region: "VPS region", City: "VPS city", Provider: "VPS provider",
				LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment, Labels: []string{"vps"}, Note: "VPS note",
			},
			wantLinkNote: "created from vps detail",
		},
		{
			name:     "secondary and fixed defaults",
			wire:     monitoringinstances.LinkedCreateWireIdentity{},
			defaults: linkedMonitoringInstanceVPSDefaults{Country: "VPS country", Datacenter: "VPS datacenter"},
			want: monitoringinstances.CreateInput{
				DisplayName: "vps_path", Region: "VPS country", City: "VPS datacenter", Provider: "未关联服务商",
				LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
			},
			wantLinkNote: "created from vps detail",
		},
		{
			name:     "unknown location defaults",
			wire:     monitoringinstances.LinkedCreateWireIdentity{},
			defaults: linkedMonitoringInstanceVPSDefaults{},
			want: monitoringinstances.CreateInput{
				DisplayName: "vps_path", Region: "未确认", City: "未确认", Provider: "未关联服务商",
				LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
			},
			wantLinkNote: "created from vps detail",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wire := monitoringinstances.NormalizeLinkedCreateWireIdentity(tt.wire)
			got, gotLinkNote := deriveLinkedMonitoringInstanceCreateInput("vps_path", wire, tt.defaults)
			got = monitoringinstances.NormalizeCreateInput(got)
			if got.DisplayName != tt.want.DisplayName || got.Group != tt.want.Group || got.Region != tt.want.Region || got.City != tt.want.City || got.Provider != tt.want.Provider || got.LifecycleStatus != tt.want.LifecycleStatus || got.Note != tt.want.Note {
				t.Fatal("derived scalar field contract mismatch")
			}
			if strings.Join(got.Labels, "\x00") != strings.Join(tt.want.Labels, "\x00") {
				t.Fatalf("derived label count = %d, want %d", len(got.Labels), len(tt.want.Labels))
			}
			if gotLinkNote != tt.wantLinkNote {
				t.Fatal("derived link note contract mismatch")
			}
			if err := monitoringinstances.ValidateCreateInput(got); err != nil {
				t.Fatalf("derived create input validation error type = %T", err)
			}
		})
	}
}

func TestCreateExperienceLogIdempotentCreatesAndRecordsReceiptAtomically(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 8, 30, 0, 0, time.UTC)
	input := testExperienceLogInput(now)
	var (
		lockArg       any
		receiptArgs   []any
		resultInserts int
	)
	tx := &fakeCreateIdempotencyTx{}
	tx.exec = func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		switch {
		case strings.Contains(sql, "pg_advisory_xact_lock"):
			lockArg = args[0]
		case strings.Contains(sql, "insert into experience_log_create_idempotency"):
			receiptArgs = append([]any(nil), args...)
		default:
			t.Fatal("unexpected Exec call")
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}
	tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from experience_log_create_idempotency"):
			return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "insert into experience_logs"):
			resultInserts++
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanExperienceLogRecordDestinations(dest, "elog_001", now)
				return nil
			}}
		default:
			t.Fatalf("unexpected QueryRow arg count = %d", len(args))
			return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
		}
	}
	repo := &PostgresRenewalDecisionRepository{
		db: fakeRenewalDecisionDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}

	record, replayed, err := repo.CreateExperienceLogIdempotent(context.Background(), input, "  idempotency-key-0001  ")
	if err != nil {
		t.Fatalf("CreateExperienceLogIdempotent() error = %T", err)
	}
	if replayed {
		t.Fatal("CreateExperienceLogIdempotent() replayed = true on first create")
	}
	if record.ExperienceLogID != "elog_001" || resultInserts != 1 {
		t.Fatalf("experience log ID match = %t, result insert count = %d", record.ExperienceLogID == "elog_001", resultInserts)
	}
	if lockArg != createidempotency.NamespacedLockKey(experienceLogCreateOperation, "idempotency-key-0001") {
		t.Fatal("advisory lock argument did not match the operation namespace")
	}
	if len(receiptArgs) != 3 || receiptArgs[0] != "idempotency-key-0001" || receiptArgs[2] != "elog_001" {
		t.Fatalf("receipt arg count = %d, key/result ID matches = %t/%t", len(receiptArgs), len(receiptArgs) > 0 && receiptArgs[0] == "idempotency-key-0001", len(receiptArgs) > 2 && receiptArgs[2] == "elog_001")
	}
	if digest, ok := receiptArgs[1].(string); !ok || len(digest) != 64 {
		t.Fatalf("receipt digest type/length valid = %t/%t", ok, ok && len(digest) == 64)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
		t.Fatalf("commitCalls=%d rollbackCalls=%d, want 1/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestCreateExperienceLogIdempotentReplaysOriginalAndRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 8, 30, 0, 0, time.UTC)
	input := testExperienceLogInput(now)
	digest, err := experienceLogCreateDigest(renewals.NormalizeCreateExperienceLogInput(input))
	if err != nil {
		t.Fatalf("experienceLogCreateDigest() error = %T", err)
	}

	for _, test := range []struct {
		name         string
		storedDigest string
		wantReplay   bool
		wantErr      error
	}{
		{name: "same digest", storedDigest: digest, wantReplay: true},
		{name: "different digest", storedDigest: strings.Repeat("f", 64), wantErr: createidempotency.ErrIdempotencyKeyReused},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resultInserts := 0
			tx := &fakeCreateIdempotencyTx{}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from experience_log_create_idempotency"):
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						*(dest[0].(*string)) = test.storedDigest
						*(dest[1].(*string)) = "elog_001"
						return nil
					}}
				case strings.Contains(sql, "from experience_logs"):
					if !strings.Contains(sql, "and vps_id = $2") || len(args) != 2 || args[0] != "elog_001" || args[1] != "vps_001" {
						t.Fatalf("experience replay scope SQL/argument contract valid = false")
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanExperienceLogRecordDestinations(dest, "elog_001", now)
						return nil
					}}
				case strings.Contains(sql, "insert into experience_logs"):
					resultInserts++
				}
				return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
			}
			repo := &PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}

			record, replayed, err := repo.CreateExperienceLogIdempotent(context.Background(), input, "idempotency-key-0001")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateExperienceLogIdempotent() error = %T, want %T", err, test.wantErr)
			}
			if replayed != test.wantReplay {
				t.Fatalf("replayed = %t, want %t", replayed, test.wantReplay)
			}
			if test.wantReplay && record.ExperienceLogID != "elog_001" {
				t.Fatalf("replayed experience log ID = %q", record.ExperienceLogID)
			}
			if resultInserts != 0 {
				t.Fatalf("result inserts = %d, want 0", resultInserts)
			}
		})
	}
}

func TestCreateExperienceLogIdempotentFailsClosedAtEveryCutPoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 8, 30, 0, 0, time.UTC)
	input := testExperienceLogInput(now)
	digest, err := experienceLogCreateDigest(renewals.NormalizeCreateExperienceLogInput(input))
	if err != nil {
		t.Fatalf("experienceLogCreateDigest() error = %T", err)
	}

	for _, cutPoint := range []string{"begin", "lock", "lookup", "scan", "insert", "receipt", "commit"} {
		cutPoint := cutPoint
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()

			tx := newExperienceLogCutPointTx(t, cutPoint, digest, now)
			repo := &PostgresRenewalDecisionRepository{
				db: fakeRenewalDecisionDB{},
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					if cutPoint == "begin" {
						return nil, errCreateIdempotencyCutPoint
					}
					return tx, nil
				},
			}

			_, _, err := repo.CreateExperienceLogIdempotent(context.Background(), input, "idempotency-key-secret")
			if err == nil {
				t.Fatal("CreateExperienceLogIdempotent error = nil")
			}
			if strings.Contains(err.Error(), "idempotency-key-secret") || strings.Contains(err.Error(), digest) || strings.Contains(err.Error(), input.Details) {
				t.Fatal("CreateExperienceLogIdempotent leaked private input")
			}
			if cutPoint != "begin" && tx.rollbackCalls != 1 {
				t.Fatalf("rollbackCalls=%d, want 1", tx.rollbackCalls)
			}
			if tx.commitCalls > 1 {
				t.Fatalf("commitCalls=%d, want at most 1", tx.commitCalls)
			}
		})
	}
}

func TestCreateExperienceLogIdempotentValidatesBeforeTransaction(t *testing.T) {
	t.Parallel()

	beginCalls := 0
	repo := &PostgresRenewalDecisionRepository{
		db: fakeRenewalDecisionDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			beginCalls++
			return &fakeCreateIdempotencyTx{}, nil
		},
	}
	validInput := renewals.CreateExperienceLogInput{VPSID: "vps_001", Category: renewals.ExperienceNetwork, Severity: renewals.ExperienceSeverityWarning, Summary: "packet loss"}
	if _, _, err := repo.CreateExperienceLogIdempotent(context.Background(), validInput, "bad key"); !errors.Is(err, createidempotency.ErrInvalidIdempotencyKey) {
		t.Fatalf("invalid key error = %T", err)
	}
	validInput.VPSID = " "
	if _, _, err := repo.CreateExperienceLogIdempotent(context.Background(), validInput, "idempotency-key-0001"); !errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
		t.Fatalf("invalid input error = %T", err)
	}
	if beginCalls != 0 {
		t.Fatalf("beginCalls=%d, want 0", beginCalls)
	}
}

func TestCreateAssetServiceIdempotentCreateReplayAndReuse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	input := testAssetServiceInput()
	digest, err := assetServiceCreateDigest(assetservices.NormalizeCreateInput(input))
	if err != nil {
		t.Fatalf("assetServiceCreateDigest() error = %T", err)
	}
	record := assetservices.Record{ServiceID: "svc_001", VPSID: "vps_001", Name: "API", ServiceType: assetservices.ServiceTypeAPI, Status: assetservices.ServiceStatusActive, Labels: []string{"prod"}, CreatedAt: now, UpdatedAt: now}

	for _, test := range []struct {
		name         string
		storedDigest string
		wantReplay   bool
		wantErr      error
		wantInsert   int
	}{
		{name: "first create", wantInsert: 1},
		{name: "same digest replay", storedDigest: digest, wantReplay: true},
		{name: "different digest reuse", storedDigest: strings.Repeat("f", 64), wantErr: createidempotency.ErrIdempotencyKeyReused},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var lockArg any
			resultInserts := 0
			receiptInserts := 0
			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "pg_advisory_xact_lock") {
					lockArg = args[0]
				} else if strings.Contains(sql, "insert into asset_service_create_idempotency") {
					receiptInserts++
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from asset_service_create_idempotency"):
					if test.storedDigest == "" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						*(dest[0].(*string)) = test.storedDigest
						*(dest[1].(*string)) = record.ServiceID
						return nil
					}}
				case strings.Contains(sql, "insert into asset_services"):
					resultInserts++
					fallthrough
				case strings.Contains(sql, "from asset_services"):
					if strings.Contains(sql, "from asset_services") &&
						(!strings.Contains(sql, "and vps_id = $2") || len(args) != 2 || args[0] != record.ServiceID || args[1] != "vps_001") {
						t.Fatalf("asset service replay scope SQL/argument contract valid = false")
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanAssetServiceRecordDestinations(dest, record)
						return nil
					}}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}

			got, replayed, err := repo.CreateAssetServiceIdempotent(context.Background(), input, "idempotency-key-0001")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateAssetServiceIdempotent() error = %T, want %T", err, test.wantErr)
			}
			if replayed != test.wantReplay || resultInserts != test.wantInsert {
				t.Fatalf("replayed=%t inserts=%d, want %t/%d", replayed, resultInserts, test.wantReplay, test.wantInsert)
			}
			if test.wantErr == nil && got.ServiceID != record.ServiceID {
				t.Fatalf("service ID = %q", got.ServiceID)
			}
			if lockArg != createidempotency.NamespacedLockKey(assetServiceCreateOperation, "idempotency-key-0001") {
				t.Fatal("asset service advisory lock argument did not match the operation namespace")
			}
			if receiptInserts != test.wantInsert {
				t.Fatalf("receipt inserts=%d, want %d", receiptInserts, test.wantInsert)
			}
		})
	}
}

func TestCreateAssetServiceIdempotentFailsClosedAtEveryCutPoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	input := testAssetServiceInput()
	digest, err := assetServiceCreateDigest(assetservices.NormalizeCreateInput(input))
	if err != nil {
		t.Fatalf("assetServiceCreateDigest() error = %T", err)
	}
	record := assetservices.Record{ServiceID: "svc_001", VPSID: "vps_001", Name: "API", ServiceType: assetservices.ServiceTypeAPI, Status: assetservices.ServiceStatusActive, CreatedAt: now, UpdatedAt: now}

	for _, cutPoint := range []string{"begin", "lock", "lookup", "scan", "insert", "receipt", "commit"} {
		cutPoint := cutPoint
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()

			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if (cutPoint == "lock" && strings.Contains(sql, "pg_advisory_xact_lock")) || (cutPoint == "receipt" && strings.Contains(sql, "insert into asset_service_create_idempotency")) {
					return pgconn.CommandTag{}, errCreateIdempotencyCutPoint
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from asset_service_create_idempotency"):
					if cutPoint == "lookup" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					if cutPoint == "scan" {
						return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
							*(dest[0].(*string)) = digest
							*(dest[1].(*string)) = record.ServiceID
							return nil
						}}
					}
					return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
				case strings.Contains(sql, "from asset_services"):
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				case strings.Contains(sql, "insert into asset_services"):
					if cutPoint == "insert" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanAssetServiceRecordDestinations(dest, record)
						return nil
					}}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			if cutPoint == "commit" {
				tx.commit = func(context.Context) error { return errCreateIdempotencyCutPoint }
			}
			repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				if cutPoint == "begin" {
					return nil, errCreateIdempotencyCutPoint
				}
				return tx, nil
			}}

			_, _, err := repo.CreateAssetServiceIdempotent(context.Background(), input, "idempotency-key-secret")
			if err == nil {
				t.Fatal("CreateAssetServiceIdempotent error = nil")
			}
			if cutPoint != "begin" && tx.rollbackCalls != 1 {
				t.Fatalf("rollbackCalls=%d, want 1", tx.rollbackCalls)
			}
		})
	}
}

func TestCreateAssetServiceIdempotentValidatesBeforeTransaction(t *testing.T) {
	t.Parallel()

	beginCalls := 0
	repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		beginCalls++
		return &fakeCreateIdempotencyTx{}, nil
	}}
	if _, _, err := repo.CreateAssetServiceIdempotent(context.Background(), assetservices.CreateInput{VPSID: " ", Name: "API"}, "idempotency-key-0001"); !errors.Is(err, assetservices.ErrInvalidServiceInput) {
		t.Fatalf("invalid input error = %T", err)
	}
	if _, _, err := repo.CreateAssetServiceIdempotent(context.Background(), testAssetServiceInput(), "bad key"); !errors.Is(err, createidempotency.ErrInvalidIdempotencyKey) {
		t.Fatalf("invalid key error = %T", err)
	}
	if beginCalls != 0 {
		t.Fatalf("beginCalls=%d, want 0", beginCalls)
	}
}

func testAssetServiceInput() assetservices.CreateInput {
	return assetservices.CreateInput{
		VPSID:       " vps_001 ",
		Name:        " API ",
		ServiceType: " api ",
		Status:      " active ",
		Labels:      []string{" prod ", "prod"},
		Note:        "private service note",
	}
}

func TestCreateAssetDomainIdempotentCreateReplayAndReuse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 9, 30, 0, 0, time.UTC)
	input := testAssetDomainInput()
	digest, err := assetDomainCreateDigest(assetdomains.NormalizeCreateInput(input))
	if err != nil {
		t.Fatalf("assetDomainCreateDigest() error = %T", err)
	}
	record := assetdomains.Record{DomainID: "dom_001", VPSID: "vps_001", DomainName: "api.example.com", Status: assetdomains.DomainStatusActive, Labels: []string{"prod"}, CreatedAt: now, UpdatedAt: now}

	for _, test := range []struct {
		name         string
		storedDigest string
		wantReplay   bool
		wantErr      error
		wantInsert   int
	}{
		{name: "first create", wantInsert: 1},
		{name: "same digest replay", storedDigest: digest, wantReplay: true},
		{name: "different digest reuse", storedDigest: strings.Repeat("f", 64), wantErr: createidempotency.ErrIdempotencyKeyReused},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var lockArg any
			resultInserts := 0
			receiptInserts := 0
			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "pg_advisory_xact_lock") {
					lockArg = args[0]
				} else if strings.Contains(sql, "insert into asset_domain_create_idempotency") {
					receiptInserts++
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from asset_domain_create_idempotency"):
					if test.storedDigest == "" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						*(dest[0].(*string)) = test.storedDigest
						*(dest[1].(*string)) = record.DomainID
						return nil
					}}
				case strings.Contains(sql, "insert into asset_domains"):
					resultInserts++
					fallthrough
				case strings.Contains(sql, "from asset_domains"):
					if strings.Contains(sql, "from asset_domains") &&
						(!strings.Contains(sql, "and vps_id = $2") || len(args) != 2 || args[0] != record.DomainID || args[1] != "vps_001") {
						t.Fatalf("asset domain replay scope SQL/argument contract valid = false")
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanAssetDomainRecordDestinations(dest, record)
						return nil
					}}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}

			got, replayed, err := repo.CreateAssetDomainIdempotent(context.Background(), input, "idempotency-key-0001")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateAssetDomainIdempotent() error = %T, want %T", err, test.wantErr)
			}
			if replayed != test.wantReplay || resultInserts != test.wantInsert {
				t.Fatalf("replayed=%t inserts=%d, want %t/%d", replayed, resultInserts, test.wantReplay, test.wantInsert)
			}
			if test.wantErr == nil && got.DomainID != record.DomainID {
				t.Fatalf("domain ID = %q", got.DomainID)
			}
			if lockArg != createidempotency.NamespacedLockKey(assetDomainCreateOperation, "idempotency-key-0001") {
				t.Fatal("asset domain advisory lock argument did not match the operation namespace")
			}
			if receiptInserts != test.wantInsert {
				t.Fatalf("receipt inserts=%d, want %d", receiptInserts, test.wantInsert)
			}
		})
	}
}

func TestCreateAssetDomainIdempotentFailsClosedAtEveryCutPoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 9, 30, 0, 0, time.UTC)
	input := testAssetDomainInput()
	digest, err := assetDomainCreateDigest(assetdomains.NormalizeCreateInput(input))
	if err != nil {
		t.Fatalf("assetDomainCreateDigest() error = %T", err)
	}
	record := assetdomains.Record{DomainID: "dom_001", VPSID: "vps_001", DomainName: "api.example.com", Status: assetdomains.DomainStatusActive, CreatedAt: now, UpdatedAt: now}

	for _, cutPoint := range []string{"begin", "lock", "lookup", "scan", "insert", "receipt", "commit"} {
		cutPoint := cutPoint
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()

			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if (cutPoint == "lock" && strings.Contains(sql, "pg_advisory_xact_lock")) || (cutPoint == "receipt" && strings.Contains(sql, "insert into asset_domain_create_idempotency")) {
					return pgconn.CommandTag{}, errCreateIdempotencyCutPoint
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from asset_domain_create_idempotency"):
					if cutPoint == "lookup" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					if cutPoint == "scan" {
						return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
							*(dest[0].(*string)) = digest
							*(dest[1].(*string)) = record.DomainID
							return nil
						}}
					}
					return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
				case strings.Contains(sql, "from asset_domains"):
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				case strings.Contains(sql, "insert into asset_domains"):
					if cutPoint == "insert" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanAssetDomainRecordDestinations(dest, record)
						return nil
					}}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			if cutPoint == "commit" {
				tx.commit = func(context.Context) error { return errCreateIdempotencyCutPoint }
			}
			repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				if cutPoint == "begin" {
					return nil, errCreateIdempotencyCutPoint
				}
				return tx, nil
			}}

			_, _, err := repo.CreateAssetDomainIdempotent(context.Background(), input, "idempotency-key-secret")
			if err == nil {
				t.Fatal("CreateAssetDomainIdempotent error = nil")
			}
			if cutPoint != "begin" && tx.rollbackCalls != 1 {
				t.Fatalf("rollbackCalls=%d, want 1", tx.rollbackCalls)
			}
		})
	}
}

func TestCreateAssetDomainIdempotentValidatesBeforeTransaction(t *testing.T) {
	t.Parallel()

	beginCalls := 0
	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		beginCalls++
		return &fakeCreateIdempotencyTx{}, nil
	}}
	if _, _, err := repo.CreateAssetDomainIdempotent(context.Background(), assetdomains.CreateInput{VPSID: " ", DomainName: "api.example.com"}, "idempotency-key-0001"); !errors.Is(err, assetdomains.ErrInvalidDomainInput) {
		t.Fatalf("invalid input error = %T", err)
	}
	if _, _, err := repo.CreateAssetDomainIdempotent(context.Background(), testAssetDomainInput(), "bad key"); !errors.Is(err, createidempotency.ErrInvalidIdempotencyKey) {
		t.Fatalf("invalid key error = %T", err)
	}
	if beginCalls != 0 {
		t.Fatalf("beginCalls=%d, want 0", beginCalls)
	}
}

func TestCreateAssetDomainIdempotentChecksServiceScopeInsideTransaction(t *testing.T) {
	t.Parallel()

	serviceID := "svc_001"
	input := testAssetDomainInput()
	input.ServiceID = &serviceID
	tx := &fakeCreateIdempotencyTx{}
	tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from asset_domain_create_idempotency"):
			return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "from asset_services"):
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		default:
			t.Fatal("unexpected QueryRow call")
			return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
		}
	}
	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{}, beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}

	_, _, err := repo.CreateAssetDomainIdempotent(context.Background(), input, "idempotency-key-0001")
	if !errors.Is(err, assetdomains.ErrDomainServiceNotFound) {
		t.Fatalf("CreateAssetDomainIdempotent() error = %T, want ErrDomainServiceNotFound", err)
	}
	if tx.rollbackCalls != 1 || tx.commitCalls != 0 {
		t.Fatalf("commitCalls=%d rollbackCalls=%d, want 0/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func testAssetDomainInput() assetdomains.CreateInput {
	return assetdomains.CreateInput{
		VPSID:      " vps_001 ",
		DomainName: " API.EXAMPLE.COM. ",
		Status:     " active ",
		Labels:     []string{" prod ", "prod"},
		Note:       "private domain note",
	}
}

func TestCreateLinkedMonitoringInstanceIdempotentCreateReplayAndReuse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	input := testMonitoringInstanceInput()
	linkNote := " private link note "
	wireIdentity := monitoringinstances.NormalizeLinkedCreateWireIdentity(linkedCreateWireIdentityFromPersistence(input, linkNote))
	digest, err := linkedMonitoringInstanceCreateDigest("vps_001", wireIdentity)
	if err != nil {
		t.Fatalf("linkedMonitoringInstanceCreateDigest() error = %T", err)
	}
	monitoringRecord := testMonitoringInstanceRecord(now)
	linkRecord := assetlinks.Record{LinkID: "vnl_001", VPSID: "vps_001", MonitoringInstanceID: monitoringRecord.MonitoringInstanceID, LinkedAt: now, Note: strings.TrimSpace(linkNote)}

	for _, test := range []struct {
		name         string
		storedDigest string
		vpsMissing   bool
		activeLinks  int
		wantReplay   bool
		wantErr      error
		wantInsert   int
	}{
		{name: "first create", wantInsert: 1},
		{name: "same digest replay", storedDigest: digest, wantReplay: true},
		{name: "different digest reuse", storedDigest: strings.Repeat("f", 64), wantErr: createidempotency.ErrIdempotencyKeyReused},
		{name: "missing vps", vpsMissing: true, wantErr: assetlinks.ErrVPSMonitoringInstanceLinkNotFound},
		{name: "active link conflict", activeLinks: 1, wantErr: assetlinks.ErrVPSActiveMonitoringInstanceExists},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var lockArg any
			monitoringInserts := 0
			linkInserts := 0
			receiptInserts := 0
			receiptLookedUp := false
			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
				if strings.Contains(sql, "pg_advisory_xact_lock") {
					lockArg = args[0]
				} else if strings.Contains(sql, "insert into vps_monitoring_instance_create_idempotency") {
					receiptInserts++
					if len(args) != 4 || args[2] != monitoringRecord.MonitoringInstanceID || args[3] != linkRecord.LinkID {
						t.Fatalf("receipt arg count = %d, monitoring/link ID matches = %t/%t", len(args), len(args) > 2 && args[2] == monitoringRecord.MonitoringInstanceID, len(args) > 3 && args[3] == linkRecord.LinkID)
					}
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			tx.queryRow = func(_ context.Context, sql string, args ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from vps_monitoring_instance_create_idempotency"):
					receiptLookedUp = true
					if test.storedDigest == "" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						*(dest[0].(*string)) = test.storedDigest
						*(dest[1].(*string)) = monitoringRecord.MonitoringInstanceID
						*(dest[2].(*string)) = linkRecord.LinkID
						return nil
					}}
				case strings.Contains(sql, "from vps_assets") && strings.Contains(sql, "for update"):
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						if !receiptLookedUp {
							t.Fatal("locked VPS defaults queried before receipt lookup")
						}
						if test.vpsMissing {
							return pgx.ErrNoRows
						}
						if len(dest) != 9 {
							t.Fatalf("vps default scan destination count = %d, want 9", len(dest))
						}
						*(dest[0].(*string)) = "vps_001"
						*(dest[1].(*string)) = "Tokyo Edge"
						*(dest[2].(*string)) = "Tokyo"
						*(dest[3].(*string)) = "Japan"
						*(dest[4].(*string)) = "Tokyo"
						*(dest[5].(*string)) = "TYO-1"
						*(dest[6].(*string)) = "Acme"
						*(dest[7].(*[]string)) = []string{"prod"}
						*(dest[8].(*string)) = "asset note"
						return nil
					}}
				case strings.Contains(sql, "select count(*)") && strings.Contains(sql, "vps_monitoring_instance_links"):
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						*(dest[0].(*int)) = test.activeLinks
						return nil
					}}
				case strings.Contains(sql, "insert into monitoring_instances"):
					monitoringInserts++
					fallthrough
				case strings.Contains(sql, "from monitoring_instances"):
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanMonitoringInstanceRecordDestinations(dest, monitoringRecord)
						return nil
					}}
				case strings.Contains(sql, "insert into vps_monitoring_instance_links"):
					linkInserts++
					fallthrough
				case strings.Contains(sql, "from vps_monitoring_instance_links"):
					if strings.Contains(sql, "from vps_monitoring_instance_links") &&
						(!strings.Contains(sql, "and vps_id = $2") ||
							!strings.Contains(sql, "and monitoring_instance_id = $3") ||
							len(args) != 3 ||
							args[0] != linkRecord.LinkID ||
							args[1] != "vps_001" ||
							args[2] != monitoringRecord.MonitoringInstanceID) {
						t.Fatalf("monitoring replay link scope/pair SQL argument contract valid = false")
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanVPSMonitoringInstanceLinkRecordDestinations(dest, linkRecord)
						return nil
					}}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

			gotMonitoring, gotLink, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(
				context.Background(),
				" vps_001 ",
				linkedCreateWireIdentityFromPersistence(input, linkNote),
				"idempotency-key-0001",
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateLinkedMonitoringInstanceIdempotent() error = %T, want %T", err, test.wantErr)
			}
			if replayed != test.wantReplay || monitoringInserts != test.wantInsert || linkInserts != test.wantInsert || receiptInserts != test.wantInsert {
				t.Fatalf("replayed=%t monitoring/link/receipt=%d/%d/%d", replayed, monitoringInserts, linkInserts, receiptInserts)
			}
			if test.wantErr == nil && (gotMonitoring.MonitoringInstanceID != monitoringRecord.MonitoringInstanceID || gotLink.LinkID != linkRecord.LinkID) {
				t.Fatalf("monitoring/link ID matches = %t/%t", gotMonitoring.MonitoringInstanceID == monitoringRecord.MonitoringInstanceID, gotLink.LinkID == linkRecord.LinkID)
			}
			if lockArg != createidempotency.NamespacedLockKey(linkedMonitoringInstanceCreateOperation, "idempotency-key-0001") {
				t.Fatal("linked monitoring advisory lock argument did not match the operation namespace")
			}
		})
	}
}

func TestCreateLinkedMonitoringInstanceIdempotentReplayIgnoresChangedDerivedPersistenceDefaults(t *testing.T) {
	t.Parallel()

	firstWireIdentity := monitoringinstances.LinkedCreateWireIdentity{
		DisplayName: "  ",
		Group:       " edge ",
		Region:      " ",
		City:        " ",
		Provider:    " ",
		Labels:      []string{" ", ""},
		Note:        " ",
		LinkNote:    " ",
	}
	secondWireIdentity := firstWireIdentity
	secondWireIdentity.Group = "edge"
	secondWireIdentity.Labels = nil
	secondWireIdentity.Note = ""
	secondWireIdentity.LinkNote = ""
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	originalMonitoring := testMonitoringInstanceRecord(now)
	originalLink := assetlinks.Record{
		LinkID:               "vnl_001",
		VPSID:                "vps_001",
		MonitoringInstanceID: originalMonitoring.MonitoringInstanceID,
		LinkedAt:             now,
		Note:                 "created from vps detail",
	}
	var storedDigest string
	monitoringInserts := 0
	linkInserts := 0
	receiptInserts := 0
	defaultLookups := 0

	firstTx := &fakeCreateIdempotencyTx{}
	firstTx.exec = func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "insert into vps_monitoring_instance_create_idempotency") {
			receiptInserts++
			if len(args) != 4 {
				t.Fatalf("receipt arg count = %d, want 4", len(args))
			}
			var ok bool
			storedDigest, ok = args[1].(string)
			if !ok {
				t.Fatalf("receipt digest type valid = %t", ok)
			}
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}
	firstTx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from vps_monitoring_instance_create_idempotency"):
			return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "from vps_assets") && strings.Contains(sql, "for update"):
			defaultLookups++
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanLinkedMonitoringVPSDefaultDestinations(dest, linkedMonitoringInstanceVPSDefaults{
					VPSID:        "vps_001",
					DisplayName:  "Tokyo Edge",
					Region:       "Tokyo",
					Country:      "Japan",
					City:         "Tokyo",
					Datacenter:   "TYO-1",
					ProviderName: "Acme",
					Labels:       []string{"prod"},
					Note:         "private monitoring note",
				})
				return nil
			}}
		case strings.Contains(sql, "select count(*)") && strings.Contains(sql, "vps_monitoring_instance_links"):
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				*(dest[0].(*int)) = 0
				return nil
			}}
		case strings.Contains(sql, "insert into monitoring_instances"):
			monitoringInserts++
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, originalMonitoring)
				return nil
			}}
		case strings.Contains(sql, "insert into vps_monitoring_instance_links"):
			linkInserts++
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanVPSMonitoringInstanceLinkRecordDestinations(dest, originalLink)
				return nil
			}}
		default:
			t.Fatal("unexpected QueryRow call during first create")
			return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
		}
	}

	secondTx := &fakeCreateIdempotencyTx{}
	secondTx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from vps_monitoring_instance_create_idempotency"):
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				*(dest[0].(*string)) = storedDigest
				*(dest[1].(*string)) = originalMonitoring.MonitoringInstanceID
				*(dest[2].(*string)) = originalLink.LinkID
				return nil
			}}
		case strings.Contains(sql, "from monitoring_instances"):
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, originalMonitoring)
				return nil
			}}
		case strings.Contains(sql, "from vps_monitoring_instance_links"):
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanVPSMonitoringInstanceLinkRecordDestinations(dest, originalLink)
				return nil
			}}
		default:
			t.Fatal("unexpected QueryRow call during replay")
			return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
		}
	}

	txs := []pgx.Tx{firstTx, secondTx}
	beginCalls := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		if beginCalls >= len(txs) {
			return nil, errCreateIdempotencyCutPoint
		}
		tx := txs[beginCalls]
		beginCalls++
		return tx, nil
	}}}

	firstMonitoring, firstLink, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(
		context.Background(), " vps_001 ", firstWireIdentity, "idempotency-key-0001",
	)
	if err != nil {
		t.Fatalf("first create error type = %T", err)
	}
	if replayed {
		t.Fatal("first create unexpectedly replayed")
	}
	secondMonitoring, secondLink, replayed, err := repo.CreateLinkedMonitoringInstanceIdempotent(
		context.Background(), "vps_001", secondWireIdentity, "idempotency-key-0001",
	)
	if err != nil {
		t.Fatalf("replay error type = %T", err)
	}
	if !replayed {
		t.Fatal("same wire identity did not replay")
	}
	if firstMonitoring.MonitoringInstanceID != originalMonitoring.MonitoringInstanceID || secondMonitoring.MonitoringInstanceID != originalMonitoring.MonitoringInstanceID {
		t.Fatalf("monitoring replay IDs differ from original ID %q", originalMonitoring.MonitoringInstanceID)
	}
	if firstLink.LinkID != originalLink.LinkID || secondLink.LinkID != originalLink.LinkID {
		t.Fatalf("link replay IDs differ from original ID %q", originalLink.LinkID)
	}
	if monitoringInserts != 1 || linkInserts != 1 || receiptInserts != 1 {
		t.Fatalf("monitoring/link/receipt insert counts = %d/%d/%d, want 1/1/1", monitoringInserts, linkInserts, receiptInserts)
	}
	if defaultLookups != 1 {
		t.Fatalf("transactional VPS default lookup count = %d, want 1", defaultLookups)
	}
	if beginCalls != 2 || firstTx.commitCalls != 1 || secondTx.commitCalls != 1 {
		t.Fatalf("begin/first commit/replay commit counts = %d/%d/%d, want 2/1/1", beginCalls, firstTx.commitCalls, secondTx.commitCalls)
	}
}

func TestCreateLinkedMonitoringInstanceIdempotentRejectsInvalidDerivedMetadataBeforeActiveLinkQuery(t *testing.T) {
	t.Parallel()

	tooManyLabels := make([]string, monitoringinstances.LinkedCreateMaxLabelCount+1)
	for index := range tooManyLabels {
		tooManyLabels[index] = fmt.Sprintf("private-derived-label-%02d", index)
	}
	tests := []struct {
		name   string
		labels []string
		note   string
	}{
		{name: "too many VPS labels", labels: tooManyLabels},
		{name: "VPS label exceeds rune limit", labels: []string{"private-derived-" + strings.Repeat("界", monitoringinstances.LinkedCreateMaxLabelRunes+1)}},
		{name: "VPS note exceeds rune limit", note: "private-derived-" + strings.Repeat("界", monitoringinstances.LinkedCreateMaxNoteRunes+1)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				receiptLookups       int
				defaultLookups       int
				activeLinkQueryCount int
				insertQueryCount     int
			)
			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				switch {
				case strings.Contains(sql, "pg_advisory_xact_lock"):
					return pgconn.NewCommandTag("SELECT 1"), nil
				case strings.Contains(sql, "insert"):
					insertQueryCount++
					return pgconn.NewCommandTag("INSERT 1"), nil
				default:
					t.Fatal("unexpected Exec call")
					return pgconn.CommandTag{}, errCreateIdempotencyCutPoint
				}
			}
			tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from vps_monitoring_instance_create_idempotency"):
					receiptLookups++
					return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
				case strings.Contains(sql, "from vps_assets") && strings.Contains(sql, "for update"):
					defaultLookups++
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanLinkedMonitoringVPSDefaultDestinations(dest, linkedMonitoringInstanceVPSDefaults{
							VPSID:        "vps_001",
							DisplayName:  "Tokyo Edge",
							Region:       "Tokyo",
							City:         "Tokyo",
							ProviderName: "Acme",
							Labels:       tt.labels,
							Note:         tt.note,
						})
						return nil
					}}
				case strings.Contains(sql, "select count(*)") && strings.Contains(sql, "vps_monitoring_instance_links"):
					activeLinkQueryCount++
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				case strings.Contains(sql, "insert into monitoring_instances"), strings.Contains(sql, "insert into vps_monitoring_instance_links"):
					insertQueryCount++
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				return tx, nil
			}}}

			_, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(
				context.Background(),
				"vps_001",
				monitoringinstances.LinkedCreateWireIdentity{},
				"idempotency-key-0001",
			)
			if err != nil {
				for _, label := range tt.labels {
					if label != "" && strings.Contains(err.Error(), label) {
						t.Fatal("derived label leaked through validation error")
					}
				}
				if tt.note != "" && strings.Contains(err.Error(), tt.note) {
					t.Fatal("derived note leaked through validation error")
				}
			}
			if !errors.Is(err, monitoringinstances.ErrInvalidCreateInput) || receiptLookups != 1 || defaultLookups != 1 || activeLinkQueryCount != 0 || insertQueryCount != 0 || tx.rollbackCalls != 1 || tx.commitCalls != 0 {
				t.Fatalf(
					"invalid class/receipt/default/active/insert/rollback/commit = %t/%d/%d/%d/%d/%d/%d",
					errors.Is(err, monitoringinstances.ErrInvalidCreateInput), receiptLookups, defaultLookups, activeLinkQueryCount, insertQueryCount, tx.rollbackCalls, tx.commitCalls,
				)
			}
		})
	}
}

func TestCreateLinkedMonitoringInstanceIdempotentFailsClosedAtEveryCutPoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	input := testMonitoringInstanceInput()
	linkNote := "private link note"
	wireIdentity := monitoringinstances.NormalizeLinkedCreateWireIdentity(linkedCreateWireIdentityFromPersistence(input, linkNote))
	digest, err := linkedMonitoringInstanceCreateDigest("vps_001", wireIdentity)
	if err != nil {
		t.Fatalf("linkedMonitoringInstanceCreateDigest() error = %T", err)
	}
	monitoringRecord := testMonitoringInstanceRecord(now)
	linkRecord := assetlinks.Record{LinkID: "vnl_001", VPSID: "vps_001", MonitoringInstanceID: monitoringRecord.MonitoringInstanceID, LinkedAt: now, Note: linkNote}

	cutPoints := []string{"begin", "lock", "lookup", "monitoring replay scan", "link replay scan", "vps guard", "active link guard", "monitoring insert", "link insert", "receipt", "commit"}
	for _, cutPoint := range cutPoints {
		cutPoint := cutPoint
		t.Run(cutPoint, func(t *testing.T) {
			t.Parallel()

			tx := &fakeCreateIdempotencyTx{}
			tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
				if (cutPoint == "lock" && strings.Contains(sql, "pg_advisory_xact_lock")) || (cutPoint == "receipt" && strings.Contains(sql, "insert into vps_monitoring_instance_create_idempotency")) {
					return pgconn.CommandTag{}, errCreateIdempotencyCutPoint
				}
				return pgconn.NewCommandTag("INSERT 1"), nil
			}
			tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
				switch {
				case strings.Contains(sql, "from vps_monitoring_instance_create_idempotency"):
					if cutPoint == "lookup" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					if cutPoint == "monitoring replay scan" || cutPoint == "link replay scan" {
						return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
							*(dest[0].(*string)) = digest
							*(dest[1].(*string)) = monitoringRecord.MonitoringInstanceID
							*(dest[2].(*string)) = linkRecord.LinkID
							return nil
						}}
					}
					return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
				case strings.Contains(sql, "from vps_assets") && strings.Contains(sql, "for update"):
					if cutPoint == "vps guard" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanLinkedMonitoringVPSDefaultDestinations(dest, linkedMonitoringInstanceVPSDefaults{
							VPSID:        "vps_001",
							DisplayName:  "Tokyo Edge",
							Region:       "Tokyo",
							City:         "Tokyo",
							ProviderName: "Acme",
						})
						return nil
					}}
				case strings.Contains(sql, "select count(*)") && strings.Contains(sql, "vps_monitoring_instance_links"):
					if cutPoint == "active link guard" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						*(dest[0].(*int)) = 0
						return nil
					}}
				case strings.Contains(sql, "insert into monitoring_instances"):
					if cutPoint == "monitoring insert" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanMonitoringInstanceRecordDestinations(dest, monitoringRecord)
						return nil
					}}
				case strings.Contains(sql, "from monitoring_instances"):
					if cutPoint == "monitoring replay scan" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanMonitoringInstanceRecordDestinations(dest, monitoringRecord)
						return nil
					}}
				case strings.Contains(sql, "insert into vps_monitoring_instance_links"):
					if cutPoint == "link insert" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanVPSMonitoringInstanceLinkRecordDestinations(dest, linkRecord)
						return nil
					}}
				case strings.Contains(sql, "from vps_monitoring_instance_links"):
					if cutPoint == "link replay scan" {
						return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
					}
					return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
						scanVPSMonitoringInstanceLinkRecordDestinations(dest, linkRecord)
						return nil
					}}
				default:
					t.Fatal("unexpected QueryRow call")
					return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
				}
			}
			if cutPoint == "commit" {
				tx.commit = func(context.Context) error { return errCreateIdempotencyCutPoint }
			}
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				if cutPoint == "begin" {
					return nil, errCreateIdempotencyCutPoint
				}
				return tx, nil
			}}}

			_, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(
				context.Background(),
				"vps_001",
				linkedCreateWireIdentityFromPersistence(input, linkNote),
				"idempotency-key-secret",
			)
			if err == nil {
				t.Fatal("CreateLinkedMonitoringInstanceIdempotent error = nil")
			}
			if cutPoint != "begin" && tx.rollbackCalls != 1 {
				t.Fatalf("rollbackCalls=%d, want 1", tx.rollbackCalls)
			}
		})
	}
}

func TestCreateLinkedMonitoringInstanceIdempotentValidatesBeforeTransaction(t *testing.T) {
	t.Parallel()

	beginCalls := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		beginCalls++
		return &fakeCreateIdempotencyTx{}, nil
	}}}
	if _, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(context.Background(), " ", linkedCreateWireIdentityFromPersistence(testMonitoringInstanceInput(), "note"), "idempotency-key-0001"); !errors.Is(err, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput) {
		t.Fatalf("invalid vps error type = %T", err)
	}
	invalidWire := linkedCreateWireIdentityFromPersistence(testMonitoringInstanceInput(), "note")
	invalidWire.Labels = make([]string, monitoringinstances.LinkedCreateMaxLabelCount+1)
	for index := range invalidWire.Labels {
		invalidWire.Labels[index] = fmt.Sprintf("label-%d", index)
	}
	if _, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(context.Background(), "vps_001", invalidWire, "idempotency-key-0001"); !errors.Is(err, monitoringinstances.ErrInvalidCreateInput) {
		t.Fatalf("invalid wire identity error type = %T", err)
	}
	if _, _, _, err := repo.CreateLinkedMonitoringInstanceIdempotent(context.Background(), "vps_001", linkedCreateWireIdentityFromPersistence(testMonitoringInstanceInput(), "note"), "bad key"); !errors.Is(err, createidempotency.ErrInvalidIdempotencyKey) {
		t.Fatalf("invalid key error type = %T", err)
	}
	if beginCalls != 0 {
		t.Fatalf("beginCalls=%d, want 0", beginCalls)
	}
}

func testMonitoringInstanceInput() monitoringinstances.CreateInput {
	return monitoringinstances.CreateInput{
		DisplayName:     " Tokyo Edge ",
		Group:           " edge ",
		Region:          " Tokyo ",
		City:            " Tokyo ",
		Provider:        " Acme ",
		LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
		Labels:          []string{" prod ", "prod"},
		Note:            " private monitoring note ",
	}
}

func scanLinkedMonitoringVPSDefaultDestinations(dest []any, defaults linkedMonitoringInstanceVPSDefaults) {
	*(dest[0].(*string)) = defaults.VPSID
	*(dest[1].(*string)) = defaults.DisplayName
	*(dest[2].(*string)) = defaults.Region
	*(dest[3].(*string)) = defaults.Country
	*(dest[4].(*string)) = defaults.City
	*(dest[5].(*string)) = defaults.Datacenter
	*(dest[6].(*string)) = defaults.ProviderName
	*(dest[7].(*[]string)) = defaults.Labels
	*(dest[8].(*string)) = defaults.Note
}

func linkedCreateWireIdentityFromPersistence(input monitoringinstances.CreateInput, linkNote string) monitoringinstances.LinkedCreateWireIdentity {
	return monitoringinstances.LinkedCreateWireIdentity{
		DisplayName: input.DisplayName,
		Group:       input.Group,
		Region:      input.Region,
		City:        input.City,
		Provider:    input.Provider,
		Labels:      input.Labels,
		Note:        input.Note,
		LinkNote:    linkNote,
	}
}

func testMonitoringInstanceRecord(now time.Time) monitoringinstances.Record {
	return monitoringinstances.Record{
		MonitoringInstanceID: "mi_001",
		DisplayName:          "Tokyo Edge",
		Group:                "edge",
		Region:               "Tokyo",
		City:                 "Tokyo",
		Provider:             "Acme",
		LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
		MonitoringStatus:     monitoringinstances.MonitoringEnabled,
		BindingStatus:        monitoringinstances.BindingUnbound,
		Labels:               []string{"prod"},
		Note:                 "private monitoring note",
		CurrentHealthStatus:  monitoringinstances.HealthNormal,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func testExperienceLogInput(now time.Time) renewals.CreateExperienceLogInput {
	return renewals.CreateExperienceLogInput{
		VPSID:      " vps_001 ",
		Category:   " network ",
		Severity:   " warning ",
		Summary:    " packet loss ",
		Details:    "raw details must stay private",
		OccurredAt: &now,
	}
}

func newExperienceLogCutPointTx(t *testing.T, cutPoint, digest string, now time.Time) *fakeCreateIdempotencyTx {
	t.Helper()

	tx := &fakeCreateIdempotencyTx{}
	tx.exec = func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "pg_advisory_xact_lock") && cutPoint == "lock" {
			return pgconn.CommandTag{}, errCreateIdempotencyCutPoint
		}
		if strings.Contains(sql, "insert into experience_log_create_idempotency") && cutPoint == "receipt" {
			return pgconn.CommandTag{}, errCreateIdempotencyCutPoint
		}
		return pgconn.NewCommandTag("INSERT 1"), nil
	}
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from experience_log_create_idempotency"):
			if cutPoint == "lookup" {
				return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
			}
			if cutPoint == "scan" {
				return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
					*(dest[0].(*string)) = digest
					*(dest[1].(*string)) = "elog_001"
					return nil
				}}
			}
			return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
		case strings.Contains(sql, "from experience_logs"):
			return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
		case strings.Contains(sql, "insert into experience_logs"):
			if cutPoint == "insert" {
				return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
			}
			return fakeCreateIdempotencyRow{scan: func(dest ...any) error {
				scanExperienceLogRecordDestinations(dest, "elog_001", now)
				return nil
			}}
		default:
			t.Fatal("unexpected QueryRow call")
			return fakeCreateIdempotencyRow{scan: func(...any) error { return errCreateIdempotencyCutPoint }}
		}
	}
	if cutPoint == "commit" {
		tx.commit = func(context.Context) error { return errCreateIdempotencyCutPoint }
	}
	return tx
}

type fakeCreateIdempotencyRow struct {
	scan func(...any) error
}

func (r fakeCreateIdempotencyRow) Scan(dest ...any) error { return r.scan(dest...) }

type fakeCreateIdempotencyTx struct {
	queryRow      func(context.Context, string, ...any) pgx.Row
	exec          func(context.Context, string, ...any) (pgconn.CommandTag, error)
	commit        func(context.Context) error
	rollback      func(context.Context) error
	commitCalls   int
	rollbackCalls int
}

func (f *fakeCreateIdempotencyTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeCreateIdempotencyTx) Commit(ctx context.Context) error {
	f.commitCalls++
	if f.commit != nil {
		return f.commit(ctx)
	}
	return nil
}
func (f *fakeCreateIdempotencyTx) Rollback(ctx context.Context) error {
	f.rollbackCalls++
	if f.rollback != nil {
		return f.rollback(ctx)
	}
	return nil
}
func (f *fakeCreateIdempotencyTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeCreateIdempotencyTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f *fakeCreateIdempotencyTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f *fakeCreateIdempotencyTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeCreateIdempotencyTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec != nil {
		return f.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 1"), nil
}
func (f *fakeCreateIdempotencyTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}
func (f *fakeCreateIdempotencyTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return fakeCreateIdempotencyRow{scan: func(...any) error { return pgx.ErrNoRows }}
}
func (f *fakeCreateIdempotencyTx) Conn() *pgx.Conn { return nil }
