package migrate

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"houfeng/db/migrations"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

var recordEvidenceCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)
var recordEvidenceColumnPattern = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9_]*)\s+(?:bigint|boolean|bytea|integer|jsonb|text|timestamptz)\b`)
var recordEvidenceIntentIDConstraintPattern = regexp.MustCompile(`(?im)^\s*intent_id\s+text\s+primary\s+key\s+check\s*\(\s*intent_id\s*~\s*'([^']+)'\s*\),?\s*$`)

var errEvidenceIntentIDAccepted = errors.New("evidence intent ID accepted by Go conformance")

func rawRecordEvidenceMigrationSQL(t *testing.T) string {
	t.Helper()

	payload, err := migrations.FS.ReadFile("0054_create_record_evidence.sql")
	if err != nil {
		t.Fatalf("read 0054 record-evidence migration: %v", err)
	}
	return string(payload)
}

func recordEvidenceMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.ToLower(rawRecordEvidenceMigrationSQL(t))
}

func normalizedRecordEvidenceMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.Join(strings.Fields(recordEvidenceMigrationSQL(t)), " ")
}

func TestRecordEvidenceMigrationIntentIDMatchesEvidenceContract(t *testing.T) {
	const wantPattern = `^evi_[0-9a-f]{24}$`
	matches := recordEvidenceIntentIDConstraintPattern.FindAllStringSubmatch(rawRecordEvidenceMigrationSQL(t), -1)
	if len(matches) != 1 || len(matches[0]) != 2 {
		t.Fatalf("0054 evidence_capture_intents parseable intent_id constraints = %d, want exactly 1", len(matches))
	}
	if gotPattern := matches[0][1]; gotPattern != wantPattern {
		t.Fatalf("0054 intent ID constraint = %q, want Go evidence contract %q", gotPattern, wantPattern)
	}

	intentIDPattern, err := regexp.Compile(matches[0][1])
	if err != nil {
		t.Fatalf("compile 0054 intent ID constraint: %v", err)
	}
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "exact lowercase hex", value: "evi_0123456789abcdef01234567", want: true},
		{name: "lower boundary", value: "evi_000000000000000000000000", want: true},
		{name: "upper boundary", value: "evi_ffffffffffffffffffffffff", want: true},
		{name: "stale eci prefix", value: "eci_0123456789abcdef01234567"},
		{name: "suffix too short", value: "evi_0123456789abcdef0123456"},
		{name: "suffix too long", value: "evi_0123456789abcdef012345678"},
		{name: "uppercase prefix", value: "EVI_0123456789abcdef01234567"},
		{name: "uppercase hex", value: "evi_0123456789abcdef0123456F"},
		{name: "non hex suffix", value: "evi_0123456789abcdef0123456g"},
		{name: "trailing newline", value: "evi_0123456789abcdef01234567\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goAccepted, conformanceErr := evidenceIntentIDAcceptedByGo(t, tt.value)
			if goAccepted != tt.want {
				t.Fatalf("Go evidence contract accepts %q = %t, want %t (conformance error: %v)", tt.value, goAccepted, tt.want, conformanceErr)
			}
			if got := intentIDPattern.MatchString(tt.value); got != tt.want {
				t.Fatalf("0054 intent ID constraint matches %q = %t, want %t", tt.value, got, tt.want)
			}
		})
	}
}

func evidenceIntentIDAcceptedByGo(t *testing.T, intentID string) (bool, error) {
	t.Helper()

	descriptor := evidence.Descriptor{
		Key:    evidence.CommandAuditV1Key(),
		Fields: []evidence.FieldDefinition{{Path: "status", Sensitivity: evidence.SensitivityNormal}},
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         "renderer.v1",
			MaxCanonicalBytes:       evidence.MaxCanonicalPayloadBytes,
		},
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_0123456789abcdef01234567",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("normalize intent contract probe actor: %v", err)
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      recordauth.ProjectIDDefault,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("normalize intent contract probe visibility: %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindTarget,
		SourceID:     "tg_0123456789abcdef",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("normalize intent contract probe authorization: %v", err)
	}
	window := evidence.TimeWindow{
		Start: time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
	}
	selection := evidence.Selection{
		Key:             descriptor.Key,
		SourceType:      string(recordauth.SourceKindTarget),
		SourceID:        "tg_0123456789abcdef",
		RequestedWindow: window,
	}
	notApplicable := evidence.DurationSemantics{Reason: "point-in-time evidence"}
	previewedAt := window.End
	probe := &evidenceIntentIDProbe{
		descriptor:    descriptor,
		authorization: authorization,
		preview: evidence.Preview{
			IntentID:  intentID,
			Key:       descriptor.Key,
			Selection: selection,
			Subject: evidence.IdentitySnapshot{
				Type:   "target",
				ID:     selection.SourceID,
				Fields: map[string]string{"display_name": "intent contract probe"},
			},
			Source: evidence.IdentitySnapshot{
				Type:   selection.SourceType,
				ID:     selection.SourceID,
				Fields: map[string]string{"display_name": "intent contract probe"},
			},
			RequestedWindow:         window,
			ActualWindow:            window,
			ObservedAt:              window.End,
			SourceRevision:          "revision-1",
			ProducerVersion:         "producer-1",
			CalculationVersion:      "calculation-1",
			Units:                   evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "point-in-time evidence"},
			Quality:                 evidence.Quality{Status: evidence.QualityComplete, SampleCount: 1},
			Sensitivity:             evidence.SensitivityNormal,
			ActualPrecision:         notApplicable,
			BucketWidth:             notApplicable,
			QuotaOutcome:            evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
			Retention:               evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
			Redaction:               []evidence.FieldDecision{{Path: "status", Sensitivity: evidence.SensitivityNormal, Action: evidence.RedactionActionIncluded}},
			EstimatedCanonicalBytes: 1,
			SourceDigest:            sha256.Sum256([]byte("source")),
			RendererVersion:         descriptor.Conformance.RendererVersion,
			PreviewedAt:             previewedAt,
			ValidUntil:              previewedAt.Add(evidence.CaptureIntentTTL),
		},
	}
	err = evidence.VerifyKindConformance(context.Background(), probe, evidence.ConformanceFixture{
		Actor:     actor,
		Selection: selection,
		Intent: evidence.Intent{
			ID:            intentID,
			Key:           descriptor.Key,
			Selection:     selection,
			PreviewDigest: sha256.Sum256([]byte("preview")),
			ValidUntil:    probe.preview.ValidUntil,
		},
	})
	return errors.Is(err, errEvidenceIntentIDAccepted), err
}

type evidenceIntentIDProbe struct {
	descriptor    evidence.Descriptor
	preview       evidence.Preview
	authorization evidence.AuthorizationScope
}

func (probe *evidenceIntentIDProbe) Descriptor() evidence.Descriptor {
	return probe.descriptor
}

func (*evidenceIntentIDProbe) ValidateSelection(context.Context, evidence.ActorScope, evidence.Selection) error {
	return nil
}

func (probe *evidenceIntentIDProbe) PreviewCapture(context.Context, evidence.ActorScope, evidence.Selection) (evidence.Preview, error) {
	return probe.preview, nil
}

func (*evidenceIntentIDProbe) Capture(context.Context, evidence.ActorScope, evidence.Intent) (evidence.CanonicalSnapshot, error) {
	return evidence.CanonicalSnapshot{}, errEvidenceIntentIDAccepted
}

func (probe *evidenceIntentIDProbe) Authorize(context.Context, evidence.ActorScope, evidence.Selection) (evidence.AuthorizationScope, error) {
	return probe.authorization, nil
}

func (*evidenceIntentIDProbe) Summarize(evidence.CanonicalSnapshot) evidence.Summary {
	return evidence.Summary{}
}

func (*evidenceIntentIDProbe) Compare(evidence.CanonicalSnapshot, evidence.CanonicalSnapshot, evidence.Alignment) evidence.Comparison {
	return evidence.Comparison{}
}

func (*evidenceIntentIDProbe) Export(evidence.CanonicalSnapshot, evidence.ExportMode) evidence.ExportMaterial {
	return evidence.ExportMaterial{}
}

func TestRecordEvidenceMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordEvidenceCreateTablePattern.FindAllStringSubmatch(recordEvidenceMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"evidence_payloads",
		"evidence_snapshots",
		"evidence_capture_intents",
		"record_revision_evidence",
		"evidence_copy_lineage",
		"evidence_purge_receipts",
		"evidence_payload_gc_receipts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0054 record-evidence tables = %#v, want %#v", got, want)
	}
}

func TestRecordEvidenceMigrationEnforcesPayloadSnapshotAndRevisionIdentity(t *testing.T) {
	rawSQL := recordEvidenceMigrationSQL(t)
	payloadSQL := normalizedRecordEvidenceTableDefinition(t, rawSQL, "evidence_payloads")
	for _, want := range []string{
		"payload_digest bytea primary key check (octet_length(payload_digest) = 32)",
		"payload_encoding text not null default 'canonical_json_gzip_v1' check (payload_encoding = 'canonical_json_gzip_v1')",
		"canonical_size_bytes bigint not null check (canonical_size_bytes between 1 and 5242880)",
		"compressed_size_bytes bigint not null check (compressed_size_bytes between 1 and 6291456)",
		"check (octet_length(compressed_payload) = compressed_size_bytes)",
		"unique (payload_digest, canonical_size_bytes)",
	} {
		if !strings.Contains(payloadSQL, want) {
			t.Errorf("0054 evidence_payloads missing invariant %q", want)
		}
	}
	if strings.Contains(payloadSQL, "compressed_size_bytes <= canonical_size_bytes") {
		t.Fatal("0054 gzip payloads must allow bounded compression overhead above canonical size")
	}

	snapshotSQL := normalizedRecordEvidenceTableDefinition(t, rawSQL, "evidence_snapshots")
	for _, want := range []string{
		"snapshot_id text primary key check (snapshot_id ~ '^evs_[a-z0-9]{1,64}$')",
		"record_id text not null",
		"kind text not null check (kind ~ '^[a-z0-9_.]{1,128}$')",
		"schema_version bigint not null check (schema_version > 0)",
		"source_kind text not null check (source_kind in ('vps', 'monitoring_instance', 'target', 'subscription', 'monitoring_event', 'command_audit', 'record_revision'))",
		"source_id text not null check (source_id ~ '^[a-z0-9_-]{1,128}$')",
		"subject_identity_snapshot jsonb not null check (jsonb_typeof(subject_identity_snapshot) = 'object')",
		"source_identity_snapshot jsonb not null check (jsonb_typeof(source_identity_snapshot) = 'object')",
		"capture_authorization jsonb not null check (jsonb_typeof(capture_authorization) = 'object')",
		"capture_authorization_digest bytea not null check (octet_length(capture_authorization_digest) = 32)",
		"requested_started_at timestamptz not null",
		"requested_ended_at timestamptz not null",
		"actual_started_at timestamptz not null",
		"actual_ended_at timestamptz not null",
		"source_digest bytea not null check (octet_length(source_digest) = 32)",
		"actual_precision jsonb not null check (jsonb_typeof(actual_precision) = 'object')",
		"bucket_width jsonb not null check (jsonb_typeof(bucket_width) = 'object')",
		"quota_outcome jsonb not null check (jsonb_typeof(quota_outcome) = 'object')",
		"retention jsonb not null check (jsonb_typeof(retention) = 'object')",
		"sensitivity_level text not null check (sensitivity_level in ('normal', 'sensitive_topology'))",
		"logical_size_bytes bigint not null check (logical_size_bytes between 1 and 5242880)",
		"check (requested_started_at <= requested_ended_at)",
		"check (actual_started_at <= actual_ended_at and actual_started_at >= requested_started_at and actual_ended_at <= requested_ended_at)",
		"check (captured_at >= observed_at and referenced_at >= captured_at)",
		"check (source_revision <> '' or source_watermark <> '')",
		"check (canonical_hash = payload_digest)",
		"unique (record_id, snapshot_id)",
		"foreign key (record_id) references public.records(record_id) on delete restrict",
		"foreign key (payload_digest, logical_size_bytes) references public.evidence_payloads(payload_digest, canonical_size_bytes) on delete restrict",
	} {
		if !strings.Contains(snapshotSQL, want) {
			t.Errorf("0054 evidence_snapshots missing invariant %q", want)
		}
	}

	revisionEvidenceSQL := normalizedRecordEvidenceTableDefinition(t, rawSQL, "record_revision_evidence")
	for _, want := range []string{
		"primary key (revision_id, ordinal)",
		"unique (revision_id, snapshot_id)",
		"foreign key (record_id, revision_id) references public.record_revisions(record_id, revision_id) on delete restrict",
		"foreign key (record_id, snapshot_id) references public.evidence_snapshots(record_id, snapshot_id) on delete restrict deferrable initially deferred",
	} {
		if !strings.Contains(revisionEvidenceSQL, want) {
			t.Errorf("0054 record_revision_evidence missing invariant %q", want)
		}
	}
}

func TestRecordEvidenceMigrationEnforcesIntentTTLAndUniqueBindings(t *testing.T) {
	sql := normalizedRecordEvidenceMigrationSQL(t)
	intentSQL := normalizedRecordEvidenceTableDefinition(t, recordEvidenceMigrationSQL(t), "evidence_capture_intents")
	for _, want := range []string{
		"snapshot_id text not null check (snapshot_id ~ '^evs_[a-z0-9]{1,64}$')",
		"unique (snapshot_id)",
	} {
		if !strings.Contains(intentSQL, want) {
			t.Errorf("0054 evidence_capture_intents missing snapshot binding %q", want)
		}
	}
	for _, want := range []string{
		"intent_id text primary key check (intent_id ~ '^evi_[0-9a-f]{24}$')",
		"preview_digest bytea not null unique check (octet_length(preview_digest) = 32)",
		"check (valid_until = created_at + interval '15 minutes')",
		"check (estimated_size_bytes between 1 and 5242880)",
		"copied_from_snapshot_id text not null check (copied_from_snapshot_id ~ '^evs_[a-z0-9]{1,64}$')",
		"primary key (operation_id, surface_kind)",
		"primary key (payload_version_digest)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0054 record-evidence migration missing TTL/unique invariant %q", want)
		}
	}
	if strings.Contains(sql, "evidence_snapshots (") && strings.Contains(recordEvidenceTableDefinition(t, sql, "evidence_snapshots"), "expires_at") {
		t.Fatal("0054 logical evidence snapshots must have no independent TTL")
	}
}

func TestRecordEvidenceMigrationKeepsSourceAndCopyCleanupDetached(t *testing.T) {
	sql := normalizedRecordEvidenceMigrationSQL(t)
	if strings.Contains(sql, "on delete cascade") || strings.Contains(sql, "on delete set null") {
		t.Fatal("0054 evidence cleanup must be explicit and must not cascade from live sources")
	}
	for _, forbidden := range []string{
		"foreign key (source_id)",
		"foreign key (source_kind, source_id)",
		"foreign key (copied_from_snapshot_id)",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0054 evidence provenance unexpectedly retains live-row foreign key %q", forbidden)
		}
	}
	for _, want := range []string{
		"create index if not exists idx_evidence_snapshots_source on public.evidence_snapshots(source_kind, source_id, captured_at desc, snapshot_id)",
		"foreign key (snapshot_id) references public.evidence_snapshots(snapshot_id) on delete restrict",
		"check (snapshot_id <> copied_from_snapshot_id)",
		"create index if not exists idx_evidence_copy_lineage_source on public.evidence_copy_lineage(copied_from_snapshot_id, snapshot_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0054 record-evidence migration missing detached provenance contract %q", want)
		}
	}
}

func TestRecordEvidenceMigrationKeepsReceiptsContentFreeAndImmutable(t *testing.T) {
	sql := normalizedRecordEvidenceMigrationSQL(t)
	for _, table := range []string{
		"evidence_payloads",
		"evidence_snapshots",
		"record_revision_evidence",
		"evidence_copy_lineage",
		"evidence_purge_receipts",
		"evidence_payload_gc_receipts",
	} {
		want := "create trigger " + table + "_reject_update before update on public." + table + " for each row execute function record_platform_internal.reject_immutable_mutation()"
		if !strings.Contains(sql, want) {
			t.Errorf("0054 record-evidence migration missing immutable update trigger %q", table)
		}
		drop := "drop trigger if exists " + table + "_reject_update on public." + table + "; " + want
		if !strings.Contains(sql, drop) {
			t.Errorf("0054 record-evidence migration trigger is not repeat-safe %q", table)
		}
	}
	for table, want := range map[string][]string{
		"evidence_purge_receipts": {
			"operation_id", "surface_kind", "receipt_digest", "completed_at", "created_at",
		},
		"evidence_payload_gc_receipts": {
			"payload_version_digest", "receipt_digest", "deleted_at", "created_at",
		},
	} {
		if got := recordEvidenceTableColumns(t, table); !reflect.DeepEqual(got, want) {
			t.Errorf("0054 content-free %s columns = %#v, want %#v", table, got, want)
		}
	}
	purgeReceiptSQL := normalizedRecordEvidenceTableDefinition(t, recordEvidenceMigrationSQL(t), "evidence_purge_receipts")
	for _, want := range []string{
		"operation_id text not null check (operation_id ~ '^rpo_[a-z0-9]{1,64}$')",
		"foreign key (operation_id) references public.record_purge_operations(operation_id) on delete restrict",
	} {
		if !strings.Contains(purgeReceiptSQL, want) {
			t.Errorf("0054 evidence purge receipt missing operation binding %q", want)
		}
	}
}

func recordEvidenceTableColumns(t *testing.T, table string) []string {
	t.Helper()

	tableSQL := recordEvidenceTableDefinition(t, recordEvidenceMigrationSQL(t), table)
	matches := recordEvidenceColumnPattern.FindAllStringSubmatch(tableSQL, -1)
	columns := make([]string, 0, len(matches))
	for _, match := range matches {
		columns = append(columns, match[1])
	}
	return columns
}

func recordEvidenceTableDefinition(t *testing.T, sql, table string) string {
	t.Helper()
	start := strings.Index(sql, "create table if not exists public."+table+" (")
	if start < 0 {
		t.Fatalf("0054 record-evidence migration missing %s table", table)
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatalf("0054 record-evidence %s table is unterminated", table)
	}
	return sql[start : start+end]
}

func normalizedRecordEvidenceTableDefinition(t *testing.T, sql, table string) string {
	t.Helper()
	return strings.Join(strings.Fields(recordEvidenceTableDefinition(t, sql, table)), " ")
}
