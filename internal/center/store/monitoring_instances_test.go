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

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
)

func TestBindingTransitionStoresPendingFingerprintMetadataOnCollision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 7, 30, 0, 0, time.UTC)

	record := resolveEnrollmentBindingTransition(monitoringinstances.Record{
		BindingStatus:      monitoringinstances.BindingBound,
		BindingFingerprint: "fp-old",
	}, "fp-new", now)

	if record.BindingStatus != monitoringinstances.BindingPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, monitoringinstances.BindingPendingConfirmation)
	}
	if record.BindingFingerprint != "fp-old" {
		t.Fatalf("BindingFingerprint = %q, want %q", record.BindingFingerprint, "fp-old")
	}
	if record.PendingBindingFingerprint != "fp-new" {
		t.Fatalf("PendingBindingFingerprint = %q, want %q", record.PendingBindingFingerprint, "fp-new")
	}
	if record.PendingBindingFirstSeenAt == nil || !record.PendingBindingFirstSeenAt.Equal(now) {
		t.Fatalf("PendingBindingFirstSeenAt = %v, want %s", record.PendingBindingFirstSeenAt, now.Format(time.RFC3339))
	}
	if record.PendingBindingLastSeenAt == nil || !record.PendingBindingLastSeenAt.Equal(now) {
		t.Fatalf("PendingBindingLastSeenAt = %v, want %s", record.PendingBindingLastSeenAt, now.Format(time.RFC3339))
	}
	if record.PendingBindingAttemptCount != 1 {
		t.Fatalf("PendingBindingAttemptCount = %d, want 1", record.PendingBindingAttemptCount)
	}
}

func TestBindingTransitionRefreshesPendingAttemptMetadata(t *testing.T) {
	t.Parallel()

	firstSeenAt := time.Date(2026, time.April, 26, 7, 0, 0, 0, time.UTC)
	now := firstSeenAt.Add(30 * time.Minute)

	record := resolveEnrollmentBindingTransition(monitoringinstances.Record{
		BindingStatus:              monitoringinstances.BindingPendingConfirmation,
		BindingFingerprint:         "fp-active",
		PendingBindingFingerprint:  "fp-pending",
		PendingBindingFirstSeenAt:  &firstSeenAt,
		PendingBindingLastSeenAt:   &firstSeenAt,
		PendingBindingAttemptCount: 2,
	}, "fp-pending", now)

	if record.BindingStatus != monitoringinstances.BindingPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, monitoringinstances.BindingPendingConfirmation)
	}
	if record.PendingBindingFingerprint != "fp-pending" {
		t.Fatalf("PendingBindingFingerprint = %q, want %q", record.PendingBindingFingerprint, "fp-pending")
	}
	if record.PendingBindingFirstSeenAt == nil || !record.PendingBindingFirstSeenAt.Equal(firstSeenAt) {
		t.Fatalf("PendingBindingFirstSeenAt = %v, want %s", record.PendingBindingFirstSeenAt, firstSeenAt.Format(time.RFC3339))
	}
	if record.PendingBindingLastSeenAt == nil || !record.PendingBindingLastSeenAt.Equal(now) {
		t.Fatalf("PendingBindingLastSeenAt = %v, want %s", record.PendingBindingLastSeenAt, now.Format(time.RFC3339))
	}
	if record.PendingBindingAttemptCount != 3 {
		t.Fatalf("PendingBindingAttemptCount = %d, want 3", record.PendingBindingAttemptCount)
	}
}

func TestBindingTransitionStartsNewBindingEpochOnInitialBind(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 7, 45, 0, 0, time.UTC)

	record := resolveEnrollmentBindingTransition(monitoringinstances.Record{
		BindingStatus: monitoringinstances.BindingUnbound,
	}, "fp-new", now)

	if record.BindingStatus != monitoringinstances.BindingBound {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, monitoringinstances.BindingBound)
	}
	if record.BindingEpochStartedAt == nil || !record.BindingEpochStartedAt.Equal(now) {
		t.Fatalf("BindingEpochStartedAt = %v, want %s", record.BindingEpochStartedAt, now.Format(time.RFC3339))
	}
}

func TestMonitoringInstanceOnboardingPhaseDerivation(t *testing.T) {
	t.Parallel()

	heartbeatAt := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name                   string
		record                 monitoringinstances.Record
		hasHostSample          bool
		hasAcceptedObservation bool
		want                   string
	}{
		{
			name: "unbound monitoringInstances have not started onboarding",
			record: monitoringinstances.Record{
				BindingStatus: monitoringinstances.BindingUnbound,
			},
			want: monitoringinstances.OnboardingPhaseNotStarted,
		},
		{
			name: "bound monitoringInstances without runtime facts await stable observation",
			record: monitoringinstances.Record{
				BindingStatus: monitoringinstances.BindingBound,
			},
			want: monitoringinstances.OnboardingPhaseBoundAwaitingObservation,
		},
		{
			name: "bound monitoringInstances with heartbeat and host sample are completed",
			record: monitoringinstances.Record{
				BindingStatus:   monitoringinstances.BindingBound,
				LastHeartbeatAt: &heartbeatAt,
			},
			hasHostSample: true,
			want:          monitoringinstances.OnboardingPhaseCompleted,
		},
		{
			name: "pending confirmation surfaces binding conflict phase",
			record: monitoringinstances.Record{
				BindingStatus:             monitoringinstances.BindingPendingConfirmation,
				PendingBindingFingerprint: "fp-pending",
			},
			want: monitoringinstances.OnboardingPhaseBindingConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := monitoringinstances.DeriveOnboardingPhase(tt.record, tt.hasHostSample, tt.hasAcceptedObservation)
			if got != tt.want {
				t.Fatalf("DeriveOnboardingPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMonitoringInstanceOnboardingMigrationAddsPersistenceColumns(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0004_add_node_onboarding_binding_state.sql"))
	if err != nil {
		t.Fatalf("ReadFile(migration) error = %v", err)
	}

	text := string(source)
	for _, snippet := range []string{
		"add column if not exists enrollment_token_issued_at timestamptz",
		"add column if not exists pending_binding_fingerprint text",
		"add column if not exists pending_binding_first_seen_at timestamptz",
		"add column if not exists pending_binding_last_seen_at timestamptz",
		"add column if not exists pending_binding_attempt_count integer not null default 0",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("migration missing %q", snippet)
		}
	}
}

func TestEnrollmentTokenConsumptionMigrationAddsActiveTokenState(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0025_add_enrollment_token_consumption.sql"))
	if err != nil {
		t.Fatalf("ReadFile(consumption migration) error = %v", err)
	}

	text := string(source)
	for _, snippet := range []string{
		"add column if not exists enrollment_token_consumed_at timestamptz",
		"create index if not exists idx_nodes_enrollment_token_active",
		"where enrollment_token_hash is not null",
		"and enrollment_token_consumed_at is null",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("migration missing %q", snippet)
		}
	}
}

func TestMonitoringInstanceLifecycleManagementMigrationAddsArchiveColumns(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0043_add_monitoring_instance_management.sql"))
	if err != nil {
		t.Fatalf("ReadFile(management migration) error = %v", err)
	}

	text := string(source)
	for _, snippet := range []string{
		"add column if not exists archived_at timestamptz",
		"add column if not exists archived_reason text not null default ''",
		"create index if not exists idx_monitoring_instances_archived_at",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("migration missing %q", snippet)
		}
	}
}

func TestScanMonitoringInstanceReadsArchiveFields(t *testing.T) {
	t.Parallel()

	archivedAt := time.Date(2026, time.June, 10, 9, 30, 0, 0, time.UTC)
	createdAt := time.Date(2026, time.June, 9, 9, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.June, 10, 9, 31, 0, 0, time.UTC)

	record, err := scanMonitoringInstance(fakeMonitoringInstanceRow{scan: func(dest ...any) error {
		scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
			MonitoringInstanceID: "mi_archived",
			DisplayName:          "Archived Instance",
			LifecycleStatus:      monitoringinstances.LifecycleRetired,
			MonitoringStatus:     monitoringinstances.MonitoringPaused,
			BindingStatus:        monitoringinstances.BindingBound,
			CurrentHealthStatus:  monitoringinstances.HealthNormal,
			ArchivedAt:           &archivedAt,
			ArchivedReason:       "重复创建",
			CreatedAt:            createdAt,
			UpdatedAt:            updatedAt,
		})
		return nil
	}})
	if err != nil {
		t.Fatalf("scanMonitoringInstance() error = %v", err)
	}
	if record.ArchivedAt == nil || !record.ArchivedAt.Equal(archivedAt) {
		t.Fatalf("ArchivedAt = %v, want %s", record.ArchivedAt, archivedAt.Format(time.RFC3339))
	}
	if record.ArchivedReason != "重复创建" {
		t.Fatalf("ArchivedReason = %q, want 重复创建", record.ArchivedReason)
	}
}

func TestSetPendingActionStoresDurablePendingLastAction(t *testing.T) {
	t.Parallel()

	var (
		execSQL  string
		execArgs []any
	)
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execSQL = sql
			execArgs = append([]any(nil), args...)
			return pgconn.NewCommandTag("UPDATE 1"), nil
		},
	}}

	if err := repo.SetPendingAction(context.Background(), "mi_001", "act_001", "uptime"); err != nil {
		t.Fatalf("SetPendingAction() error = %v", err)
	}

	if !strings.Contains(execSQL, "last_action = $3") {
		t.Fatalf("exec SQL = %q, want last_action write", execSQL)
	}
	if len(execArgs) != 4 {
		t.Fatalf("exec args = %#v, want action id, command id, payload, monitoringInstance id", execArgs)
	}
	raw, ok := execArgs[2].([]byte)
	if !ok {
		t.Fatalf("last_action arg = %#v, want []byte JSON", execArgs[2])
	}
	payload := string(raw)
	for _, want := range []string{`"action_id":"act_001"`, `"command_id":"uptime"`, `"status":"pending"`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("last_action payload = %s, missing %s", payload, want)
		}
	}
}

func TestSetPendingActionBlocksArchivedMonitoringInstance(t *testing.T) {
	t.Parallel()

	var execSQL string
	rowCall := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			execSQL = sql
			return pgconn.NewCommandTag("UPDATE 0"), nil
		},
		queryRow: func(context.Context, string, ...any) pgx.Row {
			rowCall++
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				switch d := dest[0].(type) {
				case *bool:
					*d = true
				default:
					t.Fatalf("unexpected destination type %#v", dest[0])
				}
				return nil
			}}
		},
	}}

	err := repo.SetPendingAction(context.Background(), "mi_archived", "act_001", "uptime")
	if !errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
		t.Fatalf("SetPendingAction() error = %v, want ErrArchivedMonitoringInstance", err)
	}
	if !strings.Contains(execSQL, "archived_at is null") {
		t.Fatalf("SetPendingAction() SQL = %q, want archived_at guard", execSQL)
	}
	if rowCall == 0 {
		t.Fatal("SetPendingAction() did not check archived state after update miss")
	}
}

func TestMonitoringInstanceBindingEpochMigrationAddsBoundaryColumn(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0005_add_node_binding_epoch.sql"))
	if err != nil {
		t.Fatalf("ReadFile(binding epoch migration) error = %v", err)
	}

	if !strings.Contains(string(source), "add column if not exists binding_epoch_started_at timestamptz") {
		t.Fatal("binding epoch migration missing binding_epoch_started_at column")
	}
	if !strings.Contains(string(source), "set binding_epoch_started_at = created_at") {
		t.Fatal("binding epoch migration missing created_at backfill")
	}
	if !strings.Contains(string(source), "coalesce(binding_fingerprint, '') <> ''") {
		t.Fatal("binding epoch migration missing active binding backfill scope")
	}
	if !strings.Contains(string(source), "binding_epoch_started_at is null") {
		t.Fatal("binding epoch migration missing null-epoch guard")
	}
}

func TestMonitoringInstanceOnboardingIssueEnrollmentTokenStoresIssuedAt(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 8, 15, 0, 0, time.UTC)
	var (
		gotSQL  string
		gotArgs []any
	)
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			gotArgs = append([]any(nil), args...)
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*time.Time)) = issuedAt
				return nil
			}}
		},
	}}

	result, err := repo.IssueMonitoringInstanceEnrollmentToken(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() error = %v", err)
	}

	if result.Token == "" {
		t.Fatal("IssueMonitoringInstanceEnrollmentToken().Token = empty, want generated plaintext token")
	}
	if !strings.HasPrefix(result.Token, "enroll_") || len(result.Token) != len("enroll_")+64 {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken().Token = %q, want 32-byte secret token", result.Token)
	}
	if !result.IssuedAt.Equal(issuedAt) {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken().IssuedAt = %s, want %s", result.IssuedAt.Format(time.RFC3339), issuedAt.Format(time.RFC3339))
	}
	wantExpiresAt := issuedAt.Add(monitoringinstances.EnrollmentTokenTTL)
	if !result.ExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken().ExpiresAt = %s, want %s", result.ExpiresAt.Format(time.RFC3339), wantExpiresAt.Format(time.RFC3339))
	}
	if len(gotArgs) != 2 {
		t.Fatalf("len(gotArgs) = %d, want 2", len(gotArgs))
	}
	if gotArgs[0] != "mi_001" {
		t.Fatalf("gotArgs[0] = %#v, want mi_001", gotArgs[0])
	}
	if gotArgs[1] != hashEnrollmentToken(result.Token) {
		t.Fatalf("gotArgs[1] = %#v, want enrollment token hash", gotArgs[1])
	}
	if !isHMACAgentTokenHash(gotArgs[1].(string)) {
		t.Fatalf("gotArgs[1] = %#v, want versioned hmac enrollment token hash", gotArgs[1])
	}
	if !strings.Contains(gotSQL, "enrollment_token_issued_at = now()") {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() SQL = %q, want enrollment_token_issued_at update", gotSQL)
	}
	if !strings.Contains(gotSQL, "enrollment_token_consumed_at = null") {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() SQL = %q, want consumed marker reset", gotSQL)
	}
	if !strings.Contains(gotSQL, "archived_at is null") {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() SQL = %q, want archived_at guard", gotSQL)
	}
}

func TestMonitoringInstanceOnboardingIssueEnrollmentTokenBlocksArchivedInstance(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "returning enrollment_token_issued_at") {
				return fakeMonitoringInstanceRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	_, err := repo.IssueMonitoringInstanceEnrollmentToken(context.Background(), "mi_archived")
	if !errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() error = %v, want ErrArchivedMonitoringInstance", err)
	}
}

func TestFindMonitoringInstanceByEnrollmentTokenRequiresActiveUnexpiredToken(t *testing.T) {
	t.Parallel()

	var gotSQL string
	var gotArgs []any
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			gotArgs = append([]any(nil), args...)
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{MonitoringInstanceID: "mi_001"})
				return nil
			}}
		},
	}}

	_, err := repo.FindMonitoringInstanceByEnrollmentToken(context.Background(), "enroll_001")
	if err != nil {
		t.Fatalf("FindMonitoringInstanceByEnrollmentToken() error = %v", err)
	}
	if len(gotArgs) != 2 || gotArgs[0] != hashEnrollmentToken("enroll_001") || gotArgs[1] != hashOpaqueToken("enroll_001") {
		t.Fatalf("QueryRow args = %#v, want current and legacy enrollment token hashes", gotArgs)
	}
	for _, snippet := range []string{
		"enrollment_token_consumed_at is null",
		"enrollment_token_issued_at >= now() - interval '30 minutes'",
	} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("FindMonitoringInstanceByEnrollmentToken() SQL = %q, missing %q", gotSQL, snippet)
		}
	}
}

func TestApplyEnrollmentConsumesActiveUnexpiredToken(t *testing.T) {
	t.Parallel()

	var (
		selectSQL  string
		selectArgs []any
		updateSQL  string
		updateArgs []any
		committed  bool
		call       int
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			call++
			if call == 1 {
				selectSQL = sql
				selectArgs = append([]any(nil), args...)
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "mi_001"
					*(dest[1].(*string)) = hashEnrollmentToken("enroll_001")
					*(dest[2].(*string)) = monitoringinstances.BindingUnbound
					*(dest[3].(*string)) = ""
					*(dest[4].(**time.Time)) = nil
					*(dest[5].(*string)) = ""
					*(dest[6].(**time.Time)) = nil
					*(dest[7].(**time.Time)) = nil
					*(dest[8].(*int)) = 0
					return nil
				}}
			}
			updateSQL = sql
			updateArgs = append([]any(nil), args...)
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				now := time.Date(2026, time.May, 15, 8, 0, 0, 0, time.UTC)
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID:  "mi_001",
					BindingStatus:         monitoringinstances.BindingBound,
					BindingFingerprint:    "fp_001",
					BindingEpochStartedAt: &now,
				})
				return nil
			}}
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	record, syncToken, err := repo.ApplyEnrollment(context.Background(), enrollment.EnrollInput{Token: "enroll_001", Fingerprint: "fp_001"})
	if err != nil {
		t.Fatalf("ApplyEnrollment() error = %v", err)
	}
	if record.MonitoringInstanceID != "mi_001" || record.BindingStatus != monitoringinstances.BindingBound {
		t.Fatalf("record = %#v, want bound mi_001", record)
	}
	if syncToken == "" {
		t.Fatal("syncToken = empty, want generated sync token for bound enrollment")
	}
	if !strings.HasPrefix(syncToken, "sync_") || len(syncToken) != len("sync_")+64 {
		t.Fatalf("syncToken = %q, want 32-byte secret token", syncToken)
	}
	if len(selectArgs) != 2 || selectArgs[0] != hashEnrollmentToken("enroll_001") || selectArgs[1] != hashOpaqueToken("enroll_001") {
		t.Fatalf("select args = %#v, want current and legacy enrollment hashes", selectArgs)
	}
	for _, snippet := range []string{
		"enrollment_token_consumed_at is null",
		"enrollment_token_issued_at >= now() - interval '30 minutes'",
		"for update",
	} {
		if !strings.Contains(selectSQL, snippet) {
			t.Fatalf("ApplyEnrollment select SQL = %q, missing %q", selectSQL, snippet)
		}
	}
	if !strings.Contains(updateSQL, "enrollment_token_consumed_at = now()") {
		t.Fatalf("ApplyEnrollment update SQL = %q, want consumed marker write", updateSQL)
	}
	if !strings.Contains(updateSQL, "sync_token_hash = case when $9 <> '' then $9 else sync_token_hash end") {
		t.Fatalf("ApplyEnrollment update SQL = %q, want atomic sync token write", updateSQL)
	}
	if len(updateArgs) != 10 {
		t.Fatalf("ApplyEnrollment update args = %#v, want 10 args", updateArgs)
	}
	if updateArgs[8] != hashSyncToken(syncToken) {
		t.Fatalf("ApplyEnrollment sync hash arg = %#v, want hash of returned token", updateArgs[8])
	}
	if !isHMACAgentTokenHash(updateArgs[8].(string)) {
		t.Fatalf("ApplyEnrollment sync hash arg = %#v, want versioned hmac sync token hash", updateArgs[8])
	}
	if !committed {
		t.Fatal("transaction was not committed")
	}
}

func TestMonitoringInstanceOnboardingGetStateReturnsDerivedPhaseAndPendingMetadata(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 8, 20, 0, 0, time.UTC)
	firstSeenAt := issuedAt.Add(5 * time.Minute)
	lastSeenAt := firstSeenAt.Add(2 * time.Minute)
	heartbeatAt := issuedAt.Add(10 * time.Minute)
	activeFingerprint := "sha256:curr1234567890abcdef"
	var gotSQL string
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			if len(args) != 1 || args[0] != "mi_001" {
				t.Fatalf("QueryRow args = %#v, want monitoringInstance id", args)
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID:       "mi_001",
					DisplayName:                "MonitoringInstance 001",
					LifecycleStatus:            monitoringinstances.LifecyclePendingEnrollment,
					MonitoringStatus:           monitoringinstances.MonitoringEnabled,
					BindingStatus:              monitoringinstances.BindingPendingConfirmation,
					BindingFingerprint:         activeFingerprint,
					BindingEpochStartedAt:      &issuedAt,
					PendingBindingFingerprint:  "fp-pending",
					PendingBindingFirstSeenAt:  &firstSeenAt,
					PendingBindingLastSeenAt:   &lastSeenAt,
					PendingBindingAttemptCount: 4,
					EnrollmentTokenIssuedAt:    &issuedAt,
					CurrentHealthStatus:        monitoringinstances.HealthNormal,
					LastHeartbeatAt:            &heartbeatAt,
				})
				*(dest[32].(*bool)) = true
				*(dest[33].(*bool)) = false
				return nil
			}}
		},
	}}

	state, err := repo.GetMonitoringInstanceOnboarding(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("GetMonitoringInstanceOnboarding() error = %v", err)
	}

	if state.Phase != monitoringinstances.OnboardingPhaseBindingConflict {
		t.Fatalf("Phase = %q, want %q", state.Phase, monitoringinstances.OnboardingPhaseBindingConflict)
	}
	if state.PendingBinding == nil {
		t.Fatal("PendingBinding = nil, want metadata")
	}
	if state.PendingBinding.Fingerprint != "fp-pending" {
		t.Fatalf("PendingBinding.Fingerprint = %q, want %q", state.PendingBinding.Fingerprint, "fp-pending")
	}
	if state.PendingBinding.AttemptCount != 4 {
		t.Fatalf("PendingBinding.AttemptCount = %d, want 4", state.PendingBinding.AttemptCount)
	}
	if state.CurrentBindingFingerprintSummary != "sha256:c…abcdef" {
		t.Fatalf("CurrentBindingFingerprintSummary = %q, want %q", state.CurrentBindingFingerprintSummary, "sha256:c…abcdef")
	}
	if state.EnrollmentTokenIssuedAt == nil || !state.EnrollmentTokenIssuedAt.Equal(issuedAt) {
		t.Fatalf("EnrollmentTokenIssuedAt = %v, want %s", state.EnrollmentTokenIssuedAt, issuedAt.Format(time.RFC3339))
	}
	if !state.HasHostSample {
		t.Fatal("HasHostSample = false, want true")
	}
	if state.HasAcceptedObservation {
		t.Fatal("HasAcceptedObservation = true, want false")
	}
	if !strings.Contains(gotSQL, "pending_binding_fingerprint") {
		t.Fatalf("GetMonitoringInstanceOnboarding() SQL = %q, want pending binding columns", gotSQL)
	}
	if !strings.Contains(gotSQL, "from host_samples hs") {
		t.Fatalf("GetMonitoringInstanceOnboarding() SQL = %q, want host sample existence check", gotSQL)
	}
}

func TestMonitoringInstanceOnboardingGetStateScopesEvidenceToCurrentBindingGeneration(t *testing.T) {
	t.Parallel()

	staleHeartbeatAt := time.Date(2026, time.April, 26, 6, 0, 0, 0, time.UTC)
	bindingEpochStartedAt := time.Date(2026, time.April, 26, 9, 0, 0, 0, time.UTC)
	var gotSQL string
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID:  "mi_rebind",
					BindingStatus:         monitoringinstances.BindingBound,
					BindingFingerprint:    "fp-current",
					BindingEpochStartedAt: &bindingEpochStartedAt,
					LastHeartbeatAt:       &staleHeartbeatAt,
				})
				*(dest[32].(*bool)) = false
				*(dest[33].(*bool)) = false
				return nil
			}}
		},
	}}

	state, err := repo.GetMonitoringInstanceOnboarding(context.Background(), "mi_rebind")
	if err != nil {
		t.Fatalf("GetMonitoringInstanceOnboarding() error = %v", err)
	}

	if state.Phase != monitoringinstances.OnboardingPhaseBoundAwaitingObservation {
		t.Fatalf("Phase = %q, want %q", state.Phase, monitoringinstances.OnboardingPhaseBoundAwaitingObservation)
	}
	if !strings.Contains(gotSQL, "hs.fingerprint = mi.binding_fingerprint") {
		t.Fatalf("GetMonitoringInstanceOnboarding() SQL = %q, want host sample fingerprint scope", gotSQL)
	}
	if !strings.Contains(gotSQL, "hs.received_at >= mi.binding_epoch_started_at") {
		t.Fatalf("GetMonitoringInstanceOnboarding() SQL = %q, want host sample binding epoch scope", gotSQL)
	}
	if !strings.Contains(gotSQL, "po.fingerprint = mi.binding_fingerprint") {
		t.Fatalf("GetMonitoringInstanceOnboarding() SQL = %q, want probe observation fingerprint scope", gotSQL)
	}
	if !strings.Contains(gotSQL, "po.received_at >= mi.binding_epoch_started_at") {
		t.Fatalf("GetMonitoringInstanceOnboarding() SQL = %q, want probe observation binding epoch scope", gotSQL)
	}
}

func TestUpdateMonitoringInstanceMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 27, 10, 0, 0, 0, time.UTC)
	expectedUpdatedAt := now.Add(-5 * time.Minute)
	var (
		gotSQL  string
		gotArgs []any
	)
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			gotArgs = append([]any(nil), args...)
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID:       "mi_001",
					DisplayName:                "MonitoringInstance 001",
					Region:                     "ap-northeast-1",
					City:                       "Tokyo",
					Provider:                   "Vultr",
					LifecycleStatus:            monitoringinstances.LifecyclePendingEnrollment,
					MonitoringStatus:           monitoringinstances.MonitoringEnabled,
					BindingStatus:              monitoringinstances.BindingUnbound,
					Labels:                     []string{"edge", "core"},
					Note:                       "updated",
					CurrentHealthStatus:        monitoringinstances.HealthNormal,
					CurrentActiveIncidentCount: 2,
					CurrentPrimaryIssueSummary: "packet loss",
					CreatedAt:                  now.Add(-time.Hour),
					UpdatedAt:                  now,
				})
				return nil
			}}
		},
	}}

	record, err := repo.UpdateMonitoringInstanceMetadata(context.Background(), "mi_001", monitoringinstances.UpdateMetadataInput{
		Labels:            []string{"edge", "core"},
		Note:              "updated",
		ExpectedUpdatedAt: &expectedUpdatedAt,
	})
	if err != nil {
		t.Fatalf("UpdateMonitoringInstanceMetadata() error = %v", err)
	}

	if len(gotArgs) != 5 {
		t.Fatalf("len(gotArgs) = %d, want 5", len(gotArgs))
	}
	if gotArgs[0] != "mi_001" {
		t.Fatalf("gotArgs[0] = %#v, want %q", gotArgs[0], "mi_001")
	}
	if labels, ok := gotArgs[2].([]string); !ok || len(labels) != 2 || labels[0] != "edge" || labels[1] != "core" {
		t.Fatalf("gotArgs[2] = %#v, want %#v", gotArgs[2], []string{"edge", "core"})
	}
	if gotArgs[3] != "updated" {
		t.Fatalf("gotArgs[3] = %#v, want %q", gotArgs[3], "updated")
	}
	if gotUpdatedAt, ok := gotArgs[4].(time.Time); !ok || !gotUpdatedAt.Equal(expectedUpdatedAt) {
		t.Fatalf("gotArgs[4] = %#v, want %s", gotArgs[4], expectedUpdatedAt.Format(time.RFC3339Nano))
	}
	if !strings.Contains(gotSQL, "update monitoring_instances") {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want update monitoring_instances", gotSQL)
	}
	if !strings.Contains(gotSQL, "labels") {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want labels update", gotSQL)
	}
	if !strings.Contains(gotSQL, "note") {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want note update", gotSQL)
	}
	if !strings.Contains(gotSQL, "updated_at = now()") {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want updated_at refresh", gotSQL)
	}
	if !strings.Contains(gotSQL, "updated_at = $5") {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want optimistic updated_at precondition", gotSQL)
	}
	if !strings.Contains(gotSQL, "archived_at is null") {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want archived_at guard", gotSQL)
	}
	if !strings.Contains(gotSQL, "returning "+monitoringInstanceSelectColumns) {
		t.Fatalf("UpdateMonitoringInstanceMetadata() SQL = %q, want returning monitoringInstanceSelectColumns", gotSQL)
	}
	if record.MonitoringInstanceID != "mi_001" {
		t.Fatalf("record.MonitoringInstanceID = %q, want %q", record.MonitoringInstanceID, "mi_001")
	}
	if record.DisplayName != "MonitoringInstance 001" {
		t.Fatalf("record.DisplayName = %q, want %q", record.DisplayName, "MonitoringInstance 001")
	}
	if len(record.Labels) != 2 || record.Labels[0] != "edge" || record.Labels[1] != "core" {
		t.Fatalf("record.Labels = %#v, want %#v", record.Labels, []string{"edge", "core"})
	}
	if record.Note != "updated" {
		t.Fatalf("record.Note = %q, want %q", record.Note, "updated")
	}
	if record.UpdatedAt != now {
		t.Fatalf("record.UpdatedAt = %s, want %s", record.UpdatedAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
}

func TestUpdateMonitoringInstanceMetadataBlocksArchivedInstance(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "update monitoring_instances") {
				return fakeMonitoringInstanceRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				switch d := dest[0].(type) {
				case *bool:
					*d = true
				default:
					t.Fatalf("unexpected destination type %#v", dest[0])
				}
				return nil
			}}
		},
	}}

	_, err := repo.UpdateMonitoringInstanceMetadata(context.Background(), "mi_archived", monitoringinstances.UpdateMetadataInput{})
	if !errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
		t.Fatalf("UpdateMonitoringInstanceMetadata() error = %v, want ErrArchivedMonitoringInstance", err)
	}
}

func TestUpdateMonitoringInstanceMetadataMapsNotFound(t *testing.T) {
	t.Parallel()

	queryCount := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			queryCount++
			if queryCount == 2 {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = false
					return nil
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				return pgx.ErrNoRows
			}}
		},
	}}

	_, err := repo.UpdateMonitoringInstanceMetadata(context.Background(), "mi_missing", monitoringinstances.UpdateMetadataInput{})
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("UpdateMonitoringInstanceMetadata() error = %v, want ErrMonitoringInstanceNotFound", err)
	}
}

func TestUpdateMonitoringInstanceMetadataMapsPreconditionMissToConflictWhenMonitoringInstanceExists(t *testing.T) {
	t.Parallel()

	expectedUpdatedAt := time.Date(2026, time.April, 27, 9, 55, 0, 0, time.UTC)
	queryCount := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			switch queryCount {
			case 1:
				if !strings.Contains(sql, "updated_at = $5") {
					t.Fatalf("update SQL = %q, want updated_at precondition", sql)
				}
				if len(args) != 5 {
					t.Fatalf("update args = %#v, want five args (monitoring_instance_id, group, labels, note, expected_updated_at)", args)
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					return pgx.ErrNoRows
				}}
			case 2:
				if !strings.Contains(sql, "archived_at is not null") {
					t.Fatalf("archive SQL = %q, want archived state check", sql)
				}
				if len(args) != 1 || args[0] != "mi_001" {
					t.Fatalf("archive args = %#v, want monitoringInstance id", args)
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = false
					return nil
				}}
			case 3:
				if !strings.Contains(sql, "exists (") || !strings.Contains(sql, "from monitoring_instances") {
					t.Fatalf("existence SQL = %q, want monitoringInstance existence check", sql)
				}
				if len(args) != 1 || args[0] != "mi_001" {
					t.Fatalf("existence args = %#v, want monitoringInstance id", args)
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow call %d", queryCount)
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			}
		},
	}}

	_, err := repo.UpdateMonitoringInstanceMetadata(context.Background(), "mi_001", monitoringinstances.UpdateMetadataInput{
		Labels:            []string{"edge"},
		Note:              "updated",
		ExpectedUpdatedAt: &expectedUpdatedAt,
	})
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceMetadataConflict) {
		t.Fatalf("UpdateMonitoringInstanceMetadata() error = %v, want ErrMonitoringInstanceMetadataConflict", err)
	}
	if queryCount != 3 {
		t.Fatalf("QueryRow calls = %d, want 3", queryCount)
	}
}

func TestCreateLinkedMonitoringInstanceRejectsExistingActiveLinkBeforeInsert(t *testing.T) {
	t.Parallel()

	var (
		queryRows []string
		committed bool
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			queryRows = append(queryRows, sql)
			if len(args) != 1 || args[0] != "vps_001" {
				t.Fatalf("guard QueryRow args = %#v, want vps id only", args)
			}
			switch len(queryRows) {
			case 1:
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					return nil
				}}
			case 2:
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow after active-link guard: %q", sql)
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error { return nil }}
			}
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	_, _, err := repo.CreateLinkedMonitoringInstance(context.Background(), "vps_001", monitoringinstances.CreateInput{
		DisplayName:     "Tokyo Edge",
		Region:          "Tokyo",
		City:            "Tokyo",
		Provider:        "Acme",
		LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
	}, "created from vps detail")
	if !errors.Is(err, assetlinks.ErrVPSActiveMonitoringInstanceExists) {
		t.Fatalf("CreateLinkedMonitoringInstance() error = %v, want ErrVPSActiveMonitoringInstanceExists", err)
	}
	if len(queryRows) != 2 {
		t.Fatalf("QueryRow calls = %d, want lock and active count only; SQL=%#v", len(queryRows), queryRows)
	}
	if !strings.Contains(queryRows[0], "from vps_assets") || !strings.Contains(queryRows[0], "for update") {
		t.Fatalf("first guard SQL = %q, want VPS row lock", queryRows[0])
	}
	if !strings.Contains(queryRows[1], "select count(*)") || !strings.Contains(queryRows[1], "from vps_monitoring_instance_links") || !strings.Contains(queryRows[1], "unlinked_at is null") {
		t.Fatalf("second guard SQL = %q, want active link count", queryRows[1])
	}
	if committed {
		t.Fatal("transaction committed despite active-link conflict")
	}
}

func TestBindingConfirmRebindMovesPendingFingerprintIntoActiveBinding(t *testing.T) {
	t.Parallel()

	var (
		gotSQL    string
		execSQL   string
		execArgs  []any
		committed bool
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			if len(args) != 1 || args[0] != "mi_002" {
				t.Fatalf("QueryRow args = %#v, want monitoringInstance id", args)
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID: "mi_002",
					BindingStatus:        monitoringinstances.BindingBound,
					BindingFingerprint:   "fp-pending",
				})
				return nil
			}}
		},
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execSQL = sql
			execArgs = append([]any(nil), args...)
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
	}}

	record, err := repo.ConfirmMonitoringInstanceRebind(context.Background(), "mi_002")
	if err != nil {
		t.Fatalf("ConfirmMonitoringInstanceRebind() error = %v", err)
	}

	if record.BindingStatus != monitoringinstances.BindingBound {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, monitoringinstances.BindingBound)
	}
	if record.BindingFingerprint != "fp-pending" {
		t.Fatalf("BindingFingerprint = %q, want %q", record.BindingFingerprint, "fp-pending")
	}
	for _, snippet := range []string{
		"binding_fingerprint = pending_binding_fingerprint",
		"binding_epoch_started_at = now()",
		"binding_status = '已绑定'",
		"binding_status = '指纹变更待确认'",
		"coalesce(pending_binding_fingerprint, '') <> ''",
		"pending_binding_fingerprint = null",
		"pending_binding_attempt_count = 0",
		"sync_token_hash = ''",
		"last_heartbeat_at = null",
		"last_sync_at = null",
	} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("ConfirmMonitoringInstanceRebind() SQL missing %q", snippet)
		}
	}
	if !strings.Contains(execSQL, "insert into state_change_events") {
		t.Fatalf("ConfirmMonitoringInstanceRebind() event SQL = %q, want state_change_events insert", execSQL)
	}
	if len(execArgs) != 8 {
		t.Fatalf("len(execArgs) = %d, want 8", len(execArgs))
	}
	if execArgs[1] != string(incidents.ObjectTypeMonitoringInstance) {
		t.Fatalf("event object_type = %#v, want %q", execArgs[1], incidents.ObjectTypeMonitoringInstance)
	}
	if execArgs[2] != "mi_002" {
		t.Fatalf("event object_id = %#v, want %q", execArgs[2], "mi_002")
	}
	if execArgs[3] != string(incidents.EventMonitoringInstanceBindingRebindConfirmed) {
		t.Fatalf("event_type = %#v, want %q", execArgs[3], incidents.EventMonitoringInstanceBindingRebindConfirmed)
	}
	if summary, ok := execArgs[5].(string); !ok || !strings.Contains(summary, "确认") {
		t.Fatalf("event summary = %#v, want confirm wording", execArgs[5])
	}
	if !committed {
		t.Fatal("transaction was not committed")
	}
}

func TestBindingConfirmRebindRejectsRetryWithoutPendingFingerprint(t *testing.T) {
	t.Parallel()

	callCount := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			callCount++
			if callCount == 1 {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					return pgx.ErrNoRows
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	_, err := repo.ConfirmMonitoringInstanceRebind(context.Background(), "mi_retry")
	if !errors.Is(err, monitoringinstances.ErrInvalidBindingTransition) {
		t.Fatalf("ConfirmMonitoringInstanceRebind() error = %v, want ErrInvalidBindingTransition", err)
	}
}

func TestBindingConfirmRebindReturnsMonitoringInstanceNotFoundWhenMonitoringInstanceIsMissing(t *testing.T) {
	t.Parallel()

	callCount := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			callCount++
			if callCount == 1 {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					return pgx.ErrNoRows
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}

	_, err := repo.ConfirmMonitoringInstanceRebind(context.Background(), "mi_missing")
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("ConfirmMonitoringInstanceRebind() error = %v, want ErrMonitoringInstanceNotFound", err)
	}
}

func TestBindingRejectPendingClearsPendingMetadataAndKeepsActiveBinding(t *testing.T) {
	t.Parallel()

	var (
		gotSQL    string
		execSQL   string
		execArgs  []any
		committed bool
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID:       "mi_003",
					BindingStatus:              monitoringinstances.BindingBound,
					BindingFingerprint:         "fp-active",
					PendingBindingAttemptCount: 0,
				})
				return nil
			}}
		},
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execSQL = sql
			execArgs = append([]any(nil), args...)
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
	}}

	record, err := repo.RejectPendingFingerprint(context.Background(), "mi_003")
	if err != nil {
		t.Fatalf("RejectPendingFingerprint() error = %v", err)
	}

	if record.BindingFingerprint != "fp-active" {
		t.Fatalf("BindingFingerprint = %q, want %q", record.BindingFingerprint, "fp-active")
	}
	for _, snippet := range []string{
		"binding_status = '已绑定'",
		"binding_status = '指纹变更待确认'",
		"coalesce(pending_binding_fingerprint, '') <> ''",
		"pending_binding_fingerprint = null",
		"pending_binding_attempt_count = 0",
	} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("RejectPendingFingerprint() SQL missing %q", snippet)
		}
	}
	if !strings.Contains(execSQL, "insert into state_change_events") {
		t.Fatalf("RejectPendingFingerprint() event SQL = %q, want state_change_events insert", execSQL)
	}
	if execArgs[3] != string(incidents.EventMonitoringInstanceBindingPendingRejected) {
		t.Fatalf("event_type = %#v, want %q", execArgs[3], incidents.EventMonitoringInstanceBindingPendingRejected)
	}
	if summary, ok := execArgs[5].(string); !ok || !strings.Contains(summary, "拒绝") {
		t.Fatalf("event summary = %#v, want reject wording", execArgs[5])
	}
	if !committed {
		t.Fatal("transaction was not committed")
	}
}

func TestBindingRejectPendingRejectsWhenNoPendingFingerprintExists(t *testing.T) {
	t.Parallel()

	callCount := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			callCount++
			if callCount == 1 {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					return pgx.ErrNoRows
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	_, err := repo.RejectPendingFingerprint(context.Background(), "mi_retry")
	if !errors.Is(err, monitoringinstances.ErrInvalidBindingTransition) {
		t.Fatalf("RejectPendingFingerprint() error = %v, want ErrInvalidBindingTransition", err)
	}
}

func TestBindingRejectPendingReturnsMonitoringInstanceNotFoundWhenMonitoringInstanceIsMissing(t *testing.T) {
	t.Parallel()

	callCount := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			callCount++
			if callCount == 1 {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					return pgx.ErrNoRows
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}

	_, err := repo.RejectPendingFingerprint(context.Background(), "mi_missing")
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("RejectPendingFingerprint() error = %v, want ErrMonitoringInstanceNotFound", err)
	}
}

func TestBindingResetClearsActiveAndPendingBindingState(t *testing.T) {
	t.Parallel()

	var (
		gotSQL    string
		execSQL   string
		execArgs  []any
		committed bool
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID: "mi_004",
					BindingStatus:        monitoringinstances.BindingUnbound,
				})
				return nil
			}}
		},
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execSQL = sql
			execArgs = append([]any(nil), args...)
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
	}}

	record, err := repo.ResetMonitoringInstanceBinding(context.Background(), "mi_004")
	if err != nil {
		t.Fatalf("ResetMonitoringInstanceBinding() error = %v", err)
	}

	if record.BindingStatus != monitoringinstances.BindingUnbound {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, monitoringinstances.BindingUnbound)
	}
	for _, snippet := range []string{
		"binding_status = '未绑定'",
		"binding_fingerprint = ''",
		"binding_epoch_started_at = null",
		"pending_binding_fingerprint = null",
		"pending_binding_attempt_count = 0",
		"sync_token_hash = ''",
		"last_heartbeat_at = null",
		"last_sync_at = null",
	} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("ResetMonitoringInstanceBinding() SQL missing %q", snippet)
		}
	}
	if !strings.Contains(execSQL, "insert into state_change_events") {
		t.Fatalf("ResetMonitoringInstanceBinding() event SQL = %q, want state_change_events insert", execSQL)
	}
	if execArgs[3] != string(incidents.EventMonitoringInstanceBindingReset) {
		t.Fatalf("event_type = %#v, want %q", execArgs[3], incidents.EventMonitoringInstanceBindingReset)
	}
	if summary, ok := execArgs[5].(string); !ok || !strings.Contains(summary, "重置") {
		t.Fatalf("event summary = %#v, want reset wording", execArgs[5])
	}
	if !committed {
		t.Fatal("transaction was not committed")
	}
}

func TestApplyEnrollmentDoesNotAdvanceLastSyncAt(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("monitoring_instances.go")
	if err != nil {
		t.Fatalf("ReadFile(monitoring_instances.go) error = %v", err)
	}

	applyEnrollment := sourceBetween(t, string(source), "func (r *PostgresMonitoringInstanceRepository) ApplyEnrollment", "func (r *PostgresMonitoringInstanceRepository) RecordAcceptedHeartbeats")
	if strings.Contains(applyEnrollment, "last_sync_at") {
		t.Fatalf("ApplyEnrollment() unexpectedly updates last_sync_at:\n%s", applyEnrollment)
	}
	if !strings.Contains(applyEnrollment, "binding_status = $2") {
		t.Fatal("ApplyEnrollment() source no longer contains the enrollment binding update")
	}
	if !strings.Contains(applyEnrollment, "binding_epoch_started_at = $4") {
		t.Fatal("ApplyEnrollment() should persist binding_epoch_started_at with the current trust generation")
	}

	heartbeatPath := sourceBetween(t, string(source), "func (r *PostgresMonitoringInstanceRepository) RecordAcceptedHeartbeats", "")
	if !strings.Contains(heartbeatPath, "last_sync_at = greatest(coalesce(last_sync_at, $3), $3)") {
		t.Fatal("RecordAcceptedHeartbeats() should remain the path that advances last_sync_at")
	}
}

func TestStoreSourceIncludesSyncTokenValidationForHeartbeatWrites(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("monitoring_instances.go")
	if err != nil {
		t.Fatalf("ReadFile(monitoring_instances.go) error = %v", err)
	}

	heartbeatPath := sourceBetween(t, string(source), "func (r *PostgresMonitoringInstanceRepository) RecordAcceptedHeartbeats", "")
	if !strings.Contains(heartbeatPath, "coalesce(sync_token_hash, '')") {
		t.Fatal("RecordAcceptedHeartbeats() should load sync_token_hash inside the heartbeat transaction")
	}
	if !strings.Contains(heartbeatPath, "r.tokenHasher.syncTokenMatches(storedSyncTokenHash, syncToken)") {
		t.Fatal("RecordAcceptedHeartbeats() should compare sync token hashes with the shared versioned token hasher")
	}
	if strings.Contains(heartbeatPath, "storedSyncTokenHash != hashSyncToken(syncToken)") {
		t.Fatal("RecordAcceptedHeartbeats() should not use plain string inequality for sync token hashes")
	}
	if !strings.Contains(string(source), "ids.NewSecretToken(\"enroll\")") {
		t.Fatal("IssueMonitoringInstanceEnrollmentToken() should generate enrollment tokens with NewSecretToken")
	}
	if !strings.Contains(string(source), "ids.NewSecretToken(\"sync\")") {
		t.Fatal("IssueSyncToken()/ApplyEnrollment() should generate sync tokens with NewSecretToken")
	}
	if !strings.Contains(heartbeatPath, "write.Fingerprint != bindingFingerprint") {
		t.Fatal("RecordAcceptedHeartbeats() should continue validating fingerprint within the transaction")
	}
}

func TestMonitoringInstanceRuntimeControlTransitionsWriteEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		action               func(context.Context, *PostgresMonitoringInstanceRepository, string) (monitoringinstances.Record, error)
		monitoringInstanceID string
		sourceStatus         string
		returnedStatus       string
		wantEventType        incidents.EventType
		wantSummary          string
		wantPayload          string
		wantSQLSnippets      []string
	}{
		{
			name: "enabled to maintenance",
			action: func(ctx context.Context, repo *PostgresMonitoringInstanceRepository, monitoringInstanceID string) (monitoringinstances.Record, error) {
				return repo.SetMonitoringInstanceMonitoringMaintenance(ctx, monitoringInstanceID)
			},
			monitoringInstanceID: "mi_maintenance",
			sourceStatus:         monitoringinstances.MonitoringEnabled,
			returnedStatus:       monitoringInstanceMonitoringStatusMaintenance,
			wantEventType:        incidents.EventMonitoringInstanceMonitoringMaintenanceEntered,
			wantSummary:          "进入维护",
			wantPayload:          monitoringInstanceMonitoringStatusMaintenance,
			wantSQLSnippets: []string{
				"set monitoring_status = '维护中'",
				"where monitoring_instance_id = $1",
				"monitoring_status = '启用'",
			},
		},
		{
			name: "maintenance to enabled",
			action: func(ctx context.Context, repo *PostgresMonitoringInstanceRepository, monitoringInstanceID string) (monitoringinstances.Record, error) {
				return repo.ResumeMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
			},
			monitoringInstanceID: "mi_resume_maintenance",
			sourceStatus:         monitoringInstanceMonitoringStatusMaintenance,
			returnedStatus:       monitoringinstances.MonitoringEnabled,
			wantEventType:        incidents.EventMonitoringInstanceMonitoringMaintenanceExited,
			wantSummary:          "退出维护",
			wantPayload:          monitoringinstances.MonitoringEnabled,
			wantSQLSnippets: []string{
				"set monitoring_status = '启用'",
				"where monitoring_instance_id = $1",
				"monitoring_status in ('维护中', '暂停')",
			},
		},
		{
			name: "enabled to paused",
			action: func(ctx context.Context, repo *PostgresMonitoringInstanceRepository, monitoringInstanceID string) (monitoringinstances.Record, error) {
				return repo.PauseMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
			},
			monitoringInstanceID: "mi_pause_enabled",
			sourceStatus:         monitoringinstances.MonitoringEnabled,
			returnedStatus:       monitoringInstanceMonitoringStatusPaused,
			wantEventType:        incidents.EventMonitoringInstanceMonitoringPaused,
			wantSummary:          "暂停",
			wantPayload:          monitoringInstanceMonitoringStatusPaused,
			wantSQLSnippets: []string{
				"set monitoring_status = '暂停'",
				"where monitoring_instance_id = $1",
				"monitoring_status in ('启用', '维护中')",
			},
		},
		{
			name: "paused to enabled",
			action: func(ctx context.Context, repo *PostgresMonitoringInstanceRepository, monitoringInstanceID string) (monitoringinstances.Record, error) {
				return repo.ResumeMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
			},
			monitoringInstanceID: "mi_resume_paused",
			sourceStatus:         monitoringInstanceMonitoringStatusPaused,
			returnedStatus:       monitoringinstances.MonitoringEnabled,
			wantEventType:        incidents.EventMonitoringInstanceMonitoringResumed,
			wantSummary:          "恢复",
			wantPayload:          monitoringinstances.MonitoringEnabled,
			wantSQLSnippets: []string{
				"set monitoring_status = '启用'",
				"where monitoring_instance_id = $1",
				"monitoring_status in ('维护中', '暂停')",
			},
		},
		{
			name: "maintenance to paused",
			action: func(ctx context.Context, repo *PostgresMonitoringInstanceRepository, monitoringInstanceID string) (monitoringinstances.Record, error) {
				return repo.PauseMonitoringInstanceMonitoring(ctx, monitoringInstanceID)
			},
			monitoringInstanceID: "mi_pause_maintenance",
			sourceStatus:         monitoringInstanceMonitoringStatusMaintenance,
			returnedStatus:       monitoringInstanceMonitoringStatusPaused,
			wantEventType:        incidents.EventMonitoringInstanceMonitoringPaused,
			wantSummary:          "暂停",
			wantPayload:          monitoringInstanceMonitoringStatusPaused,
			wantSQLSnippets: []string{
				"set monitoring_status = '暂停'",
				"where monitoring_instance_id = $1",
				"monitoring_status in ('启用', '维护中')",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotSQL    string
				execSQL   string
				execArgs  []any
				committed bool
			)
			tx := &fakeMonitoringInstanceTx{
				queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
					gotSQL = sql
					if len(args) != 1 || args[0] != tt.monitoringInstanceID {
						t.Fatalf("QueryRow args = %#v, want monitoringInstance id %q", args, tt.monitoringInstanceID)
					}
					return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
						scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{MonitoringInstanceID: tt.monitoringInstanceID, MonitoringStatus: tt.returnedStatus})
						if len(dest) > 32 {
							*(dest[32].(*string)) = tt.sourceStatus
						}
						return nil
					}}
				},
				exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					execSQL = sql
					execArgs = append([]any(nil), args...)
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
				commit: func(context.Context) error {
					committed = true
					return nil
				},
			}
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

			record, err := tt.action(context.Background(), repo, tt.monitoringInstanceID)
			if err != nil {
				t.Fatalf("runtime control action error = %v", err)
			}
			if record.MonitoringStatus != tt.returnedStatus {
				t.Fatalf("MonitoringStatus = %q, want %q", record.MonitoringStatus, tt.returnedStatus)
			}
			for _, snippet := range tt.wantSQLSnippets {
				if !strings.Contains(gotSQL, snippet) {
					t.Fatalf("runtime control SQL missing %q in %q", snippet, gotSQL)
				}
			}
			if !strings.Contains(execSQL, "insert into state_change_events") {
				t.Fatalf("event SQL = %q, want state_change_events insert", execSQL)
			}
			if len(execArgs) != 8 {
				t.Fatalf("len(execArgs) = %d, want 8", len(execArgs))
			}
			if execArgs[1] != string(incidents.ObjectTypeMonitoringInstance) {
				t.Fatalf("object_type = %#v, want %q", execArgs[1], incidents.ObjectTypeMonitoringInstance)
			}
			if execArgs[2] != tt.monitoringInstanceID {
				t.Fatalf("object_id = %#v, want %q", execArgs[2], tt.monitoringInstanceID)
			}
			if execArgs[3] != string(tt.wantEventType) {
				t.Fatalf("event_type = %#v, want %q", execArgs[3], tt.wantEventType)
			}
			if summary, ok := execArgs[5].(string); !ok || !strings.Contains(summary, tt.wantSummary) {
				t.Fatalf("summary = %#v, want substring %q", execArgs[5], tt.wantSummary)
			}
			payload, ok := execArgs[6].([]byte)
			if !ok || !strings.Contains(string(payload), tt.wantPayload) {
				t.Fatalf("payload = %#v, want status %q", execArgs[6], tt.wantPayload)
			}
			if !committed {
				t.Fatal("transaction was not committed")
			}
		})
	}
}

func TestMonitoringInstanceRuntimeControlResumePreservesNullSafeSelectColumns(t *testing.T) {
	t.Parallel()

	var gotSQL string
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			if len(args) != 1 || args[0] != "mi_resume_nulls" {
				t.Fatalf("QueryRow args = %#v, want monitoringInstance id %q", args, "mi_resume_nulls")
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				for _, snippet := range []string{
					"coalesce(updated.enrollment_token_hash, '')",
					"coalesce(updated.sync_token_hash, '')",
					"coalesce(updated.binding_fingerprint, '')",
					"coalesce(updated.pending_binding_fingerprint, '')",
				} {
					if !strings.Contains(gotSQL, snippet) {
						return errors.New("missing null-safe qualified select columns")
					}
				}
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{MonitoringInstanceID: "mi_resume_nulls", MonitoringStatus: monitoringinstances.MonitoringEnabled})
				*(dest[32].(*string)) = monitoringInstanceMonitoringStatusMaintenance
				return nil
			}}
		},
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	record, err := repo.ResumeMonitoringInstanceMonitoring(context.Background(), "mi_resume_nulls")
	if err != nil {
		t.Fatalf("ResumeMonitoringInstanceMonitoring() error = %v", err)
	}
	if record.MonitoringInstanceID != "mi_resume_nulls" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", record.MonitoringInstanceID, "mi_resume_nulls")
	}
	if record.EnrollmentTokenHash != "" || record.SyncTokenHash != "" || record.BindingFingerprint != "" || record.PendingBindingFingerprint != "" {
		t.Fatalf("expected empty coalesced token/binding strings, got %#v", record)
	}
	if !strings.Contains(gotSQL, "archived_at is null") {
		t.Fatalf("ResumeMonitoringInstanceMonitoring() SQL = %q, want archived_at guard", gotSQL)
	}
}

func TestMonitoringInstanceRuntimeControlBlocksArchivedInstance(t *testing.T) {
	t.Parallel()

	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "with prior as") {
				return fakeMonitoringInstanceRow{scan: func(...any) error { return pgx.ErrNoRows }}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				*(dest[1].(*bool)) = true
				return nil
			}}
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	_, err := repo.ResumeMonitoringInstanceMonitoring(context.Background(), "mi_archived")
	if !errors.Is(err, monitoringinstances.ErrArchivedMonitoringInstance) {
		t.Fatalf("ResumeMonitoringInstanceMonitoring() error = %v, want ErrArchivedMonitoringInstance", err)
	}
}

func TestMonitoringInstanceRuntimeControlRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			queryCount := 0
			return &fakeMonitoringInstanceTx{queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
				queryCount++
				if queryCount == 2 {
					return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
						*(dest[0].(*bool)) = true
						*(dest[1].(*bool)) = false
						return nil
					}}
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}}

	_, err := repo.SetMonitoringInstanceMonitoringMaintenance(context.Background(), "mi_paused")
	if !errors.Is(err, ErrInvalidMonitoringInstanceRuntimeTransition) {
		t.Fatalf("SetMonitoringInstanceMonitoringMaintenance() error = %v, want ErrInvalidMonitoringInstanceRuntimeTransition", err)
	}
}

func TestPostgresMonitoringInstanceListHidesInstancesLinkedOnlyToArchivedVPS(t *testing.T) {
	var seenSQL string
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			seenSQL = sql
			return &fakeMonitoringInstanceRows{}, nil
		},
	}}

	if _, err := repo.ListMonitoringInstances(context.Background(), monitoringinstances.ListScopeActive); err != nil {
		t.Fatalf("ListMonitoringInstances() error = %v", err)
	}
	for _, snippet := range []string{
		"archived_at is null",
		"not exists",
		"vps_monitoring_instance_links",
		"unlinked_at is null",
		"v.lifecycle_status not in ('cancelled', 'archived')",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("ListMonitoringInstances SQL missing %q in %s", snippet, seenSQL)
		}
	}
}

func TestPostgresMonitoringInstanceListScopeFiltersArchiveState(t *testing.T) {
	tests := []struct {
		name          string
		scope         monitoringinstances.ListScope
		wantSnippet   string
		rejectSnippet string
	}{
		{
			name:        "archived",
			scope:       monitoringinstances.ListScopeArchived,
			wantSnippet: "archived_at is not null",
		},
		{
			name:          "all",
			scope:         monitoringinstances.ListScopeAll,
			rejectSnippet: "archived_at is",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seenSQL string
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
				query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
					seenSQL = sql
					return &fakeMonitoringInstanceRows{}, nil
				},
			}}

			if _, err := repo.ListMonitoringInstances(context.Background(), tt.scope); err != nil {
				t.Fatalf("ListMonitoringInstances() error = %v", err)
			}
			if tt.wantSnippet != "" && !strings.Contains(seenSQL, tt.wantSnippet) {
				t.Fatalf("ListMonitoringInstances SQL = %s, missing %q", seenSQL, tt.wantSnippet)
			}
			if tt.rejectSnippet != "" && strings.Contains(seenSQL, tt.rejectSnippet) {
				t.Fatalf("ListMonitoringInstances SQL = %s, should not contain %q", seenSQL, tt.rejectSnippet)
			}
		})
	}
}

func TestMonitoringInstanceManagementReviewBuildsCountsAndActions(t *testing.T) {
	t.Parallel()

	queryRows := make([]string, 0)
	rowCall := 0
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCall++
			if len(args) != 1 || args[0] != "mi_001" {
				t.Fatalf("QueryRow args = %#v, want mi_001", args)
			}
			queryRows = append(queryRows, sql)
			switch rowCall {
			case 1:
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_001",
						DisplayName:          "Tokyo Edge",
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						BindingStatus:        monitoringinstances.BindingBound,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
						CreatedAt:            time.Date(2026, time.June, 10, 8, 0, 0, 0, time.UTC),
						UpdatedAt:            time.Date(2026, time.June, 10, 8, 30, 0, 0, time.UTC),
					})
					return nil
				}}
			default:
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 1
					*(dest[1].(*int)) = 2
					*(dest[2].(*int)) = 3
					*(dest[3].(*int)) = 4
					*(dest[4].(*int)) = 5
					*(dest[5].(*int)) = 6
					*(dest[6].(*int)) = 7
					*(dest[7].(*int)) = 8
					*(dest[8].(*int)) = 9
					*(dest[9].(*int)) = 1
					return nil
				}}
			}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if len(args) != 1 || args[0] != "mi_001" {
				t.Fatalf("Query args = %#v, want mi_001", args)
			}
			queryRows = append(queryRows, sql)
			return &fakeMonitoringInstanceManagementLinkRows{rows: []monitoringinstances.ManagementVPSLink{{
				LinkID:          "vnl_001",
				VPSID:           "vps_001",
				DisplayName:     "Tokyo VPS",
				LifecycleStatus: "active",
				UsageStatus:     "in_use",
				LinkedAt:        time.Date(2026, time.June, 10, 8, 0, 0, 0, time.UTC),
				Note:            "primary",
			}}}, nil
		},
	}}

	review, err := repo.GetMonitoringInstanceManagementReview(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("GetMonitoringInstanceManagementReview() error = %v", err)
	}

	if review.Record.MonitoringInstanceID != "mi_001" {
		t.Fatalf("review record = %#v, want mi_001", review.Record)
	}
	if review.Counts.HeartbeatCount != 1 ||
		review.Counts.HostSampleCount != 2 ||
		review.Counts.ProbeObservationCount != 3 ||
		review.Counts.HostSampleDailyAggregateCount != 4 ||
		review.Counts.IPQualityReportCount != 5 ||
		review.Counts.ActiveIncidentCount != 6 ||
		review.Counts.StateChangeEventCount != 7 ||
		review.Counts.NotificationRecordCount != 8 ||
		review.Counts.AssetLifecycleActionStepCount != 9 ||
		review.Counts.ActiveVPSLinkCount != 1 {
		t.Fatalf("counts = %#v, want populated counts", review.Counts)
	}
	if len(review.ActiveVPSLinks) != 1 || review.ActiveVPSLinks[0].VPSID != "vps_001" {
		t.Fatalf("active links = %#v, want vps_001", review.ActiveVPSLinks)
	}
	if review.EmptyMistakeCandidate {
		t.Fatal("EmptyMistakeCandidate = true, want false for counted evidence")
	}
	if review.Actions.CanArchive {
		t.Fatalf("CanArchive = true, want false while active VPS blocker exists")
	}
	if review.Actions.CanPermanentCleanup {
		t.Fatalf("CanPermanentCleanup = true, want false for non-empty unarchived instance")
	}
	for _, snippet := range []string{
		"from monitoring_instance_heartbeats",
		"from host_samples",
		"from probe_observations",
		"from monitoring_instance_host_sample_daily_aggregates",
		"from ip_quality_reports",
		"from active_incidents",
		"from state_change_events",
		"from notification_records",
		"from asset_lifecycle_action_steps",
		"from vps_monitoring_instance_links",
	} {
		if !containsSQL(queryRows, snippet) {
			t.Fatalf("management review queries = %#v, missing %q", queryRows, snippet)
		}
	}
}

func TestMonitoringInstanceManagementReviewMarksEmptyMistakeCandidate(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "select "+monitoringInstanceSelectColumns) {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_empty",
						DisplayName:          "Empty Instance",
						LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
						MonitoringStatus:     monitoringinstances.MonitoringEnabled,
						BindingStatus:        monitoringinstances.BindingUnbound,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
					})
					return nil
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				for _, d := range dest {
					*(d.(*int)) = 0
				}
				return nil
			}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeMonitoringInstanceManagementLinkRows{}, nil
		},
	}}

	review, err := repo.GetMonitoringInstanceManagementReview(context.Background(), "mi_empty")
	if err != nil {
		t.Fatalf("GetMonitoringInstanceManagementReview() error = %v", err)
	}
	if !review.EmptyMistakeCandidate {
		t.Fatalf("EmptyMistakeCandidate = false, want true for zero-evidence instance")
	}
	if !review.Actions.CanPermanentCleanup {
		t.Fatalf("CanPermanentCleanup = false, want true for empty mistake candidate")
	}
}

func TestRetireMonitoringInstancePausesAndRevokesTokens(t *testing.T) {
	t.Parallel()

	var (
		updateSQL string
		eventSQL  string
		committed bool
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			updateSQL = sql
			if len(args) != 1 || args[0] != "mi_001" {
				t.Fatalf("retire args = %#v, want id", args)
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID: "mi_001",
					DisplayName:          "Tokyo Edge",
					LifecycleStatus:      monitoringinstances.LifecycleRetired,
					MonitoringStatus:     monitoringinstances.MonitoringPaused,
					BindingStatus:        monitoringinstances.BindingBound,
					CurrentHealthStatus:  monitoringinstances.HealthNormal,
				})
				return nil
			}}
		},
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			eventSQL = sql
			payload, ok := args[6].([]byte)
			if !ok || !strings.Contains(string(payload), `"reason":"duplicate"`) {
				t.Fatalf("retire event payload = %#v, want reason", args[6])
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	record, err := repo.RetireMonitoringInstance(context.Background(), "mi_001", monitoringinstances.LifecycleActionInput{Reason: " duplicate "})
	if err != nil {
		t.Fatalf("RetireMonitoringInstance() error = %v", err)
	}
	if record.LifecycleStatus != monitoringinstances.LifecycleRetired || record.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("record statuses = %q/%q, want retired/paused", record.LifecycleStatus, record.MonitoringStatus)
	}
	for _, snippet := range []string{
		"set lifecycle_status = '已退役'",
		"monitoring_status = '暂停'",
		"enrollment_token_hash = null",
		"enrollment_token_issued_at = null",
		"sync_token_hash = ''",
		"pending_action_id = null",
		"pending_action_command_id = null",
		"archived_at is null",
	} {
		if !strings.Contains(updateSQL, snippet) {
			t.Fatalf("retire SQL = %q, missing %q", updateSQL, snippet)
		}
	}
	if !strings.Contains(eventSQL, "insert into state_change_events") {
		t.Fatalf("event SQL = %q, want state_change_events insert", eventSQL)
	}
	if !committed {
		t.Fatal("RetireMonitoringInstance() did not commit")
	}
}

func TestRestoreMonitoringInstanceLifecycleReturnsToObservingPaused(t *testing.T) {
	t.Parallel()

	var updateSQL string
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			updateSQL = sql
			if len(args) != 1 || args[0] != "mi_001" {
				t.Fatalf("restore args = %#v, want id", args)
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID: "mi_001",
					LifecycleStatus:      monitoringinstances.LifecycleObserving,
					MonitoringStatus:     monitoringinstances.MonitoringPaused,
					CurrentHealthStatus:  monitoringinstances.HealthNormal,
				})
				return nil
			}}
		},
		exec: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			payload, ok := args[6].([]byte)
			if !ok || !strings.Contains(string(payload), `"reason":"restore"`) {
				t.Fatalf("restore event payload = %#v, want reason", args[6])
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	record, err := repo.RestoreMonitoringInstanceLifecycle(context.Background(), "mi_001", monitoringinstances.LifecycleActionInput{Reason: " restore "})
	if err != nil {
		t.Fatalf("RestoreMonitoringInstanceLifecycle() error = %v", err)
	}
	if record.LifecycleStatus != monitoringinstances.LifecycleObserving || record.MonitoringStatus != monitoringinstances.MonitoringPaused {
		t.Fatalf("record statuses = %q/%q, want observing/paused", record.LifecycleStatus, record.MonitoringStatus)
	}
	for _, snippet := range []string{
		"set lifecycle_status = '观察中'",
		"monitoring_status = '暂停'",
		"where monitoring_instance_id = $1",
		"lifecycle_status = '已退役'",
		"archived_at is null",
	} {
		if !strings.Contains(updateSQL, snippet) {
			t.Fatalf("restore SQL = %q, missing %q", updateSQL, snippet)
		}
	}
}

func TestArchiveMonitoringInstanceRequiresReviewWithoutBlockers(t *testing.T) {
	t.Parallel()

	var (
		queryRows []string
		updateSQL string
		call      int
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			call++
			queryRows = append(queryRows, sql)
			switch call {
			case 1:
				if len(args) != 1 || args[0] != "mi_001" {
					t.Fatalf("archive lock args = %#v, want mi_001", args)
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_001",
						DisplayName:          "Tokyo Edge",
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
					})
					return nil
				}}
			case 2:
				if len(args) != 1 || args[0] != "mi_001" {
					t.Fatalf("archive review args = %#v, want mi_001", args)
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					for _, d := range dest {
						*(d.(*int)) = 0
					}
					return nil
				}}
			default:
				updateSQL = sql
				if len(args) != 2 || args[0] != "mi_001" || args[1] != "duplicate" {
					t.Fatalf("archive update args = %#v, want id and reason", args)
				}
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					archivedAt := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC)
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_001",
						DisplayName:          "Tokyo Edge",
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
						ArchivedAt:           &archivedAt,
						ArchivedReason:       "duplicate",
					})
					return nil
				}}
			}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeMonitoringInstanceManagementLinkRows{}, nil
		},
		exec: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			payload, ok := args[6].([]byte)
			if !ok || !strings.Contains(string(payload), `"reason":"duplicate"`) {
				t.Fatalf("archive event payload = %#v, want reason", args[6])
			}
			return pgconn.NewCommandTag("INSERT 1"), nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	record, err := repo.ArchiveMonitoringInstance(context.Background(), "mi_001", monitoringinstances.ArchiveInput{Reason: " duplicate ", ConfirmationName: "Tokyo Edge"})
	if err != nil {
		t.Fatalf("ArchiveMonitoringInstance() error = %v", err)
	}
	if record.ArchivedAt == nil || record.ArchivedReason != "duplicate" {
		t.Fatalf("archive record = %#v, want archived reason", record)
	}
	for _, snippet := range []string{
		"for update",
		"from monitoring_instance_heartbeats",
	} {
		if !containsSQL(queryRows, snippet) {
			t.Fatalf("archive review queries = %#v, missing %q", queryRows, snippet)
		}
	}
	for _, snippet := range []string{
		"archived_at = now()",
		"archived_reason = $2",
		"monitoring_status = '暂停'",
		"enrollment_token_hash = null",
		"sync_token_hash = ''",
		"pending_action_id = null",
		"where monitoring_instance_id = $1",
		"archived_at is null",
	} {
		if !strings.Contains(updateSQL, snippet) {
			t.Fatalf("archive SQL = %q, missing %q", updateSQL, snippet)
		}
	}
}

func TestArchiveMonitoringInstanceBlocksActiveVPSLinks(t *testing.T) {
	t.Parallel()

	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "for update") {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_001",
						DisplayName:          "Tokyo Edge",
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
					})
					return nil
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				for _, d := range dest {
					*(d.(*int)) = 0
				}
				*(dest[9].(*int)) = 1
				return nil
			}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeMonitoringInstanceManagementLinkRows{rows: []monitoringinstances.ManagementVPSLink{{
				LinkID:          "vnl_001",
				VPSID:           "vps_001",
				DisplayName:     "Tokyo VPS",
				LifecycleStatus: "active",
				UsageStatus:     "in_use",
			}}}, nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	_, err := repo.ArchiveMonitoringInstance(context.Background(), "mi_001", monitoringinstances.ArchiveInput{Reason: "duplicate", ConfirmationName: "Tokyo Edge"})
	if !errors.Is(err, monitoringinstances.ErrManagementActionBlocked) {
		t.Fatalf("ArchiveMonitoringInstance() error = %v, want ErrManagementActionBlocked", err)
	}
}

func TestRestoreMonitoringInstanceFromArchiveClearsArchiveFields(t *testing.T) {
	t.Parallel()

	var updateSQL string
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			updateSQL = sql
			if len(args) != 1 || args[0] != "mi_001" {
				t.Fatalf("restore archive args = %#v, want mi_001", args)
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
					MonitoringInstanceID: "mi_001",
					LifecycleStatus:      monitoringinstances.LifecycleObserving,
					MonitoringStatus:     monitoringinstances.MonitoringPaused,
					CurrentHealthStatus:  monitoringinstances.HealthNormal,
				})
				return nil
			}}
		},
	}}

	record, err := repo.RestoreMonitoringInstanceFromArchive(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("RestoreMonitoringInstanceFromArchive() error = %v", err)
	}
	if record.ArchivedAt != nil || record.ArchivedReason != "" {
		t.Fatalf("restored archive fields = %v/%q, want nil/empty", record.ArchivedAt, record.ArchivedReason)
	}
	for _, snippet := range []string{
		"archived_at = null",
		"archived_reason = ''",
		"lifecycle_status = '观察中'",
		"monitoring_status = '暂停'",
		"where monitoring_instance_id = $1",
		"archived_at is not null",
	} {
		if !strings.Contains(updateSQL, snippet) {
			t.Fatalf("restore archive SQL = %q, missing %q", updateSQL, snippet)
		}
	}
}

func TestPermanentCleanupMonitoringInstanceDeletesEmptyMistakeCandidate(t *testing.T) {
	t.Parallel()

	var (
		queryRows []string
		execSQLs  []string
		call      int
		committed bool
	)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			call++
			queryRows = append(queryRows, sql)
			if len(args) != 1 || args[0] != "mi_empty" {
				t.Fatalf("cleanup query args = %#v, want mi_empty", args)
			}
			switch call {
			case 1:
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_empty",
						DisplayName:          "Mistake",
						LifecycleStatus:      monitoringinstances.LifecyclePendingEnrollment,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
					})
					return nil
				}}
			default:
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					for _, d := range dest {
						*(d.(*int)) = 0
					}
					return nil
				}}
			}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeMonitoringInstanceManagementLinkRows{rows: []monitoringinstances.ManagementVPSLink{{
				LinkID:          "vnl_001",
				VPSID:           "vps_001",
				DisplayName:     "Linked VPS",
				LifecycleStatus: "active",
				UsageStatus:     "in_use",
			}}}, nil
		},
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			execSQLs = append(execSQLs, sql)
			if len(args) != 1 || args[0] != "mi_empty" {
				t.Fatalf("cleanup exec args = %#v, want mi_empty", args)
			}
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
		commit: func(context.Context) error {
			committed = true
			return nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	result, err := repo.PermanentCleanupMonitoringInstance(context.Background(), "mi_empty", monitoringinstances.PermanentCleanupInput{Reason: "mistake", ConfirmationName: "Mistake"})
	if err != nil {
		t.Fatalf("PermanentCleanupMonitoringInstance() error = %v", err)
	}
	if !result.Deleted || result.MonitoringInstanceID != "mi_empty" {
		t.Fatalf("cleanup result = %#v, want deleted mi_empty", result)
	}
	if !containsSQL(queryRows, "for update") || !containsSQL(queryRows, "from monitoring_instance_heartbeats") {
		t.Fatalf("cleanup review queries = %#v, want lock and review", queryRows)
	}
	assertSQLOrder(t, execSQLs,
		"delete from notification_records",
		"delete from active_incidents",
		"delete from state_change_events",
		"delete from asset_lifecycle_action_steps",
		"delete from monitoring_instances",
	)
	if !committed {
		t.Fatal("PermanentCleanupMonitoringInstance() did not commit")
	}
}

func TestPermanentCleanupMonitoringInstanceBlocksNonEmptyUnarchivedInstance(t *testing.T) {
	t.Parallel()

	var execCalled bool
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "for update") {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_001",
						DisplayName:          "Tokyo Edge",
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
					})
					return nil
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				for _, d := range dest {
					*(d.(*int)) = 0
				}
				*(dest[0].(*int)) = 1
				return nil
			}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeMonitoringInstanceManagementLinkRows{}, nil
		},
		exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
			execCalled = true
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	_, err := repo.PermanentCleanupMonitoringInstance(context.Background(), "mi_001", monitoringinstances.PermanentCleanupInput{Reason: "cleanup", ConfirmationName: "Tokyo Edge"})
	if !errors.Is(err, monitoringinstances.ErrManagementActionBlocked) {
		t.Fatalf("PermanentCleanupMonitoringInstance() error = %v, want ErrManagementActionBlocked", err)
	}
	if execCalled {
		t.Fatal("PermanentCleanupMonitoringInstance() executed deletes for blocked instance")
	}
}

func TestPermanentCleanupMonitoringInstanceDeletesArchivedNonEmptyInstance(t *testing.T) {
	t.Parallel()

	var (
		execSQLs        []string
		deletedRowsSeen int64
	)
	archivedAt := time.Date(2026, time.June, 10, 9, 0, 0, 0, time.UTC)
	tx := &fakeMonitoringInstanceTx{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if strings.Contains(sql, "for update") {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
					scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
						MonitoringInstanceID: "mi_001",
						DisplayName:          "Tokyo Edge",
						LifecycleStatus:      monitoringinstances.LifecycleRetired,
						MonitoringStatus:     monitoringinstances.MonitoringPaused,
						CurrentHealthStatus:  monitoringinstances.HealthNormal,
						ArchivedAt:           &archivedAt,
						ArchivedReason:       "duplicate",
					})
					return nil
				}}
			}
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				for _, d := range dest {
					*(d.(*int)) = 0
				}
				*(dest[0].(*int)) = 2
				*(dest[7].(*int)) = 3
				return nil
			}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeMonitoringInstanceManagementLinkRows{}, nil
		},
		exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
			execSQLs = append(execSQLs, sql)
			tag := pgconn.NewCommandTag("DELETE 1")
			deletedRowsSeen += tag.RowsAffected()
			return tag, nil
		},
	}
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

	result, err := repo.PermanentCleanupMonitoringInstance(context.Background(), "mi_001", monitoringinstances.PermanentCleanupInput{Reason: "cleanup", ConfirmationName: "Tokyo Edge"})
	if err != nil {
		t.Fatalf("PermanentCleanupMonitoringInstance() error = %v", err)
	}
	if !result.Deleted || result.Counts.HeartbeatCount != 2 || result.Counts.NotificationRecordCount != 3 {
		t.Fatalf("cleanup result = %#v, want pre-delete counts", result)
	}
	if deletedRowsSeen == 0 {
		t.Fatal("cleanup did not execute delete command tags")
	}
	assertSQLOrder(t, execSQLs,
		"delete from notification_records",
		"delete from active_incidents",
		"delete from state_change_events",
		"delete from asset_lifecycle_action_steps",
		"delete from monitoring_instances",
	)
}

type fakeMonitoringInstanceManagementLinkRows struct {
	rows []monitoringinstances.ManagementVPSLink
	idx  int
}

func (r *fakeMonitoringInstanceManagementLinkRows) Close()     {}
func (r *fakeMonitoringInstanceManagementLinkRows) Err() error { return nil }
func (r *fakeMonitoringInstanceManagementLinkRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}
func (r *fakeMonitoringInstanceManagementLinkRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (r *fakeMonitoringInstanceManagementLinkRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}
func (r *fakeMonitoringInstanceManagementLinkRows) Scan(dest ...any) error {
	row := r.rows[r.idx-1]
	*(dest[0].(*string)) = row.LinkID
	*(dest[1].(*string)) = row.VPSID
	*(dest[2].(*string)) = row.DisplayName
	*(dest[3].(*string)) = row.LifecycleStatus
	*(dest[4].(*string)) = row.UsageStatus
	*(dest[5].(*time.Time)) = row.LinkedAt
	*(dest[6].(*string)) = row.Note
	return nil
}
func (r *fakeMonitoringInstanceManagementLinkRows) Values() ([]any, error) { return nil, nil }
func (r *fakeMonitoringInstanceManagementLinkRows) RawValues() [][]byte    { return nil }
func (r *fakeMonitoringInstanceManagementLinkRows) Conn() *pgx.Conn        { return nil }

type fakeMonitoringInstanceRows struct{}

func (r *fakeMonitoringInstanceRows) Close()                                       {}
func (r *fakeMonitoringInstanceRows) Err() error                                   { return nil }
func (r *fakeMonitoringInstanceRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeMonitoringInstanceRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeMonitoringInstanceRows) Next() bool                                   { return false }
func (r *fakeMonitoringInstanceRows) Scan(...any) error                            { return nil }
func (r *fakeMonitoringInstanceRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeMonitoringInstanceRows) RawValues() [][]byte                          { return nil }
func (r *fakeMonitoringInstanceRows) Conn() *pgx.Conn                              { return nil }

func sourceBetween(t *testing.T, source, start, end string) string {
	t.Helper()

	startIndex := strings.Index(source, start)
	if startIndex == -1 {
		t.Fatalf("source missing start marker %q", start)
	}
	section := source[startIndex:]
	if end == "" {
		return section
	}
	endIndex := strings.Index(section, end)
	if endIndex == -1 {
		t.Fatalf("source missing end marker %q", end)
	}
	return section[:endIndex]
}

func assertSQLOrder(t *testing.T, sqls []string, snippets ...string) {
	t.Helper()

	nextStart := 0
	for _, snippet := range snippets {
		found := -1
		for i := nextStart; i < len(sqls); i++ {
			if strings.Contains(sqls[i], snippet) {
				found = i
				break
			}
		}
		if found == -1 {
			t.Fatalf("SQL order missing %q after index %d in %#v", snippet, nextStart, sqls)
		}
		nextStart = found + 1
	}
}

type fakeMonitoringInstanceDB struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginTx  func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func (f fakeMonitoringInstanceDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeMonitoringInstanceRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

func (f fakeMonitoringInstanceDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return nil, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeMonitoringInstanceDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec == nil {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return f.exec(ctx, sql, args...)
}

func (f fakeMonitoringInstanceDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	if f.beginTx == nil {
		return &fakeMonitoringInstanceTx{
			queryRow: f.queryRow,
			exec:     f.exec,
		}, nil
	}
	return f.beginTx(ctx, txOptions)
}

type fakeMonitoringInstanceRow struct {
	scan func(dest ...any) error
}

func (r fakeMonitoringInstanceRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeMonitoringInstanceTx struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	commit   func(context.Context) error
	rollback func(context.Context) error
}

func (f *fakeMonitoringInstanceTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeMonitoringInstanceTx) Commit(ctx context.Context) error {
	if f.commit != nil {
		return f.commit(ctx)
	}
	return nil
}
func (f *fakeMonitoringInstanceTx) Rollback(ctx context.Context) error {
	if f.rollback != nil {
		return f.rollback(ctx)
	}
	return nil
}
func (f *fakeMonitoringInstanceTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeMonitoringInstanceTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f *fakeMonitoringInstanceTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f *fakeMonitoringInstanceTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeMonitoringInstanceTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec != nil {
		return f.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 1"), nil
}
func (f *fakeMonitoringInstanceTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query != nil {
		return f.query(ctx, sql, args...)
	}
	return nil, nil
}
func (f *fakeMonitoringInstanceTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return fakeMonitoringInstanceRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
}
func (f *fakeMonitoringInstanceTx) Conn() *pgx.Conn { return nil }

func scanMonitoringInstanceRecordDestinations(dest []any, record monitoringinstances.Record) {
	*(dest[0].(*string)) = record.MonitoringInstanceID
	*(dest[1].(*string)) = record.DisplayName
	*(dest[2].(*string)) = record.Group
	*(dest[3].(*string)) = record.Region
	*(dest[4].(*string)) = record.City
	*(dest[5].(*string)) = record.Provider
	*(dest[6].(*string)) = record.LifecycleStatus
	*(dest[7].(*string)) = record.MonitoringStatus
	*(dest[8].(*string)) = record.BindingStatus
	*(dest[9].(*string)) = record.EnrollmentTokenHash
	*(dest[10].(**time.Time)) = cloneTimePtr(record.EnrollmentTokenIssuedAt)
	*(dest[11].(*string)) = record.SyncTokenHash
	*(dest[12].(*string)) = record.BindingFingerprint
	*(dest[13].(**time.Time)) = cloneTimePtr(record.BindingEpochStartedAt)
	*(dest[14].(*string)) = record.PendingBindingFingerprint
	*(dest[15].(**time.Time)) = cloneTimePtr(record.PendingBindingFirstSeenAt)
	*(dest[16].(**time.Time)) = cloneTimePtr(record.PendingBindingLastSeenAt)
	*(dest[17].(*int)) = record.PendingBindingAttemptCount
	*(dest[18].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[19].(*string)) = record.Note
	*(dest[20].(*string)) = record.CurrentHealthStatus
	*(dest[21].(**time.Time)) = cloneTimePtr(record.LastHeartbeatAt)
	*(dest[22].(**time.Time)) = cloneTimePtr(record.LastSyncAt)
	*(dest[23].(*int)) = record.CurrentActiveIncidentCount
	*(dest[24].(*string)) = record.CurrentPrimaryIssueSummary
	// pending_action_id (25), pending_action_command_id (26), last_action (27)
	if record.LastAction != nil {
		*(dest[27].(*[]byte)) = []byte(record.LastActionRaw)
	}
	*(dest[28].(**time.Time)) = cloneTimePtr(record.ArchivedAt)
	*(dest[29].(*string)) = record.ArchivedReason
	*(dest[30].(*time.Time)) = record.CreatedAt
	*(dest[31].(*time.Time)) = record.UpdatedAt
}
func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
