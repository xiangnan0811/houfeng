package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestNewPostgresEvidenceRepositoryWithReadSourcesRejectsClosedDependencies(t *testing.T) {
	t.Parallel()

	registry := storeEvidenceReadRegistry(t)
	current := &evidenceReadCurrentAuthorizationSource{current: evidenceReadCurrentAuthorization(t)}
	subjects := &RecordSubjectReadResolver{}
	gate := AdmissionGate(AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }))
	var typedNilGate *evidenceReadTypedNilGate
	var typedNilCurrent *evidenceReadCurrentAuthorizationSource
	tests := []struct {
		name     string
		pool     *pgxpool.Pool
		gate     AdmissionGate
		registry evidence.Registry
		current  records.CurrentRecordAuthorizationSource
		subjects *RecordSubjectReadResolver
	}{
		{name: "nil pool", gate: gate, registry: registry, current: current, subjects: subjects},
		{name: "nil gate", pool: &pgxpool.Pool{}, registry: registry, current: current, subjects: subjects},
		{name: "typed nil gate", pool: &pgxpool.Pool{}, gate: typedNilGate, registry: registry, current: current, subjects: subjects},
		{name: "empty registry", pool: &pgxpool.Pool{}, gate: gate, current: current, subjects: subjects},
		{name: "typed nil current", pool: &pgxpool.Pool{}, gate: gate, registry: registry, current: typedNilCurrent, subjects: subjects},
		{name: "nil subjects", pool: &pgxpool.Pool{}, gate: gate, registry: registry, current: current},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if repository, err := NewPostgresEvidenceRepositoryWithReadSources(test.pool, test.gate, test.registry, test.current, test.subjects); repository != nil || !errors.Is(err, evidence.ErrEvidenceServiceUnavailable) {
				t.Fatalf("NewPostgresEvidenceRepositoryWithReadSources() = (%T, %v), want nil/unavailable", repository, err)
			}
		})
	}
}

func TestDecodePersistedEvidenceSnapshotRejectsDivergentAuthorizationDigest(t *testing.T) {
	t.Parallel()

	snapshot := storeEvidenceSnapshotFixture(t, "digest binding")
	row := evidenceReadPersistedRow(t, snapshot)
	authorization := snapshot.Envelope().Authorization
	authorization.Digest = sha256.Sum256([]byte("divergent authorization JSON digest"))
	row.authorizationJSON = mustStoreRecordSubjectJSON(t, authorization)
	if _, err := decodePersistedEvidenceSnapshot(row, false); !errors.Is(err, ErrEvidencePersistenceConflict) {
		t.Fatalf("decodePersistedEvidenceSnapshot() error = %v, want persistence conflict", err)
	}
}

func TestDecodePersistedEvidenceReferenceRequiresPayloadMetadataWithoutBytes(t *testing.T) {
	t.Parallel()

	snapshot := storeEvidenceSnapshotFixture(t, "metadata binding")
	valid := evidenceReadPersistedRow(t, snapshot)
	valid.payloadEncoding = EvidencePayloadEncodingCanonicalJSONGzipV1
	valid.payloadCanonicalSize = int64(snapshot.Size())
	valid.payloadCompressedSize = 17
	tests := []struct {
		name   string
		mutate func(*persistedEvidenceSnapshotRow)
	}{
		{name: "missing payload metadata", mutate: func(value *persistedEvidenceSnapshotRow) { value.payloadEncoding = "" }},
		{name: "canonical size split", mutate: func(value *persistedEvidenceSnapshotRow) { value.payloadCanonicalSize++ }},
		{name: "invalid compressed size", mutate: func(value *persistedEvidenceSnapshotRow) { value.payloadCompressedSize = 0 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			row := valid
			test.mutate(&row)
			if _, err := decodePersistedEvidenceSnapshot(row, false); !errors.Is(err, ErrEvidencePersistenceConflict) {
				t.Fatalf("decodePersistedEvidenceSnapshot(metadata only) error = %v, want persistence conflict", err)
			}
		})
	}
	if _, err := decodePersistedEvidenceSnapshot(valid, false); err != nil {
		t.Fatalf("decodePersistedEvidenceSnapshot(valid metadata only) error = %v", err)
	}
	if len(valid.compressedPayload) != 0 {
		t.Fatal("metadata-only fixture unexpectedly contains payload bytes")
	}
}

func TestPostgresEvidenceReadSourcesAuthorizeAndKeepReferencesMetadataOnly(t *testing.T) {
	t.Parallel()

	snapshot := storeEvidenceSnapshotFixture(t, "authorized payload")
	registry := storeEvidenceReadRegistry(t)
	actor := mustStoreRecordActor(t)
	currentAuthorization := &evidenceReadCurrentAuthorizationSource{current: evidenceReadCurrentAuthorization(t)}
	currentSource := &evidenceReadSubjectResolver{resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization)}
	includePayload := make([]bool, 0, 3)
	repository := &PostgresEvidenceRepository{
		registry: registry, current: currentAuthorization, subjects: currentSource,
		loadEvidenceSnapshot: func(_ context.Context, snapshotID string, withPayload bool) (persistedEvidenceSnapshot, error) {
			includePayload = append(includePayload, withPayload)
			return persistedEvidenceSnapshot{
				recordID: "rec_evidenceread", snapshotID: snapshotID, envelope: snapshot.Envelope(),
				canonicalPayload: snapshot.Bytes(), payloadDigest: snapshot.Hash(), referenced: true,
			}, nil
		},
	}

	read, err := repository.LoadEvidenceSnapshot(context.Background(), actor, "evs_evidenceread")
	if err != nil {
		t.Fatalf("LoadEvidenceSnapshot() error = %v", err)
	}
	if read.RecordID != "rec_evidenceread" || read.SnapshotID != "evs_evidenceread" ||
		string(read.CanonicalPayload) != string(snapshot.Bytes()) || !read.SourceAvailable {
		t.Fatalf("LoadEvidenceSnapshot() = %#v", read)
	}
	authorized, err := repository.LoadAuthorizedEvidenceSnapshot(context.Background(), actor, "evs_evidenceread")
	if err != nil {
		t.Fatalf("LoadAuthorizedEvidenceSnapshot() error = %v", err)
	}
	if authorized.RecordID != "rec_evidenceread" || authorized.Key != snapshot.Envelope().Key ||
		authorized.Snapshot.Hash() != snapshot.Hash() {
		t.Fatalf("LoadAuthorizedEvidenceSnapshot() = %#v", authorized)
	}
	reference, err := repository.ReauthorizeExistingSnapshot(context.Background(), actor, "rec_evidenceread", "evs_evidenceread")
	if err != nil {
		t.Fatalf("ReauthorizeExistingSnapshot() error = %v", err)
	}
	if reference.RecordID != "rec_evidenceread" || reference.SnapshotID != "evs_evidenceread" ||
		reference.Key != snapshot.Envelope().Key || reference.PayloadDigest != snapshot.Hash() ||
		reference.Authorization.Digest == [32]byte{} {
		t.Fatalf("ReauthorizeExistingSnapshot() = %#v", reference)
	}
	if !reflect.DeepEqual(includePayload, []bool{false, true, false, true, false}) {
		t.Fatalf("payload load flags = %#v, want metadata/payload pairs then metadata-only reference", includePayload)
	}
}

func TestPostgresEvidenceReadSourcesFailClosedMatrix(t *testing.T) {
	t.Parallel()

	snapshot := storeEvidenceSnapshotFixture(t, "closed payload")
	actor := mustStoreRecordActor(t)
	tests := []struct {
		name       string
		mutate     func(*persistedEvidenceSnapshot)
		registry   evidence.Registry
		currentErr error
		payloadErr error
		resolved   records.ResolvedSubject
		wantErr    error
	}{
		{name: "unreferenced is opaque not found", mutate: func(value *persistedEvidenceSnapshot) { value.referenced = false }, registry: storeEvidenceReadRegistry(t), resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrSnapshotNotFound},
		{name: "unregistered known kind fails closed", registry: storeEvidenceReadOtherRegistry(t), resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrKindNotRegistered},
		{name: "corrupt canonical payload", mutate: func(value *persistedEvidenceSnapshot) { value.canonicalPayload = []byte(`{"value":"tampered"}`) }, registry: storeEvidenceReadRegistry(t), resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrEvidenceServiceUnavailable},
		{name: "corrupt envelope metadata", mutate: func(value *persistedEvidenceSnapshot) { value.envelope.Quality.Status = "corrupt" }, registry: storeEvidenceReadRegistry(t), resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrEvidenceServiceUnavailable},
		{name: "record denial is opaque", registry: storeEvidenceReadRegistry(t), currentErr: recordauth.ErrDenied, resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrSnapshotNotFound},
		{name: "denied actor cannot distinguish unregistered kind", registry: storeEvidenceReadOtherRegistry(t), currentErr: recordauth.ErrDenied, resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrSnapshotNotFound},
		{name: "denied actor cannot distinguish corrupt envelope", mutate: func(value *persistedEvidenceSnapshot) { value.envelope.Quality.Status = "corrupt" }, registry: storeEvidenceReadRegistry(t), currentErr: recordauth.ErrDenied, resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrSnapshotNotFound},
		{name: "denied actor cannot distinguish corrupt payload", registry: storeEvidenceReadRegistry(t), currentErr: recordauth.ErrDenied, payloadErr: evidence.ErrEvidenceServiceUnavailable, resolved: evidenceReadResolvedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrSnapshotNotFound},
		{name: "current source intersection denies", registry: storeEvidenceReadRegistry(t), resolved: evidenceReadRestrictedSubject(t, snapshot.Envelope().Authorization), wantErr: evidence.ErrSnapshotNotFound},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			persisted := persistedEvidenceSnapshot{
				recordID: "rec_evidenceread", snapshotID: "evs_evidenceread", envelope: snapshot.Envelope(),
				canonicalPayload: snapshot.Bytes(), payloadDigest: snapshot.Hash(), referenced: true,
			}
			if test.mutate != nil {
				test.mutate(&persisted)
			}
			repository := &PostgresEvidenceRepository{
				registry: test.registry,
				current:  &evidenceReadCurrentAuthorizationSource{current: evidenceReadCurrentAuthorization(t), err: test.currentErr},
				subjects: &evidenceReadSubjectResolver{resolved: test.resolved},
				loadEvidenceSnapshot: func(_ context.Context, _ string, withPayload bool) (persistedEvidenceSnapshot, error) {
					if withPayload && test.payloadErr != nil {
						return persistedEvidenceSnapshot{}, test.payloadErr
					}
					return persisted, nil
				},
			}
			_, err := repository.LoadEvidenceSnapshot(context.Background(), actor, "evs_evidenceread")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("LoadEvidenceSnapshot() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

type evidenceReadCurrentAuthorizationSource struct {
	current records.CurrentRecordAuthorization
	err     error
}

type evidenceReadTypedNilGate struct{}

func (*evidenceReadTypedNilGate) Admit(context.Context, pgx.Tx) error { return nil }

func (source *evidenceReadCurrentAuthorizationSource) ResolveCurrentRecordAuthorization(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error) {
	return source.current, source.err
}

type evidenceReadSubjectResolver struct {
	resolved records.ResolvedSubject
	err      error
}

func (resolver *evidenceReadSubjectResolver) Resolve(context.Context, recordauth.ActorScope, RecordSubjectReadInput) (records.ResolvedSubject, error) {
	return resolver.resolved, resolver.err
}

func evidenceReadCurrentAuthorization(t *testing.T) records.CurrentRecordAuthorization {
	t.Helper()
	visibility := mustStoreProjectVisibility(t)
	source := mustStoreLiveSourceAuthorization(t, recordauth.SourceKindMonitoringInstance, "mi_0123456789abcdef", visibility)
	return records.CurrentRecordAuthorization{
		RecordID: "rec_evidenceread", CurrentRevisionID: "rrv_evidenceread", LockVersion: 1,
		AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive,
		Evidence: records.RecordAuthorizationEvidence{ProjectID: recordauth.ProjectIDDefault, Visibility: visibility, Sources: []recordauth.SourceAuthorization{source}},
	}
}

func evidenceReadResolvedSubject(t *testing.T, capture recordauth.SourceAuthorization) records.ResolvedSubject {
	t.Helper()
	identity := mustStoreRecordSnapshot(t, records.SubjectKindMonitoringInstance, map[string]string{"display_name": "Evidence Host"})
	return records.ResolvedSubject{
		ProjectID: recordauth.ProjectIDDefault, StableID: capture.SourceID, IdentitySnapshot: identity,
		LiveRoute: "/monitoring/" + capture.SourceID, CaptureAuthorization: capture,
	}
}

func evidenceReadRestrictedSubject(t *testing.T, capture recordauth.SourceAuthorization) records.ResolvedSubject {
	t.Helper()
	restricted := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleViewer}, nil, 2)
	current, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: capture.Kind, SourceID: capture.SourceID,
		State: recordauth.SourceStateLive, CaptureScope: capture.CaptureScope, CurrentScope: &restricted,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return evidenceReadResolvedSubject(t, current)
}

func storeEvidenceReadRegistry(t *testing.T) evidence.Registry {
	t.Helper()
	registry, err := evidence.NewRegistry([]evidence.Kind{task5RecoveryEvidenceKind{descriptor: storeEvidenceDescriptor()}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func storeEvidenceReadOtherRegistry(t *testing.T) evidence.Registry {
	t.Helper()
	descriptor := storeEvidenceDescriptor()
	descriptor.Key = evidence.IPQualityReportV1Key()
	registry, err := evidence.NewRegistry([]evidence.Kind{task5RecoveryEvidenceKind{descriptor: descriptor}})
	if err != nil {
		t.Fatalf("NewRegistry(other kind) error = %v", err)
	}
	return registry
}

func evidenceReadPersistedRow(t *testing.T, snapshot evidence.CanonicalSnapshot) persistedEvidenceSnapshotRow {
	t.Helper()
	envelope := snapshot.Envelope()
	return persistedEvidenceSnapshotRow{
		recordID: "rec_evidenceread", snapshotID: "evs_evidenceread",
		kind: string(envelope.Key.Kind), schemaVersion: int64(envelope.Key.SchemaVersion),
		sourceKind: envelope.Source.Type, sourceID: envelope.Source.ID,
		subjectJSON: mustStoreRecordSubjectJSON(t, envelope.Subject), sourceJSON: mustStoreRecordSubjectJSON(t, envelope.Source),
		authorizationJSON: mustStoreRecordSubjectJSON(t, envelope.Authorization), authorizationDigest: append([]byte(nil), envelope.Authorization.Digest[:]...),
		requestedStart: envelope.RequestedWindow.Start, requestedEnd: envelope.RequestedWindow.End,
		actualStart: envelope.ActualWindow.Start, actualEnd: envelope.ActualWindow.End,
		observedAt: envelope.ObservedAt, capturedAt: envelope.CapturedAt, referencedAt: envelope.ReferencedAt,
		sourceRevision: envelope.SourceRevision, sourceWatermark: envelope.SourceWatermark,
		sourceDigest: append([]byte(nil), envelope.SourceDigest[:]...), producerVersion: envelope.ProducerVersion,
		calculationVersion:  envelope.CalculationVersion,
		actualPrecisionJSON: mustStoreRecordSubjectJSON(t, envelope.ActualPrecision), bucketWidthJSON: mustStoreRecordSubjectJSON(t, envelope.BucketWidth),
		unitsJSON: mustStoreRecordSubjectJSON(t, envelope.Units), qualityJSON: mustStoreRecordSubjectJSON(t, envelope.Quality),
		quotaJSON: mustStoreRecordSubjectJSON(t, envelope.QuotaOutcome), retentionJSON: mustStoreRecordSubjectJSON(t, envelope.Retention),
		sensitivity: string(envelope.Sensitivity), redactionJSON: mustStoreRecordSubjectJSON(t, envelope.Redaction),
		canonicalHash: append([]byte(nil), envelope.CanonicalHash[:]...), logicalSize: int64(envelope.CanonicalSize),
		payloadDigest: append([]byte(nil), envelope.CanonicalHash[:]...), referenced: true,
	}
}
