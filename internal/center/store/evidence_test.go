package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestPostgresEvidenceRepositoryPersistsCaptureIntentAfterAdmission(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	tx := &fakeRecordPlatformTx{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if !storeEvidenceSQLContains(sql, "select public.record_evidence_intent_insert") || len(args) != 1 {
			t.Fatalf("unexpected intent command: %s %#v", sql, args)
		}
		metadata, selectionBytes, previewBytes := unframeEvidenceFixtureCommand(t, args[0].([]byte))
		var route map[string]string
		if err := json.Unmarshal(metadata, &route); err != nil || route["intent_id"] != intent.ID ||
			route["record_id"] != "rec_evidence1" || route["snapshot_id"] != "evs_evidence1" {
			t.Fatalf("intent route = %#v, error %v", route, err)
		}
		var storedSelection evidence.Selection
		if err := json.Unmarshal(selectionBytes, &storedSelection); err != nil || !reflect.DeepEqual(storedSelection, intent.Selection) {
			t.Fatalf("stored selection = %#v, error = %v", storedSelection, err)
		}
		var storedPreview evidence.Preview
		if err := json.Unmarshal(previewBytes, &storedPreview); err != nil || !reflect.DeepEqual(storedPreview, preview) {
			t.Fatalf("stored preview = %#v, error = %v", storedPreview, err)
		}
		return pgconn.NewCommandTag("SELECT 1"), nil
	}}
	repository := newFakePostgresEvidenceRepository(tx, AdmissionGateFunc(func(_ context.Context, got pgx.Tx) error {
		if got != tx {
			t.Fatal("AdmissionGate received a different transaction")
		}
		tx.calls = append(tx.calls, "gate")
		return nil
	}))

	if err := persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview); err != nil {
		t.Fatalf("PersistCaptureIntent() error = %v", err)
	}
	if !reflect.DeepEqual(tx.calls, []string{"gate", "exec", "exec", "exec", "exec"}) {
		t.Fatalf("PersistCaptureIntent() calls = %#v, want admission, fences and insert", tx.calls)
	}
	if !tx.committed {
		t.Fatal("PersistCaptureIntent() did not commit")
	}
}

func TestPostgresEvidenceRepositoryLoadsFullLiveCaptureIntentBindingAfterAdmission(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	selectionJSON, err := json.Marshal(intent.Selection)
	if err != nil {
		t.Fatalf("json.Marshal(selection) error = %v", err)
	}
	previewJSON, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("json.Marshal(preview) error = %v", err)
	}
	tx := &fakeRecordPlatformTx{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
		if !storeEvidenceSQLContains(sql, "from public.evidence_capture_intents") ||
			!storeEvidenceSQLContains(sql, "valid_until > transaction_timestamp()") {
			t.Fatalf("unexpected SQL:\n%s", sql)
		}
		if !reflect.DeepEqual(args, []any{"rec_evidence1", intent.ID}) {
			t.Fatalf("intent load arguments = %#v", args)
		}
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			if len(dest) != 13 {
				t.Fatalf("intent load destination count = %d, want 13", len(dest))
			}
			*dest[0].(*string) = "rec_evidence1"
			*dest[1].(*string) = string(intent.Key.Kind)
			*dest[2].(*int64) = int64(intent.Key.SchemaVersion)
			*dest[3].(*[]byte) = append([]byte(nil), intent.PreviewDigest[:]...)
			*dest[4].(*[]byte) = append([]byte(nil), preview.SourceDigest[:]...)
			*dest[5].(*[]byte) = append([]byte(nil), selectionJSON...)
			*dest[6].(*[]byte) = append([]byte(nil), previewJSON...)
			*dest[7].(*string) = "evs_evidence1"
			*dest[8].(*int64) = int64(preview.EstimatedCanonicalBytes)
			*dest[9].(*time.Time) = preview.PreviewedAt
			*dest[10].(*time.Time) = preview.ValidUntil
			*dest[11].(*string) = intent.ID
			*dest[12].(*string) = ""
			return nil
		}}
	}}
	repository := newFakePostgresEvidenceRepository(tx, AdmissionGateFunc(func(_ context.Context, got pgx.Tx) error {
		if got != tx {
			t.Fatal("AdmissionGate received a different transaction")
		}
		tx.calls = append(tx.calls, "gate")
		return nil
	}))

	binding, err := repository.LoadCaptureIntentBinding(context.Background(), "rec_evidence1", intent.ID)
	if err != nil {
		t.Fatalf("LoadCaptureIntentBinding() error = %v", err)
	}
	want := evidence.CaptureIntentBinding{
		RecordID: "rec_evidence1", SnapshotID: "evs_evidence1", Intent: intent, Preview: preview,
	}
	if !reflect.DeepEqual(binding, want) {
		t.Fatalf("LoadCaptureIntentBinding() = %#v, want %#v", binding, want)
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("CaptureIntentBinding.Validate() error = %v", err)
	}
	if !reflect.DeepEqual(tx.calls, []string{"gate", "query"}) || !tx.committed {
		t.Fatalf("LoadCaptureIntentBinding() transaction = calls %#v committed %t", tx.calls, tx.committed)
	}
}

func TestPostgresEvidenceRepositoryMapsMissingOrExpiredCaptureIntentToUnavailable(t *testing.T) {
	intent, _ := storeEvidenceIntentFixture()
	tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(...any) error { return pgx.ErrNoRows }}
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	_, err := repository.LoadCaptureIntentBinding(context.Background(), "rec_evidence1", intent.ID)
	if !errors.Is(err, evidence.ErrCaptureIntentUnavailable) {
		t.Fatalf("LoadCaptureIntentBinding() error = %v, want ErrCaptureIntentUnavailable", err)
	}
	if tx.committed {
		t.Fatal("LoadCaptureIntentBinding() committed a missing or expired lookup")
	}
}

func TestPostgresEvidenceRepositoryRejectsNonCanonicalCaptureIntentJSONTimestamps(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	offset := time.FixedZone("persisted-offset", 5*60*60+30*60)
	intent.Selection.RequestedWindow.Start = intent.Selection.RequestedWindow.Start.In(offset)
	intent.Selection.RequestedWindow.End = intent.Selection.RequestedWindow.End.In(offset)
	preview.Selection = intent.Selection
	preview.RequestedWindow.Start = preview.RequestedWindow.Start.In(offset)
	preview.RequestedWindow.End = preview.RequestedWindow.End.In(offset)
	preview.ActualWindow.Start = preview.ActualWindow.Start.In(offset)
	preview.ActualWindow.End = preview.ActualWindow.End.In(offset)
	preview.ObservedAt = preview.ObservedAt.In(offset)
	preview.PreviewedAt = preview.PreviewedAt.In(offset)
	preview.ValidUntil = preview.ValidUntil.In(offset)
	selectionJSON, err := json.Marshal(intent.Selection)
	if err != nil {
		t.Fatalf("json.Marshal(selection) error = %v", err)
	}
	previewJSON, err := json.Marshal(preview)
	if err != nil {
		t.Fatalf("json.Marshal(preview) error = %v", err)
	}
	tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*dest[0].(*string) = "rec_evidence1"
			*dest[1].(*string) = string(intent.Key.Kind)
			*dest[2].(*int64) = int64(intent.Key.SchemaVersion)
			*dest[3].(*[]byte) = append([]byte(nil), intent.PreviewDigest[:]...)
			*dest[4].(*[]byte) = append([]byte(nil), preview.SourceDigest[:]...)
			*dest[5].(*[]byte) = append([]byte(nil), selectionJSON...)
			*dest[6].(*[]byte) = append([]byte(nil), previewJSON...)
			*dest[7].(*string) = "evs_evidence1"
			*dest[8].(*int64) = int64(preview.EstimatedCanonicalBytes)
			*dest[9].(*time.Time) = preview.PreviewedAt.UTC()
			*dest[10].(*time.Time) = preview.ValidUntil.UTC()
			*dest[11].(*string) = intent.ID
			*dest[12].(*string) = ""
			return nil
		}}
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	_, err = repository.LoadCaptureIntentBinding(context.Background(), "rec_evidence1", intent.ID)
	if !errors.Is(err, ErrEvidencePersistenceConflict) {
		t.Fatalf("LoadCaptureIntentBinding() error = %v, want ErrEvidencePersistenceConflict", err)
	}
	if tx.committed {
		t.Fatal("LoadCaptureIntentBinding() committed noncanonical persisted JSON")
	}
}

func TestPostgresEvidenceRepositoryPersistsDeterministicContentAddressedGzip(t *testing.T) {
	snapshot := storeEvidenceSnapshotFixture(t, "payload persistence")
	var compressed []byte
	tx := &fakeRecordPlatformTx{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		if !storeEvidenceSQLContains(sql, "select public.record_evidence_payload_insert") || len(args) != 1 {
			t.Fatalf("unexpected payload command: %s %#v", sql, args)
		}
		_, canonical, body := unframeEvidenceFixtureCommand(t, args[0].([]byte))
		if !bytes.Equal(canonical, snapshot.Bytes()) {
			t.Fatal("payload command changed canonical content")
		}
		compressed = append([]byte(nil), body...)
		return pgconn.NewCommandTag("SELECT 1"), nil
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	stored, err := persistEvidencePayloadFixture(context.Background(), repository, snapshot, evidencePayloadCommandRoute{})
	if err != nil {
		t.Fatalf("PersistPayload() error = %v", err)
	}
	if stored.Digest != snapshot.Hash() || stored.Encoding != EvidencePayloadEncodingCanonicalJSONGzipV1 ||
		stored.CanonicalSizeBytes != snapshot.Size() || stored.CompressedSizeBytes != uint64(len(compressed)) {
		t.Fatalf("PersistPayload() = %#v", stored)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read compressed payload: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close compressed payload reader: %v", err)
	}
	if !bytes.Equal(decoded, snapshot.Bytes()) {
		t.Fatalf("decoded payload = %q, want %q", decoded, snapshot.Bytes())
	}

	firstCompressed := append([]byte(nil), compressed...)
	second := &fakeRecordPlatformTx{exec: tx.exec}
	secondRepository := newFakePostgresEvidenceRepository(second, allowRecordPlatformAdmissionGate)
	if _, err := persistEvidencePayloadFixture(context.Background(), secondRepository, snapshot, evidencePayloadCommandRoute{}); err != nil {
		t.Fatalf("PersistPayload(second) error = %v", err)
	}
	if !bytes.Equal(compressed, firstCompressed) {
		t.Fatal("PersistPayload() gzip bytes are not deterministic")
	}
}

func TestEvidencePayloadBindingRejectsDigestAndSizeMismatch(t *testing.T) {
	canonical := []byte(`{"value":"bound"}`)
	digest := evidence.CanonicalPayloadDigest(canonical)
	wrongDigest := sha256.Sum256([]byte("different"))
	for _, test := range []struct {
		name   string
		digest [sha256.Size]byte
		size   uint64
		want   bool
	}{
		{name: "exact", digest: digest, size: uint64(len(canonical)), want: true},
		{name: "digest mismatch", digest: wrongDigest, size: uint64(len(canonical))},
		{name: "size mismatch", digest: digest, size: uint64(len(canonical) + 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateEvidencePayloadBinding(canonical, test.digest, test.size)
			if test.want && err != nil {
				t.Fatalf("validateEvidencePayloadBinding() error = %v", err)
			}
			if !test.want && !errors.Is(err, ErrInvalidEvidencePersistence) {
				t.Fatalf("validateEvidencePayloadBinding() error = %v, want ErrInvalidEvidencePersistence", err)
			}
		})
	}
}

func TestPostgresEvidenceRepositoryRejectsCaptureIntentTimestampThatPostgresWouldRound(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	preview.PreviewedAt = preview.PreviewedAt.Add(time.Nanosecond)
	preview.ValidUntil = preview.PreviewedAt.Add(evidence.CaptureIntentTTL)
	intent.ValidUntil = preview.ValidUntil
	tx := &fakeRecordPlatformTx{}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	err := persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview)
	if !errors.Is(err, ErrInvalidEvidencePersistence) {
		t.Fatalf("PersistCaptureIntent() error = %v, want ErrInvalidEvidencePersistence", err)
	}
	if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
		t.Fatalf("primitive state = queries %d execs %d committed %t, want zero", tx.queryCount, tx.execCount, tx.committed)
	}
}

func TestPostgresEvidenceRepositoryRejectsNestedCaptureIntentTimestampThatPostgresWouldRound(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*evidence.Intent, *evidence.Preview)
	}{
		{name: "requested window", mutate: func(intent *evidence.Intent, preview *evidence.Preview) {
			intent.Selection.RequestedWindow.Start = intent.Selection.RequestedWindow.Start.Add(time.Nanosecond)
			preview.Selection.RequestedWindow.Start = intent.Selection.RequestedWindow.Start
			preview.RequestedWindow.Start = intent.Selection.RequestedWindow.Start
			preview.ActualWindow.Start = intent.Selection.RequestedWindow.Start
		}},
		{name: "actual window", mutate: func(_ *evidence.Intent, preview *evidence.Preview) {
			preview.ActualWindow.Start = preview.ActualWindow.Start.Add(time.Nanosecond)
		}},
		{name: "observed at", mutate: func(_ *evidence.Intent, preview *evidence.Preview) {
			preview.ObservedAt = preview.ObservedAt.Add(time.Nanosecond)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, preview := storeEvidenceIntentFixture()
			test.mutate(&intent, &preview)
			tx := &fakeRecordPlatformTx{}
			repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

			err := persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview)
			if !errors.Is(err, ErrInvalidEvidencePersistence) {
				t.Fatalf("PersistCaptureIntent() error = %v, want ErrInvalidEvidencePersistence", err)
			}
			if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
				t.Fatalf("primitive state = queries %d execs %d committed %t, want zero", tx.queryCount, tx.execCount, tx.committed)
			}
		})
	}
}

func TestPostgresEvidenceRepositoryNormalizesCaptureIntentTimestampsToUTCBeforePersistence(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	offset := time.FixedZone("capture-offset", 5*60*60+30*60)
	intent.Selection.RequestedWindow.Start = intent.Selection.RequestedWindow.Start.In(offset)
	intent.Selection.RequestedWindow.End = intent.Selection.RequestedWindow.End.In(offset)
	intent.ValidUntil = intent.ValidUntil.In(offset)
	preview.Selection = intent.Selection
	preview.RequestedWindow.Start = preview.RequestedWindow.Start.In(offset)
	preview.RequestedWindow.End = preview.RequestedWindow.End.In(offset)
	preview.ActualWindow.Start = preview.ActualWindow.Start.In(offset)
	preview.ActualWindow.End = preview.ActualWindow.End.In(offset)
	preview.ObservedAt = preview.ObservedAt.In(offset)
	preview.PreviewedAt = preview.PreviewedAt.In(offset)
	preview.ValidUntil = preview.ValidUntil.In(offset)

	tx := &fakeRecordPlatformTx{exec: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
		_, selectionBytes, previewBytes := unframeEvidenceFixtureCommand(t, args[0].([]byte))
		var storedSelection evidence.Selection
		if err := json.Unmarshal(selectionBytes, &storedSelection); err != nil {
			t.Fatalf("decode stored selection: %v", err)
		}
		var storedPreview evidence.Preview
		if err := json.Unmarshal(previewBytes, &storedPreview); err != nil {
			t.Fatalf("decode stored preview: %v", err)
		}
		for name, value := range map[string]time.Time{
			"selection requested start": storedSelection.RequestedWindow.Start,
			"selection requested end":   storedSelection.RequestedWindow.End,
			"preview requested start":   storedPreview.RequestedWindow.Start,
			"preview requested end":     storedPreview.RequestedWindow.End,
			"preview actual start":      storedPreview.ActualWindow.Start,
			"preview actual end":        storedPreview.ActualWindow.End,
			"preview observed at":       storedPreview.ObservedAt,
			"previewed at":              storedPreview.PreviewedAt,
			"valid until":               storedPreview.ValidUntil,
		} {
			if value.Location() != time.UTC {
				t.Errorf("%s location = %v, want UTC", name, value.Location())
			}
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	if err := persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview); err != nil {
		t.Fatalf("PersistCaptureIntent() error = %v", err)
	}
}

func TestPostgresEvidenceRepositoryRejectsOversizedPersistedPreview(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	preview.Source.Fields["oversized"] = strings.Repeat("x", int(evidence.MaxCanonicalPayloadBytes)+1)
	tx := &fakeRecordPlatformTx{}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	err := persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview)
	if !errors.Is(err, ErrInvalidEvidencePersistence) {
		t.Fatalf("PersistCaptureIntent() error = %v, want ErrInvalidEvidencePersistence", err)
	}
	if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
		t.Fatalf("primitive state = queries %d execs %d committed %t, want zero", tx.queryCount, tx.execCount, tx.committed)
	}
}

func TestPostgresEvidenceRepositoryMapsIntentUniquenessConflict(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	uniqueErr := &pgconn.PgError{Code: "23505", ConstraintName: "evidence_capture_intents_preview_digest_key"}
	tx := &fakeRecordPlatformTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, uniqueErr
	}}
	repository := newFakePostgresEvidenceRepository(tx, allowRecordPlatformAdmissionGate)

	err := persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview)
	if !errors.Is(err, ErrEvidencePersistenceConflict) || !errors.Is(err, uniqueErr) {
		t.Fatalf("PersistCaptureIntent() error = %v, want conflict and PostgreSQL causes", err)
	}
}

func TestPostgresEvidenceRepositoryNilTypedNilAndFailingAdmissionMakeZeroPrimitiveWrites(t *testing.T) {
	intent, preview := storeEvidenceIntentFixture()
	snapshot := storeEvidenceSnapshotFixture(t, "admission")
	var typedNil *storeEvidenceTypedNilAdmissionGate
	for _, gate := range []struct {
		name string
		gate AdmissionGate
	}{
		{name: "nil"},
		{name: "typed nil", gate: typedNil},
		{name: "failure", gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error { return errors.New("membership unavailable") })},
	} {
		for _, operation := range []struct {
			name string
			run  func(*PostgresEvidenceRepository) error
		}{
			{name: "intent", run: func(repository *PostgresEvidenceRepository) error {
				return persistEvidenceCaptureIntentFixture(context.Background(), repository, "rec_evidence1", "evs_evidence1", intent, preview)
			}},
			{name: "intent load", run: func(repository *PostgresEvidenceRepository) error {
				_, err := repository.LoadCaptureIntentBinding(context.Background(), "rec_evidence1", intent.ID)
				return err
			}},
			{name: "payload", run: func(repository *PostgresEvidenceRepository) error {
				_, err := persistEvidencePayloadFixture(context.Background(), repository, snapshot, evidencePayloadCommandRoute{})
				return err
			}},
			{name: "intent cleanup", run: func(repository *PostgresEvidenceRepository) error {
				_, err := repository.DeleteExpiredCaptureIntents(context.Background(), 10)
				return err
			}},
			{name: "payload gc", run: func(repository *PostgresEvidenceRepository) error {
				_, err := repository.CollectUnreferencedPayloads(context.Background(), 10)
				return err
			}},
			{name: "project capacity", run: func(repository *PostgresEvidenceRepository) error {
				_, err := repository.ReadProjectEvidenceCapacity(context.Background(), string(recordauth.ProjectIDDefault))
				return err
			}},
			{name: "capacity aggregate", run: func(repository *PostgresEvidenceRepository) error {
				_, err := repository.ReadEvidenceCapacityAggregate(context.Background())
				return err
			}},
			{name: "lifecycle backlog", run: func(repository *PostgresEvidenceRepository) error {
				_, err := repository.ReadEvidenceLifecycleBacklog(context.Background(), 10)
				return err
			}},
		} {
			t.Run(gate.name+"/"+operation.name, func(t *testing.T) {
				tx := &fakeRecordPlatformTx{}
				repository := newFakePostgresEvidenceRepository(tx, gate.gate)
				if err := operation.run(repository); !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
					t.Fatalf("operation error = %v, want ErrRecordPlatformAdmissionUnavailable", err)
				}
				if tx.queryCount != 0 || tx.execCount != 0 || tx.committed {
					t.Fatalf("primitive state = queries %d execs %d committed %t, want zero", tx.queryCount, tx.execCount, tx.committed)
				}
			})
		}
	}
}

type storeEvidenceTypedNilAdmissionGate struct{}

func (*storeEvidenceTypedNilAdmissionGate) Admit(context.Context, pgx.Tx) error {
	return errors.New("typed nil gate must not be called")
}

func newFakePostgresEvidenceRepository(tx pgx.Tx, gate AdmissionGate) *PostgresEvidenceRepository {
	if fake, ok := tx.(*fakeRecordPlatformTx); ok {
		execute := fake.exec
		fake.exec = func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if strings.Contains(sql, "source_application_admit") || strings.Contains(sql, "pg_advisory_xact_lock_shared") {
				return pgconn.NewCommandTag("SELECT 1"), nil
			}
			if execute != nil {
				return execute(ctx, sql, args...)
			}
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
	}
	repository := NewPostgresEvidenceRepository(nil, gate, nil)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
	return repository
}

// Tests use the same restricted intent command as runtime writers.
func persistEvidenceCaptureIntentFixture(ctx context.Context, repository *PostgresEvidenceRepository, recordID, snapshotID string, intent evidence.Intent, preview evidence.Preview, allocationIDs ...string) error {
	intent, preview, selectionJSON, previewJSON, err := prepareEvidenceCaptureIntent(recordID, snapshotID, intent, preview)
	if err != nil {
		return err
	}
	tx, err := repository.startEvidenceMutationTransaction(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, fake := tx.(*fakeRecordPlatformTx); !fake {
		resources, err := normalizeRecordMutationDeclarations(records.RevisionMutationResources{
			Records: []records.RevisionMutationRecord{{ProjectID: "default", RecordID: recordID}},
		})
		if err != nil {
			return err
		}
		if err := acquireRecordMutationResources(ctx, tx, resources); err != nil {
			return err
		}
	}
	allocationID := ""
	if len(allocationIDs) != 0 {
		allocationID = allocationIDs[0]
	}
	actor := recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
	if err := insertEvidenceCaptureIntentCommandOnTx(ctx, tx, actor, allocationID, recordID, snapshotID, intent, preview, selectionJSON, previewJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func persistEvidencePayloadFixture(ctx context.Context, repository *PostgresEvidenceRepository, snapshot evidence.CanonicalSnapshot, route evidencePayloadCommandRoute) (EvidencePayloadMetadata, error) {
	tx, err := repository.startEvidenceMutationTransaction(ctx)
	if err != nil {
		return EvidencePayloadMetadata{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	metadata, err := persistEvidencePayloadOnTx(ctx, tx, route, snapshot)
	if err != nil {
		return EvidencePayloadMetadata{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EvidencePayloadMetadata{}, err
	}
	return metadata, nil
}

func storeEvidenceIntentFixture() (evidence.Intent, evidence.Preview) {
	previewedAt := time.Date(2026, time.August, 12, 8, 0, 0, 0, time.UTC)
	key := evidence.MonitoringHostV1Key()
	selection := evidence.Selection{
		Key: key, SourceType: string(recordauth.SourceKindMonitoringInstance), SourceID: "mi_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: previewedAt.Add(-time.Hour), End: previewedAt},
		Metrics:         []string{"load_1"}, Precision: time.Minute,
	}
	preview := evidence.Preview{
		IntentID: "evi_0123456789abcdef01234567", Key: key, Selection: selection,
		Subject:         evidence.IdentitySnapshot{Type: "monitoring_instance", ID: "mi_0123456789abcdef", Fields: map[string]string{"display_name": "Evidence Host"}},
		Source:          evidence.IdentitySnapshot{Type: string(recordauth.SourceKindMonitoringInstance), ID: "mi_0123456789abcdef", Fields: map[string]string{"display_name": "Evidence Host"}},
		RequestedWindow: selection.RequestedWindow, ActualWindow: selection.RequestedWindow, ObservedAt: previewedAt,
		SourceRevision: "revision-1", ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units:   evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"load_1": "ratio"}},
		Quality: evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60}, Sensitivity: evidence.SensitivityNormal,
		ActualPrecision:         evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		BucketWidth:             evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		QuotaOutcome:            evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:               evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
		Redaction:               []evidence.FieldDecision{{Path: "load_1", Sensitivity: evidence.SensitivityNormal, Action: evidence.RedactionActionIncluded}},
		EstimatedCanonicalBytes: 128, SourceDigest: sha256.Sum256([]byte("source evidence")), RendererVersion: "renderer.v1",
		PreviewedAt: previewedAt, ValidUntil: previewedAt.Add(evidence.CaptureIntentTTL),
	}
	intent := evidence.Intent{
		ID: preview.IntentID, Key: key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("preview evidence")), ValidUntil: preview.ValidUntil,
	}
	return intent, preview
}

func storeEvidenceDescriptor() evidence.Descriptor {
	return evidence.Descriptor{
		Key:    evidence.MonitoringHostV1Key(),
		Fields: []evidence.FieldDefinition{{Path: "value", Sensitivity: evidence.SensitivityNormal}},
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         "renderer.v1", MaxCanonicalBytes: evidence.MaxCanonicalPayloadBytes,
		},
	}
}

func storeEvidenceSnapshotFixture(t *testing.T, value string) evidence.CanonicalSnapshot {
	t.Helper()
	descriptor := storeEvidenceDescriptor()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindMonitoringInstance,
		SourceID: "mi_0123456789abcdef", State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	now := time.Date(2026, time.August, 12, 8, 0, 0, 0, time.UTC)
	window := evidence.TimeWindow{Start: now.Add(-time.Hour), End: now}
	snapshot, _, err := evidence.NewCanonicalSnapshot(descriptor, evidence.SnapshotEnvelope{
		Key:           descriptor.Key,
		Subject:       evidence.IdentitySnapshot{Type: "monitoring_instance", ID: "mi_0123456789abcdef", Fields: map[string]string{"display_name": "Evidence Host"}},
		Source:        evidence.IdentitySnapshot{Type: string(recordauth.SourceKindMonitoringInstance), ID: "mi_0123456789abcdef", Fields: map[string]string{"display_name": "Evidence Host"}},
		Authorization: authorization, RequestedWindow: window, ActualWindow: window, ObservedAt: now,
		CapturedAt: now.Add(time.Minute), ReferencedAt: now.Add(2 * time.Minute), SourceRevision: "revision-1",
		SourceDigest: sha256.Sum256([]byte("source evidence")), ProducerVersion: "producer-1", CalculationVersion: "calculation-1",
		Units:   evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"value": "text"}},
		Quality: evidence.Quality{Status: evidence.QualityComplete, SampleCount: 1}, Sensitivity: evidence.SensitivityNormal,
		ActualPrecision: evidence.DurationSemantics{Applicable: false, Reason: "not applicable"},
		BucketWidth:     evidence.DurationSemantics{Applicable: false, Reason: "not applicable"},
		QuotaOutcome:    evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:       evidence.RetentionSemantics{Immutable: true, Scope: evidence.RetentionScopeRecordRevision, SourceDeletion: evidence.SourceDeletionSnapshotRetained},
	}, map[string]any{"value": value}, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}

func storeEvidenceSQLContains(sql, fragment string) bool {
	return bytes.Contains([]byte(sql), []byte(fragment))
}

func unframeEvidenceFixtureCommand(t *testing.T, command []byte) ([]byte, []byte, []byte) {
	t.Helper()
	if len(command) < 8 {
		t.Fatal("short evidence command")
	}
	metadataSize := int(binary.BigEndian.Uint32(command[:4]))
	if metadataSize < 1 || len(command) < 8+metadataSize {
		t.Fatal("invalid evidence command metadata")
	}
	metadata := command[4 : 4+metadataSize]
	bodySize := int(binary.BigEndian.Uint32(command[4+metadataSize : 8+metadataSize]))
	if bodySize < 1 || len(command) <= 8+metadataSize+bodySize {
		t.Fatal("invalid evidence command body")
	}
	return metadata, command[8+metadataSize : 8+metadataSize+bodySize], command[8+metadataSize+bodySize:]
}
