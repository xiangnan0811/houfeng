package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestEvidenceComparisonCandidatePostgresListsMatchingSubjectsInOneQuery(t *testing.T) {
	ctx := context.Background()
	fixture := newComparisonCandidateFixture(t, ctx, true)

	refs, err := fixture.repository.ListComparisonCandidateRefs(
		ctx, []evidence.ComparisonSubjectRef{fixture.left, fixture.right},
		fixture.window, []evidence.KindKey{evidence.MonitoringHostV1Key()},
	)
	if err != nil {
		t.Fatalf("ListComparisonCandidateRefs() error = %v", err)
	}
	got := comparisonCandidateSnapshotIDs(refs)
	if !containsAllComparisonSnapshotIDs(got, "evs_cmpleft", "evs_cmpright") {
		t.Fatalf("host candidates = %#v, want left and right overlapping host snapshots", got)
	}
	if containsComparisonSnapshotID(got, "evs_cmpold") || containsComparisonSnapshotID(got, "evs_cmpcmd") {
		t.Fatalf("candidates leaked out-of-window or filtered kind: %#v", got)
	}

	identityRefs, err := fixture.repository.ListComparisonCandidateRefs(
		ctx, []evidence.ComparisonSubjectRef{fixture.vps, fixture.right},
		fixture.window, []evidence.KindKey{evidence.MonitoringHostV1Key()},
	)
	if err != nil {
		t.Fatalf("ListComparisonCandidateRefs(vps identity) error = %v", err)
	}
	if !containsComparisonSnapshotID(comparisonCandidateSnapshotIDs(identityRefs), "evs_cmpvpsid") {
		t.Fatalf("identity candidates = %#v, want subject Type/ID match evs_cmpvpsid", comparisonCandidateSnapshotIDs(identityRefs))
	}
}

type comparisonCandidateFixture struct {
	signed     ImportAttachmentGCFixture
	repository *PostgresEvidenceRepository
	window     evidence.TimeWindow
	left       evidence.ComparisonSubjectRef
	right      evidence.ComparisonSubjectRef
	vps        evidence.ComparisonSubjectRef
}

// Current snapshots use the real capture, payload and revision publication path.
// The separate VPS-subject/instance-source row is historical: today's source
// resolver makes those identities equal, so it is installed before C81.
func newComparisonCandidateFixture(t *testing.T, ctx context.Context, includeHistorical bool) comparisonCandidateFixture {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	left := evidence.ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcdef"}
	right := evidence.ComparisonSubjectRef{Kind: "monitoring_instance", ID: "mi_0123456789abcde0"}
	vps := evidence.ComparisonSubjectRef{Kind: "vps", ID: "vps_0123456789abcdef"}
	var beforeC81 func(context.Context, *pgx.Conn, recordplatform.DeploymentID) error
	if includeHistorical {
		snapshot := comparisonHistoricalSnapshot(t, window, left, vps)
		beforeC81 = func(ctx context.Context, conn *pgx.Conn, _ recordplatform.DeploymentID) error {
			return seedHistoricalComparisonCandidate(ctx, conn, snapshot)
		}
	}
	signed := newImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false, beforeC81)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	if !includeHistorical {
		insertRuntimeFactsMonitoringInstance(t, ctx, signed.platform.db, left.ID, "comparison-fingerprint", now.Add(-4*time.Hour))
	}
	insertRuntimeFactsMonitoringInstance(t, ctx, signed.platform.db, right.ID, "comparison-fingerprint", now.Add(-4*time.Hour))
	insertRuntimeFactsHostSample(t, ctx, signed.platform.db, left.ID, window.Start.Add(10*time.Minute), window.Start.Add(10*time.Minute+time.Second), "comparison-fingerprint", 20, 1, false, "comparison-left")
	insertRuntimeFactsHostSample(t, ctx, signed.platform.db, right.ID, window.Start.Add(20*time.Minute), window.Start.Add(20*time.Minute+time.Second), "comparison-fingerprint", 30, 1, false, "comparison-right")
	if includeHistorical {
		old := window.Start.Add(-150 * time.Minute)
		insertRuntimeFactsHostSample(t, ctx, signed.platform.db, left.ID, old, old.Add(time.Second), "comparison-fingerprint", 10, 1, false, "comparison-old")
		if _, err := signed.platform.db.Exec(ctx, `insert into public.monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, monitoring_instance_name_snapshot, command_id,
			sensitivity, event_type, actor_user_id, actor_username_snapshot, actor_display_name_snapshot,
			source, exit_code, occurred_at, details
		) values ('aud_comparison', 'act_comparison', $1, 'Comparison instance', 'uptime',
			'standard', 'queued', null, 'Operator', 'Operator', 'web', null, $2, '{}'::jsonb)`, left.ID, window.Start.Add(30*time.Minute)); err != nil {
			t.Fatalf("seed comparison command source: %v", err)
		}
	}

	live := transactionRecordSubjectSource{db: signed.Runtime}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live), newMonitoringInstanceRecordSubjectAdapter(live), newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		t.Fatal(err)
	}
	witness, err := NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, signed.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	readResolver := NewRecordSubjectReadResolver(subjects, witness)
	current := NewPostgresCurrentRecordAuthorizationSource(signed.Runtime, readResolver, signed.Gate)
	sourceResolver, err := adapters.NewRecordEvidenceSourceResolver(subjects)
	if err != nil {
		t.Fatal(err)
	}
	options := adapters.AdapterOptions{Clock: func() time.Time { return now }}
	host, err := adapters.NewMonitoringHostAdapter(&PostgresRuntimeFactsRepository{db: signed.Runtime}, sourceResolver, options)
	if err != nil {
		t.Fatal(err)
	}
	commandAudit, err := adapters.NewCommandAuditAdapter(NewPostgresCommandAuditRepository(signed.Runtime), sourceResolver, options)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := evidence.NewRegistry([]evidence.Kind{host, commandAudit})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewPostgresEvidenceRepositoryWithReadSources(signed.Runtime, signed.Gate, registry, current, readResolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := evidence.NewCapacityEnforcer(evidence.DefaultCapacityPolicy(), repository)
	if err != nil {
		t.Fatal(err)
	}
	service, err := evidence.NewService(registry, repository, repository, capacity)
	if err != nil {
		t.Fatal(err)
	}
	preparer, err := evidence.NewRevisionPreparer(registry, repository, repository, repository, capacity)
	if err != nil {
		t.Fatal(err)
	}
	revisions := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant())
	first, err := revisions.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, signed,
		recordsPostgresCompleteRevisionInput(t, "Comparison candidates"), "comparison-candidates-create"))
	if err != nil {
		t.Fatalf("create comparison record: %v", err)
	}
	actor := storeEvidenceParticipantActor(t)
	type captureCase struct {
		snapshotID string
		selection  evidence.Selection
	}
	hostSelection := func(source string, selectedWindow evidence.TimeWindow) evidence.Selection {
		return evidence.Selection{Key: evidence.MonitoringHostV1Key(), SourceType: left.Kind, SourceID: source,
			RequestedWindow: selectedWindow, Metrics: []string{"mem_used_pct"}, Precision: time.Minute}
	}
	captures := []captureCase{
		{snapshotID: "evs_cmpleft", selection: hostSelection(left.ID, window)},
		{snapshotID: "evs_cmpright", selection: hostSelection(right.ID, window)},
	}
	if includeHistorical {
		captures = append(captures,
			captureCase{snapshotID: "evs_cmpold", selection: hostSelection(left.ID, evidence.TimeWindow{Start: window.Start.Add(-3 * time.Hour), End: window.Start.Add(-2 * time.Hour)})},
			captureCase{snapshotID: "evs_cmpcmd", selection: evidence.Selection{Key: evidence.CommandAuditV1Key(), SourceType: left.Kind, SourceID: left.ID, RequestedWindow: window}},
		)
	}
	items := make([]evidence.RevisionPreparationItem, 0, len(captures))
	for _, capture := range captures {
		preview, err := service.CapturePreview(ctx, evidence.CapturePreviewRequest{
			Actor: actor, RecordID: first.RecordID, SnapshotID: capture.snapshotID, Selection: capture.selection,
		})
		if err != nil {
			t.Fatalf("preview comparison snapshot %s: %v", capture.snapshotID, err)
		}
		items = append(items, evidence.RevisionPreparationItem{CaptureIntentID: preview.Preview.IntentID})
	}
	preparation, err := preparer.Prepare(ctx, actor, evidence.RevisionPreparationRequest{RecordID: first.RecordID, Items: items})
	if err != nil {
		t.Fatalf("prepare comparison evidence: %v", err)
	}
	command := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordUpdate, first.RecordID, first.RevisionID,
		first.LockVersion, first.AuthorizationEpoch, "Published comparison evidence", "comparison-candidates-publish", preparation)
	if _, err := revisions.CommitRevision(ctx, command); err != nil {
		t.Fatalf("publish comparison evidence: %v", err)
	}
	return comparisonCandidateFixture{signed: signed, repository: repository, window: window, left: left, right: right, vps: vps}
}

// This snapshot represents the predecessor's distinct subject/source shape.
// Its payload and metadata are canonical, and the predecessor migration (not
// current runtime authority) inserts them before the C81 upgrade.
func comparisonHistoricalSnapshot(t *testing.T, window evidence.TimeWindow, source, subject evidence.ComparisonSubjectRef) evidence.CanonicalSnapshot {
	t.Helper()
	adapter, err := adapters.NewMonitoringHostAdapter(integrationMonitoringSource{}, integrationMonitoringResolver{}, adapters.AdapterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{Version: recordauth.VisibilityScopeVersionV1,
		Kind: recordauth.VisibilityKindProject, ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindMonitoringInstance,
		SourceID: source.ID, State: recordauth.SourceStateLive, CaptureScope: scope, CurrentScope: &scope,
	})
	if err != nil {
		t.Fatal(err)
	}
	stamp := func(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
	payload := map[string]any{
		"requested_start": stamp(window.Start), "requested_end": stamp(window.End),
		"coverage_start": stamp(window.Start), "coverage_end": stamp(window.End), "actual_precision_seconds": 3600,
		"buckets": []any{map[string]any{
			"series_id": "", "series_kind": "host", "start": stamp(window.Start), "end": stamp(window.End),
			"source_layer": "raw", "source_granularity_seconds": 3600, "sample_count": 1, "maintenance_count": 0, "backfilled_count": 0,
			"metrics": []any{map[string]any{"name": "mem_used_pct", "unit": "percent", "average": 10, "min": 10, "max": 10, "p95": 10,
				"sample_count": 1, "maintenance_count": 0, "backfilled_count": 0, "source_layer": "raw", "source_granularity_seconds": 3600}},
		}}, "gaps": []any{}, "peaks": []any{},
	}
	canonical, _, err := evidence.CanonicalizePayload(adapter.Descriptor(), payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("canonicalize historical comparison payload: %v", err)
	}
	envelope := evidence.SnapshotEnvelope{
		Key: evidence.MonitoringHostV1Key(), Subject: evidence.IdentitySnapshot{Type: subject.Kind, ID: subject.ID, Fields: map[string]string{"display_name": "Historical VPS"}},
		Source:        evidence.IdentitySnapshot{Type: source.Kind, ID: source.ID, Fields: map[string]string{"display_name": "Historical instance"}},
		Authorization: authorization, RequestedWindow: window, ActualWindow: window, ObservedAt: window.End.Add(-time.Microsecond), CapturedAt: window.End, ReferencedAt: window.End,
		SourceRevision: "legacy-revision", SourceDigest: canonical.Hash(), ProducerVersion: "legacy-host/v1", CalculationVersion: "legacy-calculation/v1",
		Units:       evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"mem_used_pct": "percent"}},
		Quality:     evidence.Quality{Status: evidence.QualityComplete, SampleCount: 1, BucketCount: 1, DataPointCount: 1},
		Sensitivity: evidence.SensitivityNormal, ActualPrecision: evidence.DurationSemantics{Applicable: true, Value: time.Hour},
		BucketWidth: evidence.DurationSemantics{Applicable: true, Value: time.Hour}, QuotaOutcome: evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention: evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(adapter.Descriptor(), envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("build historical comparison snapshot: %v", err)
	}
	return snapshot
}

func seedHistoricalComparisonCandidate(ctx context.Context, conn *pgx.Conn, snapshot evidence.CanonicalSnapshot) error {
	envelope := snapshot.Envelope()
	if _, err := conn.Exec(ctx, `insert into public.vps_assets (vps_id, display_name, lifecycle_status)
		values ($1, 'Historical VPS', 'active') on conflict (vps_id) do nothing`, envelope.Subject.ID); err != nil {
		return fmt.Errorf("seed predecessor comparison VPS: %w", err)
	}
	if _, err := conn.Exec(ctx, `insert into public.monitoring_instances (
		monitoring_instance_id, display_name, "group", region, city, provider,
		lifecycle_status, monitoring_status, binding_status, binding_fingerprint,
		binding_epoch_started_at, vps_id, ever_connected, last_trusted_online_at
	) values ($1, 'Historical instance', 'test', 'test-region', 'test-city', 'test-provider',
		'已接入', '启用', '已绑定', 'comparison-fingerprint', $2, $3, true, $2)`,
		envelope.Source.ID, envelope.RequestedWindow.End.Add(-4*time.Hour), envelope.Subject.ID); err != nil {
		return fmt.Errorf("seed predecessor comparison instance: %w", err)
	}
	if _, err := conn.Exec(ctx, `insert into public.records (record_id) values ('rec_comparisonlegacy')`); err != nil {
		return fmt.Errorf("seed predecessor comparison record: %w", err)
	}
	compressed, err := deterministicEvidenceGzip(snapshot.Bytes())
	if err != nil {
		return err
	}
	digest := snapshot.Hash()
	if _, err := conn.Exec(ctx, `insert into public.evidence_payloads
		(payload_digest, canonical_size_bytes, compressed_size_bytes, compressed_payload)
		values ($1, $2, $3, $4)`, digest[:], int64(snapshot.Size()), int64(len(compressed)), compressed); err != nil {
		return fmt.Errorf("seed predecessor comparison payload: %w", err)
	}
	values := []any{envelope.Subject, envelope.Source, envelope.Authorization, envelope.ActualPrecision, envelope.BucketWidth,
		envelope.Units, envelope.Quality, envelope.QuotaOutcome, envelope.Retention, envelope.Redaction}
	encoded := make([][]byte, len(values))
	for index, value := range values {
		encoded[index], err = marshalEvidenceParticipantJSON(value)
		if err != nil {
			return err
		}
	}
	_, err = conn.Exec(ctx, `insert into public.evidence_snapshots (
		snapshot_id, record_id, kind, schema_version, source_kind, source_id,
		subject_identity_snapshot, source_identity_snapshot, capture_authorization, capture_authorization_digest,
		requested_started_at, requested_ended_at, actual_started_at, actual_ended_at, observed_at, captured_at, referenced_at,
		source_revision, source_watermark, source_digest, producer_version, calculation_version,
		actual_precision, bucket_width, unit_semantics, quality, quota_outcome, retention, sensitivity_level, redaction,
		canonical_hash, logical_size_bytes, payload_digest
	) values ('evs_cmpvpsid', 'rec_comparisonlegacy', $1, $2, $3, $4, $5, $6, $7, $8,
		$9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $29)`,
		string(envelope.Key.Kind), int64(envelope.Key.SchemaVersion), envelope.Source.Type, envelope.Source.ID,
		encoded[0], encoded[1], encoded[2], envelope.Authorization.Digest[:],
		envelope.RequestedWindow.Start, envelope.RequestedWindow.End, envelope.ActualWindow.Start, envelope.ActualWindow.End,
		envelope.ObservedAt, envelope.CapturedAt, envelope.ReferencedAt, envelope.SourceRevision, envelope.SourceWatermark,
		envelope.SourceDigest[:], envelope.ProducerVersion, envelope.CalculationVersion, encoded[3], encoded[4], encoded[5],
		encoded[6], encoded[7], encoded[8], string(envelope.Sensitivity), encoded[9], digest[:], int64(snapshot.Size()))
	if err != nil {
		return fmt.Errorf("seed predecessor comparison snapshot: %w", err)
	}
	return nil
}

func comparisonCandidateSnapshotIDs(refs []evidence.ComparisonCandidateRef) []string {
	ids := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		if _, exists := seen[ref.SnapshotID]; exists {
			continue
		}
		seen[ref.SnapshotID] = struct{}{}
		ids = append(ids, ref.SnapshotID)
	}
	return ids
}

func containsComparisonSnapshotID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func containsAllComparisonSnapshotIDs(ids []string, want ...string) bool {
	for _, id := range want {
		if !containsComparisonSnapshotID(ids, id) {
			return false
		}
	}
	return true
}
