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
	if !strings.Contains(gotSQL, "enrollment_token_issued_at = now()") {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() SQL = %q, want enrollment_token_issued_at update", gotSQL)
	}
	if !strings.Contains(gotSQL, "enrollment_token_consumed_at = null") {
		t.Fatalf("IssueMonitoringInstanceEnrollmentToken() SQL = %q, want consumed marker reset", gotSQL)
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
	if len(gotArgs) != 1 || gotArgs[0] != hashEnrollmentToken("enroll_001") {
		t.Fatalf("QueryRow args = %#v, want enrollment token hash only", gotArgs)
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
					*(dest[1].(*string)) = monitoringinstances.BindingUnbound
					*(dest[2].(*string)) = ""
					*(dest[3].(**time.Time)) = nil
					*(dest[4].(*string)) = ""
					*(dest[5].(**time.Time)) = nil
					*(dest[6].(**time.Time)) = nil
					*(dest[7].(*int)) = 0
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
	if len(selectArgs) != 1 || selectArgs[0] != hashEnrollmentToken("enroll_001") {
		t.Fatalf("select args = %#v, want enrollment hash", selectArgs)
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
	if len(updateArgs) != 9 {
		t.Fatalf("ApplyEnrollment update args = %#v, want 9 args", updateArgs)
	}
	if updateArgs[8] != hashSyncToken(syncToken) {
		t.Fatalf("ApplyEnrollment sync hash arg = %#v, want hash of returned token", updateArgs[8])
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
				*(dest[30].(*bool)) = true
				*(dest[31].(*bool)) = false
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
				*(dest[30].(*bool)) = false
				*(dest[31].(*bool)) = false
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

func TestUpdateMonitoringInstanceMetadataMapsNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
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
				if !strings.Contains(sql, "select exists") || !strings.Contains(sql, "from monitoring_instances") {
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
	if queryCount != 2 {
		t.Fatalf("QueryRow calls = %d, want 2", queryCount)
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
	if !strings.Contains(heartbeatPath, "storedSyncTokenHash != hashSyncToken(syncToken)") {
		t.Fatal("RecordAcceptedHeartbeats() should reject mismatched sync token hashes")
	}
	if !strings.Contains(heartbeatPath, "write.Fingerprint != bindingFingerprint") {
		t.Fatal("RecordAcceptedHeartbeats() should continue validating fingerprint within the transaction")
	}
}

func TestMonitoringInstanceLifecycleTransitionsWriteEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		method           func(*PostgresMonitoringInstanceRepository, context.Context, string) (monitoringinstances.Record, error)
		initialLifecycle string
		returnLifecycle  string
		wantEventType    incidents.EventType
		wantSummary      string
		wantSQLSnippet   string
	}{
		{
			name:             "retire",
			method:           (*PostgresMonitoringInstanceRepository).RetireMonitoringInstance,
			initialLifecycle: monitoringinstances.LifecycleInUse,
			returnLifecycle:  monitoringinstances.LifecycleRetired,
			wantEventType:    incidents.EventMonitoringInstanceRetired,
			wantSummary:      "监控实例已退役并退出活跃观测集，历史记录保留",
			wantSQLSnippet:   "lifecycle_status <> '已退役'",
		},
		{
			name:             "restore retired to observing",
			method:           (*PostgresMonitoringInstanceRepository).RestoreRetiredMonitoringInstanceToObserving,
			initialLifecycle: monitoringinstances.LifecycleRetired,
			returnLifecycle:  monitoringinstances.LifecycleObserving,
			wantEventType:    incidents.EventMonitoringInstanceRestoredToObserving,
			wantSummary:      "监控实例已从退役恢复到观察中",
			wantSQLSnippet:   "lifecycle_status = '已退役'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotSQL string
			var execSQL string
			var execArgs []any
			committed := false
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					return &fakeMonitoringInstanceTx{
						queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
							gotSQL = sql
							if len(args) != 1 || args[0] != "mi_001" {
								t.Fatalf("QueryRow args = %#v, want monitoringInstance id", args)
							}
							return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
								scanMonitoringInstanceRecordDestinations(dest, monitoringinstances.Record{
									MonitoringInstanceID: "mi_001",
									DisplayName:          "Tokyo Edge",
									LifecycleStatus:      tt.returnLifecycle,
									MonitoringStatus:     monitoringinstances.MonitoringEnabled,
									BindingStatus:        monitoringinstances.BindingBound,
								})
								return nil
							}}
						},
						exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
							execSQL = sql
							execArgs = append([]any(nil), args...)
							return pgconn.NewCommandTag("INSERT 0 1"), nil
						},
						commit: func(context.Context) error {
							committed = true
							return nil
						},
					}, nil
				},
			}}

			got, err := tt.method(repo, context.Background(), "mi_001")
			if err != nil {
				t.Fatalf("%s error = %v", tt.name, err)
			}
			if got.LifecycleStatus != tt.returnLifecycle {
				t.Fatalf("LifecycleStatus = %q, want %q", got.LifecycleStatus, tt.returnLifecycle)
			}
			if !strings.Contains(gotSQL, tt.wantSQLSnippet) {
				t.Fatalf("transition SQL = %q, want snippet %q", gotSQL, tt.wantSQLSnippet)
			}
			if !strings.Contains(execSQL, "insert into state_change_events") {
				t.Fatalf("event SQL = %q, want state_change_events insert", execSQL)
			}
			if len(execArgs) < 8 {
				t.Fatalf("event args = %#v, want full insert args", execArgs)
			}
			if execArgs[1] != string(incidents.ObjectTypeMonitoringInstance) {
				t.Fatalf("event object_type arg = %#v, want monitoringInstance", execArgs[1])
			}
			if execArgs[2] != "mi_001" {
				t.Fatalf("event object_id arg = %#v, want mi_001", execArgs[2])
			}
			if execArgs[3] != string(tt.wantEventType) {
				t.Fatalf("event type arg = %#v, want %q", execArgs[3], tt.wantEventType)
			}
			if execArgs[5] != tt.wantSummary {
				t.Fatalf("summary arg = %#v, want %q", execArgs[5], tt.wantSummary)
			}
			payload, ok := execArgs[6].([]byte)
			if !ok || !strings.Contains(string(payload), `"lifecycle_status":"`+tt.returnLifecycle+`"`) {
				t.Fatalf("payload arg = %#v, want lifecycle status", execArgs[6])
			}
			if !committed {
				t.Fatal("transaction was not committed")
			}
		})
	}
}

func TestMonitoringInstanceLifecycleRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method func(*PostgresMonitoringInstanceRepository, context.Context, string) (monitoringinstances.Record, error)
	}{
		{
			name:   "retire already retired monitoringInstance",
			method: (*PostgresMonitoringInstanceRepository).RetireMonitoringInstance,
		},
		{
			name:   "restore non-retired monitoringInstance",
			method: (*PostgresMonitoringInstanceRepository).RestoreRetiredMonitoringInstanceToObserving,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
				beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					return &fakeMonitoringInstanceTx{
						queryRow: func(context.Context, string, ...any) pgx.Row {
							return fakeMonitoringInstanceRow{scan: func(...any) error { return pgx.ErrNoRows }}
						},
					}, nil
				},
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
						*(dest[0].(*bool)) = true
						return nil
					}}
				},
			}}

			_, err := tt.method(repo, context.Background(), "mi_001")
			if !errors.Is(err, ErrInvalidMonitoringInstanceLifecycleTransition) {
				t.Fatalf("%s error = %v, want ErrInvalidMonitoringInstanceLifecycleTransition", tt.name, err)
			}
			if errors.Is(err, ErrInvalidMonitoringInstanceRuntimeTransition) {
				t.Fatalf("%s error = %v, must not use runtime transition sentinel", tt.name, err)
			}
		})
	}
}

func TestMonitoringInstanceLifecycleReturnsNotFoundForMissingMonitoringInstance(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return &fakeMonitoringInstanceTx{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeMonitoringInstanceRow{scan: func(...any) error { return pgx.ErrNoRows }}
				},
			}, nil
		},
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}

	_, err := repo.RestoreRetiredMonitoringInstanceToObserving(context.Background(), "mi_missing")
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("RestoreRetiredMonitoringInstanceToObserving() error = %v, want monitoringinstances.ErrMonitoringInstanceNotFound", err)
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
						if len(dest) > 30 {
							*(dest[30].(*string)) = tt.sourceStatus
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
				*(dest[30].(*string)) = monitoringInstanceMonitoringStatusMaintenance
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
}

func TestMonitoringInstanceRuntimeControlRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeMonitoringInstanceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return &fakeMonitoringInstanceTx{queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
				return fakeMonitoringInstanceRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}}

	_, err := repo.SetMonitoringInstanceMonitoringMaintenance(context.Background(), "mi_paused")
	if !errors.Is(err, ErrInvalidMonitoringInstanceRuntimeTransition) {
		t.Fatalf("SetMonitoringInstanceMonitoringMaintenance() error = %v, want ErrInvalidMonitoringInstanceRuntimeTransition", err)
	}
}

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
func (f *fakeMonitoringInstanceTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
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
	*(dest[28].(*time.Time)) = record.CreatedAt
	*(dest[29].(*time.Time)) = record.UpdatedAt
}
func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
