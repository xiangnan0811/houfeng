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

	"houfeng/internal/center/nodes"
)

func TestBindingTransitionStoresPendingFingerprintMetadataOnCollision(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 26, 7, 30, 0, 0, time.UTC)

	record := resolveEnrollmentBindingTransition(nodes.Record{
		BindingStatus:      nodes.BindingBound,
		BindingFingerprint: "fp-old",
	}, "fp-new", now)

	if record.BindingStatus != nodes.BindingPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, nodes.BindingPendingConfirmation)
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

	record := resolveEnrollmentBindingTransition(nodes.Record{
		BindingStatus:              nodes.BindingPendingConfirmation,
		BindingFingerprint:         "fp-active",
		PendingBindingFingerprint:  "fp-pending",
		PendingBindingFirstSeenAt:  &firstSeenAt,
		PendingBindingLastSeenAt:   &firstSeenAt,
		PendingBindingAttemptCount: 2,
	}, "fp-pending", now)

	if record.BindingStatus != nodes.BindingPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, nodes.BindingPendingConfirmation)
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

func TestNodeOnboardingPhaseDerivation(t *testing.T) {
	t.Parallel()

	heartbeatAt := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name                   string
		record                 nodes.Record
		hasHostSample          bool
		hasAcceptedObservation bool
		want                   string
	}{
		{
			name: "unbound nodes have not started onboarding",
			record: nodes.Record{
				BindingStatus: nodes.BindingUnbound,
			},
			want: nodes.OnboardingPhaseNotStarted,
		},
		{
			name: "bound nodes without runtime facts await stable observation",
			record: nodes.Record{
				BindingStatus: nodes.BindingBound,
			},
			want: nodes.OnboardingPhaseBoundAwaitingObservation,
		},
		{
			name: "bound nodes with heartbeat and host sample are completed",
			record: nodes.Record{
				BindingStatus:   nodes.BindingBound,
				LastHeartbeatAt: &heartbeatAt,
			},
			hasHostSample: true,
			want:          nodes.OnboardingPhaseCompleted,
		},
		{
			name: "pending confirmation surfaces binding conflict phase",
			record: nodes.Record{
				BindingStatus:             nodes.BindingPendingConfirmation,
				PendingBindingFingerprint: "fp-pending",
			},
			want: nodes.OnboardingPhaseBindingConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := nodes.DeriveOnboardingPhase(tt.record, tt.hasHostSample, tt.hasAcceptedObservation)
			if got != tt.want {
				t.Fatalf("DeriveOnboardingPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNodeOnboardingMigrationAddsPersistenceColumns(t *testing.T) {
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

func TestNodeOnboardingIssueEnrollmentTokenStoresIssuedAt(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 8, 15, 0, 0, time.UTC)
	var (
		gotSQL  string
		gotArgs []any
	)
	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			gotArgs = append([]any(nil), args...)
			return fakeNodeRow{scan: func(dest ...any) error {
				*(dest[0].(*time.Time)) = issuedAt
				return nil
			}}
		},
	}}

	result, err := repo.IssueNodeEnrollmentToken(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("IssueNodeEnrollmentToken() error = %v", err)
	}

	if result.Token == "" {
		t.Fatal("IssueNodeEnrollmentToken().Token = empty, want generated plaintext token")
	}
	if !result.IssuedAt.Equal(issuedAt) {
		t.Fatalf("IssueNodeEnrollmentToken().IssuedAt = %s, want %s", result.IssuedAt.Format(time.RFC3339), issuedAt.Format(time.RFC3339))
	}
	if len(gotArgs) != 2 {
		t.Fatalf("len(gotArgs) = %d, want 2", len(gotArgs))
	}
	if gotArgs[0] != "nd_001" {
		t.Fatalf("gotArgs[0] = %#v, want nd_001", gotArgs[0])
	}
	if gotArgs[1] != hashEnrollmentToken(result.Token) {
		t.Fatalf("gotArgs[1] = %#v, want enrollment token hash", gotArgs[1])
	}
	if !strings.Contains(gotSQL, "enrollment_token_issued_at = now()") {
		t.Fatalf("IssueNodeEnrollmentToken() SQL = %q, want enrollment_token_issued_at update", gotSQL)
	}
}

func TestNodeOnboardingGetStateReturnsDerivedPhaseAndPendingMetadata(t *testing.T) {
	t.Parallel()

	issuedAt := time.Date(2026, time.April, 26, 8, 20, 0, 0, time.UTC)
	firstSeenAt := issuedAt.Add(5 * time.Minute)
	lastSeenAt := firstSeenAt.Add(2 * time.Minute)
	heartbeatAt := issuedAt.Add(10 * time.Minute)
	var gotSQL string
	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			if len(args) != 1 || args[0] != "nd_001" {
				t.Fatalf("QueryRow args = %#v, want node id", args)
			}
			return fakeNodeRow{scan: func(dest ...any) error {
				scanNodeRecordDestinations(dest, nodes.Record{
					NodeID:                     "nd_001",
					DisplayName:                "Node 001",
					LifecycleStatus:            nodes.LifecyclePendingEnrollment,
					MonitoringStatus:           nodes.MonitoringEnabled,
					BindingStatus:              nodes.BindingPendingConfirmation,
					BindingFingerprint:         "fp-active",
					PendingBindingFingerprint:  "fp-pending",
					PendingBindingFirstSeenAt:  &firstSeenAt,
					PendingBindingLastSeenAt:   &lastSeenAt,
					PendingBindingAttemptCount: 4,
					EnrollmentTokenIssuedAt:    &issuedAt,
					CurrentHealthStatus:        nodes.HealthNormal,
					LastHeartbeatAt:            &heartbeatAt,
				})
				*(dest[25].(*bool)) = true
				*(dest[26].(*bool)) = false
				return nil
			}}
		},
	}}

	state, err := repo.GetNodeOnboarding(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("GetNodeOnboarding() error = %v", err)
	}

	if state.Phase != nodes.OnboardingPhaseBindingConflict {
		t.Fatalf("Phase = %q, want %q", state.Phase, nodes.OnboardingPhaseBindingConflict)
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
		t.Fatalf("GetNodeOnboarding() SQL = %q, want pending binding columns", gotSQL)
	}
	if !strings.Contains(gotSQL, "from host_samples hs") {
		t.Fatalf("GetNodeOnboarding() SQL = %q, want host sample existence check", gotSQL)
	}
}

func TestNodeOnboardingGetStateScopesEvidenceToActiveFingerprint(t *testing.T) {
	t.Parallel()

	staleHeartbeatAt := time.Date(2026, time.April, 26, 6, 0, 0, 0, time.UTC)
	var gotSQL string
	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			return fakeNodeRow{scan: func(dest ...any) error {
				scanNodeRecordDestinations(dest, nodes.Record{
					NodeID:             "nd_rebind",
					BindingStatus:      nodes.BindingBound,
					BindingFingerprint: "fp-current",
					LastHeartbeatAt:    &staleHeartbeatAt,
				})
				*(dest[25].(*bool)) = false
				*(dest[26].(*bool)) = false
				return nil
			}}
		},
	}}

	state, err := repo.GetNodeOnboarding(context.Background(), "nd_rebind")
	if err != nil {
		t.Fatalf("GetNodeOnboarding() error = %v", err)
	}

	if state.Phase != nodes.OnboardingPhaseBoundAwaitingObservation {
		t.Fatalf("Phase = %q, want %q", state.Phase, nodes.OnboardingPhaseBoundAwaitingObservation)
	}
	if !strings.Contains(gotSQL, "hs.fingerprint = nodes.binding_fingerprint") {
		t.Fatalf("GetNodeOnboarding() SQL = %q, want host sample fingerprint scope", gotSQL)
	}
	if !strings.Contains(gotSQL, "po.fingerprint = nodes.binding_fingerprint") {
		t.Fatalf("GetNodeOnboarding() SQL = %q, want probe observation fingerprint scope", gotSQL)
	}
}

func TestBindingConfirmRebindMovesPendingFingerprintIntoActiveBinding(t *testing.T) {
	t.Parallel()

	var gotSQL string
	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			if len(args) != 1 || args[0] != "nd_002" {
				t.Fatalf("QueryRow args = %#v, want node id", args)
			}
			return fakeNodeRow{scan: func(dest ...any) error {
				scanNodeRecordDestinations(dest, nodes.Record{
					NodeID:             "nd_002",
					BindingStatus:      nodes.BindingBound,
					BindingFingerprint: "fp-pending",
				})
				return nil
			}}
		},
	}}

	record, err := repo.ConfirmNodeRebind(context.Background(), "nd_002")
	if err != nil {
		t.Fatalf("ConfirmNodeRebind() error = %v", err)
	}

	if record.BindingStatus != nodes.BindingBound {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, nodes.BindingBound)
	}
	if record.BindingFingerprint != "fp-pending" {
		t.Fatalf("BindingFingerprint = %q, want %q", record.BindingFingerprint, "fp-pending")
	}
	for _, snippet := range []string{
		"binding_fingerprint = pending_binding_fingerprint",
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
			t.Fatalf("ConfirmNodeRebind() SQL missing %q", snippet)
		}
	}
}

func TestBindingConfirmRebindRejectsRetryWithoutPendingFingerprint(t *testing.T) {
	t.Parallel()

	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeNodeRow{scan: func(dest ...any) error {
				return pgx.ErrNoRows
			}}
		},
	}}

	_, err := repo.ConfirmNodeRebind(context.Background(), "nd_retry")
	if !errors.Is(err, nodes.ErrInvalidBindingTransition) {
		t.Fatalf("ConfirmNodeRebind() error = %v, want ErrInvalidBindingTransition", err)
	}
}

func TestBindingRejectPendingClearsPendingMetadataAndKeepsActiveBinding(t *testing.T) {
	t.Parallel()

	var gotSQL string
	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			return fakeNodeRow{scan: func(dest ...any) error {
				scanNodeRecordDestinations(dest, nodes.Record{
					NodeID:                     "nd_003",
					BindingStatus:              nodes.BindingBound,
					BindingFingerprint:         "fp-active",
					PendingBindingAttemptCount: 0,
				})
				return nil
			}}
		},
	}}

	record, err := repo.RejectPendingFingerprint(context.Background(), "nd_003")
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
}

func TestBindingRejectPendingRejectsWhenNoPendingFingerprintExists(t *testing.T) {
	t.Parallel()

	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeNodeRow{scan: func(dest ...any) error {
				return pgx.ErrNoRows
			}}
		},
	}}

	_, err := repo.RejectPendingFingerprint(context.Background(), "nd_retry")
	if !errors.Is(err, nodes.ErrInvalidBindingTransition) {
		t.Fatalf("RejectPendingFingerprint() error = %v, want ErrInvalidBindingTransition", err)
	}
}

func TestBindingResetClearsActiveAndPendingBindingState(t *testing.T) {
	t.Parallel()

	var gotSQL string
	repo := &PostgresNodeRepository{db: fakeNodeDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			gotSQL = sql
			return fakeNodeRow{scan: func(dest ...any) error {
				scanNodeRecordDestinations(dest, nodes.Record{
					NodeID:        "nd_004",
					BindingStatus: nodes.BindingUnbound,
				})
				return nil
			}}
		},
	}}

	record, err := repo.ResetNodeBinding(context.Background(), "nd_004")
	if err != nil {
		t.Fatalf("ResetNodeBinding() error = %v", err)
	}

	if record.BindingStatus != nodes.BindingUnbound {
		t.Fatalf("BindingStatus = %q, want %q", record.BindingStatus, nodes.BindingUnbound)
	}
	for _, snippet := range []string{
		"binding_status = '未绑定'",
		"binding_fingerprint = ''",
		"pending_binding_fingerprint = null",
		"pending_binding_attempt_count = 0",
		"sync_token_hash = ''",
		"last_heartbeat_at = null",
		"last_sync_at = null",
	} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("ResetNodeBinding() SQL missing %q", snippet)
		}
	}
}

func TestApplyEnrollmentDoesNotAdvanceLastSyncAt(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("nodes.go")
	if err != nil {
		t.Fatalf("ReadFile(nodes.go) error = %v", err)
	}

	applyEnrollment := sourceBetween(t, string(source), "func (r *PostgresNodeRepository) ApplyEnrollment", "func (r *PostgresNodeRepository) RecordAcceptedHeartbeats")
	if strings.Contains(applyEnrollment, "last_sync_at") {
		t.Fatalf("ApplyEnrollment() unexpectedly updates last_sync_at:\n%s", applyEnrollment)
	}
	if !strings.Contains(applyEnrollment, "binding_status = $2") {
		t.Fatal("ApplyEnrollment() source no longer contains the enrollment binding update")
	}

	heartbeatPath := sourceBetween(t, string(source), "func (r *PostgresNodeRepository) RecordAcceptedHeartbeats", "")
	if !strings.Contains(heartbeatPath, "last_sync_at = greatest(coalesce(last_sync_at, $3), $3)") {
		t.Fatal("RecordAcceptedHeartbeats() should remain the path that advances last_sync_at")
	}
}

func TestStoreSourceIncludesSyncTokenValidationForHeartbeatWrites(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("nodes.go")
	if err != nil {
		t.Fatalf("ReadFile(nodes.go) error = %v", err)
	}

	heartbeatPath := sourceBetween(t, string(source), "func (r *PostgresNodeRepository) RecordAcceptedHeartbeats", "")
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

type fakeNodeDB struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginTx  func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func (f fakeNodeDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeNodeRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

func (f fakeNodeDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return nil, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeNodeDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec == nil {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return f.exec(ctx, sql, args...)
}

func (f fakeNodeDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	if f.beginTx == nil {
		return nil, nil
	}
	return f.beginTx(ctx, txOptions)
}

type fakeNodeRow struct {
	scan func(dest ...any) error
}

func (r fakeNodeRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

func scanNodeRecordDestinations(dest []any, record nodes.Record) {
	*(dest[0].(*string)) = record.NodeID
	*(dest[1].(*string)) = record.DisplayName
	*(dest[2].(*string)) = record.Region
	*(dest[3].(*string)) = record.City
	*(dest[4].(*string)) = record.Provider
	*(dest[5].(*string)) = record.LifecycleStatus
	*(dest[6].(*string)) = record.MonitoringStatus
	*(dest[7].(*string)) = record.BindingStatus
	*(dest[8].(*string)) = record.EnrollmentTokenHash
	*(dest[9].(**time.Time)) = cloneTimePtr(record.EnrollmentTokenIssuedAt)
	*(dest[10].(*string)) = record.SyncTokenHash
	*(dest[11].(*string)) = record.BindingFingerprint
	*(dest[12].(*string)) = record.PendingBindingFingerprint
	*(dest[13].(**time.Time)) = cloneTimePtr(record.PendingBindingFirstSeenAt)
	*(dest[14].(**time.Time)) = cloneTimePtr(record.PendingBindingLastSeenAt)
	*(dest[15].(*int)) = record.PendingBindingAttemptCount
	*(dest[16].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[17].(*string)) = record.Note
	*(dest[18].(*string)) = record.CurrentHealthStatus
	*(dest[19].(**time.Time)) = cloneTimePtr(record.LastHeartbeatAt)
	*(dest[20].(**time.Time)) = cloneTimePtr(record.LastSyncAt)
	*(dest[21].(*int)) = record.CurrentActiveIncidentCount
	*(dest[22].(*string)) = record.CurrentPrimaryIssueSummary
	*(dest[23].(*time.Time)) = record.CreatedAt
	*(dest[24].(*time.Time)) = record.UpdatedAt
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
