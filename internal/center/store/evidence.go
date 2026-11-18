package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

const (
	EvidencePayloadEncodingCanonicalJSONGzipV1 = "canonical_json_gzip_v1"
	EvidencePayloadOrphanGracePeriod           = 24 * time.Hour

	maxEvidenceLifecycleBatchSize = uint64(100)
	maxEvidenceCompressedBytes    = uint64(6 * 1024 * 1024)

	evidencePayloadVersionDigestDomainV1   = "houfeng.evidence.payload-version.v1"
	evidencePayloadGCReceiptDigestDomainV1 = "houfeng.evidence.payload-gc-receipt.v1"
)

var (
	ErrInvalidEvidencePersistence  = errors.New("invalid evidence persistence input")
	ErrEvidencePersistenceConflict = errors.New("evidence persistence conflict")
)

type EvidencePayloadMetadata struct {
	Digest              [sha256.Size]byte
	Encoding            string
	CanonicalSizeBytes  uint64
	CompressedSizeBytes uint64
}

type EvidencePayloadGCReceipt = evidence.PayloadGCReceipt

// PostgresEvidenceRepository owns capture-intent and content-addressed payload
// lifecycle primitives. Worker scheduling and revision participation live at
// higher layers.
type PostgresEvidenceRepository struct {
	platform             *PostgresRecordPlatformRepository
	registry             evidence.Registry
	current              evidenceCurrentRecordAuthorizationSource
	subjects             currentRecordSubjectResolver
	continuity           recordbirth.ContinuitySource
	loadEvidenceSnapshot func(context.Context, string, bool) (persistedEvidenceSnapshot, error)
}

var (
	_ evidence.CaptureIntentBindingSource = (*PostgresEvidenceRepository)(nil)
	_ evidence.CapturePayloadSink         = (*PostgresEvidenceRepository)(nil)
	_ evidence.ProjectCapacitySource      = (*PostgresEvidenceRepository)(nil)
	_ evidence.MaintenanceRepository      = (*PostgresEvidenceRepository)(nil)
)

func NewPostgresEvidenceRepository(pool *pgxpool.Pool, gate AdmissionGate, continuity recordbirth.ContinuitySource) *PostgresEvidenceRepository {
	return &PostgresEvidenceRepository{platform: NewPostgresRecordPlatformRepository(pool, gate), continuity: continuity}
}

func (repository *PostgresEvidenceRepository) LoadCaptureIntentBinding(
	ctx context.Context,
	recordID string,
	intentID string,
) (evidence.CaptureIntentBinding, error) {
	if ctx == nil || !recordidentity.IsValid(recordID) || !evidence.ValidCaptureIntentID(intentID) {
		return evidence.CaptureIntentBinding{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.CaptureIntentBinding{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		persistedRecordID, kind, snapshotID, persistedIntentID, allocationID string
		schemaVersion, estimatedSize                                         int64
		previewDigest, sourceDigest                                          []byte
		selectionJSON, previewJSON                                           []byte
		createdAt, validUntil                                                time.Time
	)
	err = tx.QueryRow(ctx, `
		select record_id, kind, schema_version, preview_digest,
		       source_digest, selection, preview,
		       coalesce(snapshot_id, ''), estimated_size_bytes,
		       created_at, valid_until, intent_id, coalesce(birth_allocation_id, '')
		from public.evidence_capture_intents
		where record_id = $1
		  and intent_id = $2
		  and valid_until > transaction_timestamp()`,
		recordID,
		intentID,
	).Scan(
		&persistedRecordID,
		&kind,
		&schemaVersion,
		&previewDigest,
		&sourceDigest,
		&selectionJSON,
		&previewJSON,
		&snapshotID,
		&estimatedSize,
		&createdAt,
		&validUntil,
		&persistedIntentID,
		&allocationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return evidence.CaptureIntentBinding{}, evidence.ErrCaptureIntentUnavailable
	}
	if err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("load evidence capture intent binding: %w", err)
	}
	if persistedRecordID != recordID || persistedIntentID != intentID ||
		schemaVersion < 1 || schemaVersion > int64(^uint16(0)) ||
		len(previewDigest) != sha256.Size || len(sourceDigest) != sha256.Size ||
		!evidence.ValidSnapshotID(snapshotID) || estimatedSize < 1 ||
		uint64(estimatedSize) > evidence.MaxCanonicalPayloadBytes ||
		len(selectionJSON) == 0 || uint64(len(selectionJSON)) > evidence.MaxCanonicalPayloadBytes ||
		len(previewJSON) == 0 || uint64(len(previewJSON)) > evidence.MaxCanonicalPayloadBytes ||
		!evidencePostgresTimestampExact(createdAt) || !evidencePostgresTimestampExact(validUntil) {
		return evidence.CaptureIntentBinding{}, ErrEvidencePersistenceConflict
	}
	var selection evidence.Selection
	if err := json.Unmarshal(selectionJSON, &selection); err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence selection: %w", ErrEvidencePersistenceConflict, err)
	}
	if !equalEvidenceJSON(selectionJSON, selection) {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence selection", ErrEvidencePersistenceConflict)
	}
	var preview evidence.Preview
	if err := json.Unmarshal(previewJSON, &preview); err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence preview: %w", ErrEvidencePersistenceConflict, err)
	}
	if !equalEvidenceJSON(previewJSON, preview) {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("%w: decode persisted evidence preview", ErrEvidencePersistenceConflict)
	}
	key := evidence.KindKey{Kind: evidence.KindName(kind), SchemaVersion: evidence.SchemaVersion(schemaVersion)}
	intent := evidence.Intent{
		ID: persistedIntentID, Key: key, Selection: selection, ValidUntil: validUntil.UTC(),
	}
	copy(intent.PreviewDigest[:], previewDigest)
	normalizedIntent, normalizedPreview := normalizeEvidenceCaptureIntentTimestamps(intent, preview)
	if !reflect.DeepEqual(intent, normalizedIntent) || !reflect.DeepEqual(preview, normalizedPreview) ||
		!evidenceCaptureIntentTimestampsExact(intent, preview) {
		return evidence.CaptureIntentBinding{}, ErrEvidencePersistenceConflict
	}
	binding := evidence.CaptureIntentBinding{
		AllocationID: allocationID, RecordID: persistedRecordID, SnapshotID: snapshotID, Intent: intent, Preview: preview,
	}
	if binding.Validate() != nil || !bytes.Equal(sourceDigest, preview.SourceDigest[:]) ||
		estimatedSize != int64(preview.EstimatedCanonicalBytes) ||
		!createdAt.Equal(preview.PreviewedAt) || !validUntil.Equal(preview.ValidUntil) {
		return evidence.CaptureIntentBinding{}, ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.CaptureIntentBinding{}, fmt.Errorf("commit evidence capture intent load: %w", err)
	}
	return binding, nil
}

func (repository *PostgresEvidenceRepository) PersistCaptureIntent(
	ctx context.Context,
	request evidence.CaptureIntentPersistence,
) error {
	recordID, snapshotID := request.RecordID, request.SnapshotID
	intent, preview, selectionJSON, previewJSON, err := prepareEvidenceCaptureIntent(recordID, snapshotID, request.Intent, request.Preview)
	if err != nil {
		return err
	}
	actor, actorErr := recordauth.NormalizeActorScope(request.Actor)
	authorization, authorizationErr := recordauth.NormalizeSourceAuthorization(request.Authorization)
	if ctx == nil || actorErr != nil || !reflect.DeepEqual(actor, request.Actor) || authorizationErr != nil ||
		!reflect.DeepEqual(authorization, request.Authorization) || authorization.CaptureScope.ProjectID != actor.ProjectID ||
		string(authorization.Kind) != intent.Selection.SourceType || authorization.SourceID != intent.Selection.SourceID {
		return ErrInvalidEvidencePersistence
	}
	allocation, err := repository.prepareEvidenceAllocation(ctx, actor, request.AllocationID, recordID, false)
	if err != nil {
		return err
	}
	prepared, err := repository.prepareEvidenceIntentAuthorization(ctx, request)
	if err != nil {
		return err
	}
	return repository.runCaptureMutation(ctx, allocation != nil, func(ctx context.Context, tx pgx.Tx, proof []byte) error {
		resources, err := discoverEvidenceIntentResources(ctx, tx, request, allocation)
		if err != nil {
			return err
		}
		if evidenceCaptureTargetExists(resources, string(actor.ProjectID), recordID) != (allocation == nil) {
			return evidence.ErrEvidenceServiceUnavailable
		}
		resources.continuityProof = proof
		if err := acquireRecordMutationResources(ctx, tx, resources); err != nil {
			return err
		}
		current, err := discoverEvidenceIntentResources(ctx, tx, request, allocation)
		if err != nil || !reflect.DeepEqual(resources.resources, current.resources) {
			return &pgconn.PgError{Code: "40001", Message: "evidence intent route changed; restart transaction"}
		}
		member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, tx, actor.ProjectID, actor.UserID)
		if err != nil || member.CanonicalHash() != actor.CanonicalHash() {
			return recordauth.ErrDenied
		}
		if err := prepared.authorize(ctx, tx, member, evidence.CapturePayloadRoute{
			Actor: actor, AllocationID: request.AllocationID, RecordID: recordID, SnapshotID: snapshotID, CaptureIntentID: intent.ID,
		}); err != nil {
			return err
		}
		return insertEvidenceCaptureIntentCommandOnTx(ctx, tx, actor, request.AllocationID, recordID, snapshotID, intent, preview, selectionJSON, previewJSON)
	})
}

func prepareEvidenceCaptureIntent(recordID, snapshotID string, intent evidence.Intent, preview evidence.Preview) (evidence.Intent, evidence.Preview, []byte, []byte, error) {
	intent, preview = normalizeEvidenceCaptureIntentTimestamps(intent, preview)
	if !recordidentity.IsValid(recordID) || !evidence.ValidSnapshotID(snapshotID) ||
		!evidence.ValidCaptureIntentID(intent.ID) || intent.ID != preview.IntentID || intent.Key != preview.Key || intent.Key.SchemaVersion == 0 ||
		intent.Selection.Key != intent.Key || preview.Selection.Key != intent.Key ||
		!reflect.DeepEqual(intent.Selection, preview.Selection) || intent.PreviewDigest == [sha256.Size]byte{} ||
		preview.SourceDigest == [sha256.Size]byte{} || preview.EstimatedCanonicalBytes == 0 ||
		preview.EstimatedCanonicalBytes > evidence.MaxCanonicalPayloadBytes || preview.PreviewedAt.IsZero() ||
		!evidenceCaptureIntentTimestampsExact(intent, preview) ||
		!preview.ValidUntil.Equal(preview.PreviewedAt.Add(evidence.CaptureIntentTTL)) || !intent.ValidUntil.Equal(preview.ValidUntil) {
		return intent, preview, nil, nil, ErrInvalidEvidencePersistence
	}
	selectionJSON, err := json.Marshal(intent.Selection)
	if err != nil {
		return intent, preview, nil, nil, fmt.Errorf("marshal evidence intent selection: %w", err)
	}
	if len(selectionJSON) == 0 || uint64(len(selectionJSON)) > evidence.MaxCanonicalPayloadBytes {
		return intent, preview, nil, nil, ErrInvalidEvidencePersistence
	}
	previewJSON, err := json.Marshal(preview)
	if err != nil {
		return intent, preview, nil, nil, fmt.Errorf("marshal evidence intent preview: %w", err)
	}
	if len(previewJSON) == 0 || uint64(len(previewJSON)) > evidence.MaxCanonicalPayloadBytes {
		return intent, preview, nil, nil, ErrInvalidEvidencePersistence
	}
	return intent, preview, selectionJSON, previewJSON, nil
}

func insertEvidenceCaptureIntentCommandOnTx(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, allocationID, recordID, snapshotID string, intent evidence.Intent, preview evidence.Preview, selectionJSON, previewJSON []byte) error {
	actorHash := actor.CanonicalHash()
	metadata, err := json.Marshal(struct {
		ActorID          string    `json:"actor_id"`
		ActorScopeDigest string    `json:"actor_scope_digest"`
		AllocationID     string    `json:"allocation_id"`
		RecordID         string    `json:"record_id"`
		SnapshotID       string    `json:"snapshot_id"`
		IntentID         string    `json:"intent_id"`
		Kind             string    `json:"kind"`
		SchemaVersion    string    `json:"schema_version"`
		PreviewDigest    string    `json:"preview_digest"`
		SourceDigest     string    `json:"source_digest"`
		EstimatedSize    string    `json:"estimated_size_bytes"`
		CreatedAt        time.Time `json:"created_at"`
		ValidUntil       time.Time `json:"valid_until"`
	}{actor.UserID, fmt.Sprintf("%x", actorHash), allocationID, recordID, snapshotID, intent.ID,
		string(intent.Key.Kind), fmt.Sprintf("%d", intent.Key.SchemaVersion), fmt.Sprintf("%x", intent.PreviewDigest),
		fmt.Sprintf("%x", preview.SourceDigest), fmt.Sprintf("%d", preview.EstimatedCanonicalBytes), preview.PreviewedAt.UTC(), preview.ValidUntil.UTC()})
	if err != nil {
		return fmt.Errorf("encode evidence intent command: %w", err)
	}
	command := make([]byte, 0, 8+len(metadata)+len(selectionJSON)+len(previewJSON))
	command = binary.BigEndian.AppendUint32(command, uint32(len(metadata)))
	command = append(command, metadata...)
	command = binary.BigEndian.AppendUint32(command, uint32(len(selectionJSON)))
	command = append(command, selectionJSON...)
	command = append(command, previewJSON...)
	_, err = tx.Exec(ctx, `select public.record_evidence_intent_insert($1)`, command)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return fmt.Errorf("%w: insert evidence capture intent: %w", ErrEvidencePersistenceConflict, err)
		}
		return fmt.Errorf("insert evidence capture intent: %w", err)
	}
	return nil
}

func (repository *PostgresEvidenceRepository) prepareEvidenceIntentAuthorization(ctx context.Context, request evidence.CaptureIntentPersistence) (*preparedEvidencePayloadAuthorization, error) {
	if repository == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	prefetcher, ok := repository.current.(currentRecordAuthorizationPrefetcher)
	if !ok || nilRecordSubjectDependency(prefetcher) {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	resolver, ok := repository.subjects.(*RecordSubjectReadResolver)
	if !ok || resolver == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	reader, ok := resolver.tombstones.(*WitnessedRecordSubjectTombstoneReader)
	if !ok || reader == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	var recordIDs []string
	if request.AllocationID == "" {
		recordIDs = append(recordIDs, request.RecordID)
	}
	recordsAuthorization, err := prefetcher.prepareCurrentAuthorization(ctx, recordIDs)
	if err != nil {
		return nil, err
	}
	sources, err := reader.Prefetch(ctx, []sourceretirement.SourceKey{
		reader.sourceKey(request.Actor.ProjectID, request.Authorization.Kind, request.Authorization.SourceID),
	})
	if err != nil {
		return nil, err
	}
	return &preparedEvidencePayloadAuthorization{
		records: recordsAuthorization, sources: sources,
		envelopes: []evidence.SnapshotEnvelope{{Source: request.Preview.Source, Authorization: request.Authorization}},
	}, nil
}

func discoverEvidenceIntentResources(ctx context.Context, tx pgx.Tx, request evidence.CaptureIntentPersistence, allocation *recordbirth.Allocation) (recordMutationResources, error) {
	projectID := string(request.Actor.ProjectID)
	resources := records.RevisionMutationResources{Records: []records.RevisionMutationRecord{{ProjectID: projectID, RecordID: request.RecordID}}}
	appendEvidenceAllocationResource(&resources, allocation)
	var user records.RevisionMutationUser
	if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, request.Actor.UserID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
		return recordMutationResources{}, err
	}
	resources.Users = append(resources.Users, user)
	groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, request.Actor.ProjectID, request.Actor.UserID)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, groupID := range groups {
		resources.Groups = append(resources.Groups, records.RevisionMutationGroup{ProjectID: projectID, GroupID: groupID})
		resources.Memberships = append(resources.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: request.Actor.UserID})
	}
	appendRevisionMutationSource(&resources, request.Authorization)
	if err := appendEvidenceCurrentRecordResources(ctx, tx, &resources, resources.Records[0]); err != nil {
		return recordMutationResources{}, err
	}
	return normalizeRecordMutationDeclarations(resources)
}

type evidencePayloadCommandRoute struct {
	Route            string `json:"route"`
	ActorID          string `json:"actor_id"`
	ActorScopeDigest string `json:"actor_scope_digest"`
	AllocationID     string `json:"allocation_id"`
	RecordID         string `json:"record_id"`
	SnapshotID       string `json:"snapshot_id"`
	IntentID         string `json:"intent_id"`
	SourceKind       string `json:"source_kind"`
	SourceID         string `json:"source_id"`
	SourceDigest     string `json:"source_digest"`
	Kind             string `json:"kind"`
	SchemaVersion    string `json:"schema_version"`
	PayloadDigest    string `json:"payload_digest"`
	ImportJobID      string `json:"import_job_id"`
	ImportPlanID     string `json:"import_plan_id"`
	RevisionID       string `json:"revision_id"`
}

// Callers pass a snapshot validated by the capture/comparison preparer or by
// the official import decoder. The restricted command hashes these canonical
// bytes independently and checks the persisted route before writing gzip.
func persistEvidencePayloadOnTx(ctx context.Context, tx pgx.Tx, route evidencePayloadCommandRoute, snapshot evidence.CanonicalSnapshot) (EvidencePayloadMetadata, error) {
	canonical := snapshot.Bytes()
	digest := snapshot.Hash()
	if ctx == nil || nilRecordEvidenceParticipantTx(tx) ||
		validateEvidencePayloadBinding(canonical, digest, snapshot.Size()) != nil {
		return EvidencePayloadMetadata{}, ErrInvalidEvidencePersistence
	}
	compressed, err := deterministicEvidenceGzip(canonical)
	if err != nil {
		return EvidencePayloadMetadata{}, err
	}
	if len(compressed) == 0 || uint64(len(compressed)) > maxEvidenceCompressedBytes {
		return EvidencePayloadMetadata{}, ErrInvalidEvidencePersistence
	}
	envelope := snapshot.Envelope()
	route.SourceKind, route.SourceID = envelope.Source.Type, envelope.Source.ID
	route.SourceDigest = fmt.Sprintf("%x", envelope.SourceDigest)
	route.Kind, route.SchemaVersion = string(envelope.Key.Kind), fmt.Sprintf("%d", envelope.Key.SchemaVersion)
	route.PayloadDigest = fmt.Sprintf("%x", digest)
	metadata, err := json.Marshal(route)
	if err != nil || len(metadata) == 0 || len(metadata) > 32768 {
		return EvidencePayloadMetadata{}, ErrInvalidEvidencePersistence
	}
	command := make([]byte, 0, 8+len(metadata)+len(canonical)+len(compressed))
	command = binary.BigEndian.AppendUint32(command, uint32(len(metadata)))
	command = append(command, metadata...)
	command = binary.BigEndian.AppendUint32(command, uint32(len(canonical)))
	command = append(command, canonical...)
	command = append(command, compressed...)
	if _, err := tx.Exec(ctx, `select public.record_evidence_payload_insert($1)`, command); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return EvidencePayloadMetadata{}, fmt.Errorf("%w: insert evidence payload: %w", ErrEvidencePersistenceConflict, err)
		}
		return EvidencePayloadMetadata{}, fmt.Errorf("insert evidence payload: %w", err)
	}
	return EvidencePayloadMetadata{Digest: digest, Encoding: EvidencePayloadEncodingCanonicalJSONGzipV1,
		CanonicalSizeBytes: uint64(len(canonical)), CompressedSizeBytes: uint64(len(compressed))}, nil
}

func (repository *PostgresEvidenceRepository) PersistCapturePayload(
	ctx context.Context,
	route evidence.CapturePayloadRoute,
	snapshot evidence.CanonicalSnapshot,
) error {
	actor, err := recordauth.NormalizeActorScope(route.Actor)
	if ctx == nil || err != nil || !reflect.DeepEqual(actor, route.Actor) ||
		!recordidentity.IsValid(route.RecordID) || !evidence.ValidSnapshotID(route.SnapshotID) ||
		(route.CaptureIntentID == "") == (route.ComparisonClaims == nil) {
		return ErrInvalidEvidencePersistence
	}
	if repository == nil {
		return evidence.ErrEvidenceServiceUnavailable
	}
	envelope := snapshot.Envelope()
	kind, err := repository.registry.LookupKey(envelope.Key)
	if err != nil {
		return evidence.ErrEvidenceServiceUnavailable
	}
	validated, err := evidence.RestoreCanonicalSnapshot(kind.Descriptor(), envelope, snapshot.Bytes())
	if err != nil || validated.Hash() != snapshot.Hash() {
		return ErrInvalidEvidencePersistence
	}
	snapshot = validated
	if route.ComparisonClaims != nil {
		claims := route.ComparisonClaims
		actorDigest := actor.CanonicalHash()
		if claims.Purpose != evidence.ComparisonIntentPurpose || claims.ProjectID != string(actor.ProjectID) ||
			claims.ActorHash != fmt.Sprintf("%x", actorDigest) ||
			route.SnapshotID != evidence.DeriveComparisonResultSnapshotID(route.RecordID, claims.Digest) ||
			snapshot.Envelope().Key != evidence.ComparisonResultV1Key() {
			return ErrInvalidEvidencePersistence
		}
		result, err := evidence.NewPreparedComparisonResult(route.RecordID, route.SnapshotID, snapshot)
		if err != nil {
			return evidence.ErrComparisonIntentStale
		}
		if _, err := evidence.ComparisonDependenciesFromNativeClaims(result, *claims); err != nil {
			return evidence.ErrComparisonIntentStale
		}
	} else if !evidence.ValidCaptureIntentID(route.CaptureIntentID) {
		return ErrInvalidEvidencePersistence
	}
	allocation, err := repository.prepareEvidenceAllocation(ctx, actor, route.AllocationID, route.RecordID, route.ComparisonClaims != nil)
	if err != nil {
		return err
	}
	prepared, err := repository.prepareEvidencePayloadAuthorization(ctx, route, snapshot)
	if err != nil {
		return err
	}
	return repository.runCaptureMutation(ctx, allocation != nil, func(ctx context.Context, tx pgx.Tx, proof []byte) error {
		resources, err := discoverEvidencePayloadResources(ctx, tx, route, snapshot, prepared, allocation)
		if err != nil {
			return err
		}
		if evidenceCaptureTargetExists(resources, string(actor.ProjectID), route.RecordID) != (allocation == nil) {
			return evidence.ErrEvidenceServiceUnavailable
		}
		resources.continuityProof = proof
		if err := acquireRecordMutationResources(ctx, tx, resources); err != nil {
			return err
		}
		current, err := discoverEvidencePayloadResources(ctx, tx, route, snapshot, prepared, allocation)
		if err != nil {
			return &pgconn.PgError{Code: "40001", Message: "evidence payload route changed; restart transaction"}
		}
		if !reflect.DeepEqual(resources.resources, current.resources) {
			return &pgconn.PgError{Code: "40001", Message: "evidence payload resources changed; restart transaction"}
		}
		member, err := NewPostgresCollaborationMembershipReader().ReadMemberActor(ctx, tx, actor.ProjectID, actor.UserID)
		if err != nil || member.CanonicalHash() != actor.CanonicalHash() {
			return recordauth.ErrDenied
		}
		if err := prepared.authorize(ctx, tx, member, route); err != nil {
			return err
		}
		commandRoute := evidencePayloadCommandRoute{
			Route: "capture", ActorID: actor.UserID, ActorScopeDigest: fmt.Sprintf("%x", actor.CanonicalHash()),
			AllocationID: route.AllocationID, RecordID: route.RecordID, SnapshotID: route.SnapshotID,
			IntentID: route.CaptureIntentID,
		}
		if route.ComparisonClaims != nil {
			commandRoute.Route = "comparison"
		}
		_, err = persistEvidencePayloadOnTx(ctx, tx, commandRoute, snapshot)
		return err
	})
}

type preparedEvidencePayloadAuthorization struct {
	records           currentRecordAuthorizationTransactionResolver
	sources           *PreparedSourceAuthority
	envelopes         []evidence.SnapshotEnvelope
	parents           map[string]persistedEvidenceSnapshot
	comparisonParents map[string]evidence.CanonicalSnapshot
}

// W2 observations are obtained before opening the writer transaction. The
// resolver still rereads membership, current records and source facts in it.
func (repository *PostgresEvidenceRepository) prepareEvidencePayloadAuthorization(
	ctx context.Context, route evidence.CapturePayloadRoute, snapshot evidence.CanonicalSnapshot,
) (*preparedEvidencePayloadAuthorization, error) {
	if repository == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	prefetcher, ok := repository.current.(currentRecordAuthorizationPrefetcher)
	if !ok || nilRecordSubjectDependency(prefetcher) {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	resolver, ok := repository.subjects.(*RecordSubjectReadResolver)
	if !ok || resolver == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	reader, ok := resolver.tombstones.(*WitnessedRecordSubjectTombstoneReader)
	if !ok || reader == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	prepared := &preparedEvidencePayloadAuthorization{
		envelopes:         []evidence.SnapshotEnvelope{snapshot.Envelope()},
		parents:           make(map[string]persistedEvidenceSnapshot),
		comparisonParents: make(map[string]evidence.CanonicalSnapshot),
	}
	var recordIDs []string
	if route.AllocationID == "" {
		recordIDs = append(recordIDs, route.RecordID)
	}
	if route.ComparisonClaims != nil {
		if repository.loadEvidenceSnapshot == nil {
			return nil, evidence.ErrEvidenceServiceUnavailable
		}
		for _, item := range route.ComparisonClaims.Items {
			recordIDs = append(recordIDs, item.SourceRecordID)
			ids := append([]string(nil), item.ChosenSnapshotIDs...)
			if item.SnapshotID != "" {
				ids = append(ids, item.SnapshotID)
			}
			for _, id := range ids {
				if _, exists := prepared.parents[id]; exists {
					continue
				}
				parent, err := repository.loadEvidenceSnapshot(ctx, id, true)
				if err != nil || parent.recordID != item.SourceRecordID {
					return nil, evidence.ErrComparisonIntentStale
				}
				prepared.parents[id] = parent
				prepared.envelopes = append(prepared.envelopes, parent.envelope)
				if parent.envelope.Key == evidence.ComparisonResultV1Key() {
					kind, err := evidence.NewComparisonResultKind()
					if err != nil {
						return nil, err
					}
					canonical, err := evidence.RestoreCanonicalSnapshot(kind.Descriptor(), parent.envelope, parent.canonicalPayload)
					if err != nil {
						return nil, evidence.ErrComparisonIntentStale
					}
					prepared.comparisonParents[id] = canonical
				}
			}
		}
	}
	var err error
	prepared.records, err = prefetcher.prepareCurrentAuthorization(ctx, recordIDs)
	if err != nil {
		return nil, err
	}
	keys := make([]sourceretirement.SourceKey, 0, len(prepared.envelopes))
	for _, envelope := range prepared.envelopes {
		keys = append(keys, reader.sourceKey(route.Actor.ProjectID, envelope.Authorization.Kind, envelope.Authorization.SourceID))
	}
	prepared.sources, err = reader.Prefetch(ctx, keys)
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

func (prepared *preparedEvidencePayloadAuthorization) authorize(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, route evidence.CapturePayloadRoute) error {
	if prepared == nil || nilRecordSubjectDependency(prepared.records) || prepared.sources == nil {
		return evidence.ErrEvidenceServiceUnavailable
	}
	var targetExists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from public.records where project_id=$1 and record_id=$2)`, actor.ProjectID, route.RecordID).Scan(&targetExists); err != nil {
		return err
	}
	if route.AllocationID != "" {
		if targetExists {
			return evidence.ErrEvidenceServiceUnavailable
		}
		if err := recordauth.AllowsCapability(actor, recordauth.CapabilityRecordCreate); err != nil {
			return err
		}
	} else {
		if !targetExists {
			return evidence.ErrEvidenceServiceUnavailable
		}
		current, err := prepared.records.resolveCurrentAuthorizationInTransaction(ctx, tx, actor, route.RecordID)
		if err != nil {
			return err
		}
		if err := records.AuthorizeRecordResource(actor, recordauth.CapabilityEvidenceCreate, current.Evidence); err != nil {
			return err
		}
	}
	if route.ComparisonClaims != nil {
		seen := make(map[string]struct{})
		for _, item := range route.ComparisonClaims.Items {
			if _, exists := seen[item.SourceRecordID]; exists {
				continue
			}
			seen[item.SourceRecordID] = struct{}{}
			current, err := prepared.records.resolveCurrentAuthorizationInTransaction(ctx, tx, actor, item.SourceRecordID)
			if err != nil {
				return err
			}
			if err := records.AuthorizeRecordResource(actor, recordauth.CapabilityEvidenceRead, current.Evidence); err != nil {
				return err
			}
		}
	}
	for id, parent := range prepared.parents {
		current, err := readRevisionMutationSnapshot(ctx, tx, id)
		if err != nil || current.RecordID != parent.recordID || current.PayloadDigest != parent.payloadDigest ||
			current.SourceKind != records.SubjectKind(parent.envelope.Source.Type) || current.SourceID != parent.envelope.Source.ID ||
			current.AuthorizationDigest != parent.envelope.Authorization.Digest {
			return &pgconn.PgError{Code: "40001", Message: "comparison evidence source changed; restart transaction"}
		}
	}
	live := transactionRecordSubjectSource{db: tx}
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live), newMonitoringInstanceRecordSubjectAdapter(live), newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		return err
	}
	resolver := NewRecordSubjectReadResolver(registry, prepared.sources.ForTransaction(tx))
	for index, envelope := range prepared.envelopes {
		kind, ok := evidenceReadSubjectKind(envelope.Source.Type)
		if !ok || envelope.Source.ID != envelope.Authorization.SourceID {
			return evidence.ErrEvidenceServiceUnavailable
		}
		identity, err := records.NewSubjectIdentitySnapshot(kind, envelope.Source.Fields)
		if err != nil {
			return err
		}
		resolved, err := resolver.Resolve(ctx, actor, RecordSubjectReadInput{
			Reference:        records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: kind, Role: records.RelationRoleEvidenceSource, SourceID: envelope.Source.ID},
			IdentitySnapshot: identity, CaptureAuthorization: envelope.Authorization,
		})
		if err != nil {
			return err
		}
		if index == 0 && route.CaptureIntentID != "" && resolved.CaptureAuthorization.State != recordauth.SourceStateLive {
			return evidence.ErrPreviewStale
		}
		capability := recordauth.CapabilityEvidenceRead
		if index == 0 {
			capability = recordauth.CapabilityEvidenceCreate
		}
		if err := recordauth.Authorize(actor, capability, recordauth.ResourceScope{
			Version: recordauth.ResourceScopeVersionV1, ProjectID: actor.ProjectID,
			Visibility: envelope.Authorization.CaptureScope, Sources: []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
		}); err != nil {
			return err
		}
	}
	return nil
}

// prepareEvidenceAllocation resolves the server-owned request before opening a
// writer transaction. The ranked allocation command rechecks its immutable
// request, monotonic state and current certified binding after acquisition.
func (repository *PostgresEvidenceRepository) prepareEvidenceAllocation(ctx context.Context, actor recordauth.ActorScope, allocationID, recordID string, comparison bool) (*recordbirth.Allocation, error) {
	if allocationID == "" {
		return nil, nil
	}
	if repository == nil || repository.platform == nil {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	allocation, err := (&PostgresRecordBirthRepository{platform: repository.platform}).GetAllocation(ctx, actor, allocationID)
	if err != nil || allocation.Validate() != nil || allocation.AllocationID != allocationID ||
		allocation.RecordID != recordID || allocation.Request.ProjectID != string(actor.ProjectID) ||
		allocation.Request.ActorID != actor.UserID || allocation.Request.ActorScopeDigest != actor.CanonicalHash() {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	if comparison {
		if allocation.Request.Purpose != recordbirth.PurposeComparison {
			return nil, evidence.ErrEvidenceServiceUnavailable
		}
	} else if allocation.Request.Purpose != recordbirth.PurposeCreate && allocation.Request.Purpose != recordbirth.PurposeEvidence {
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
	switch allocation.State {
	case recordbirth.StateAllocated, recordbirth.StatePreparing, recordbirth.StatePrepared:
		return &allocation, nil
	default:
		return nil, evidence.ErrEvidenceServiceUnavailable
	}
}

func appendEvidenceAllocationResource(resources *records.RevisionMutationResources, allocation *recordbirth.Allocation) {
	if allocation != nil {
		resources.Allocations = append(resources.Allocations, records.RevisionMutationAllocation{
			AllocationID: allocation.AllocationID, Request: allocation.Request, RecordID: allocation.RecordID,
		})
	}
}

// A missing root is only a candidate; an acquired, exact birth allocation is
// required before its route can authorize a pre-root mutation.
func evidenceCaptureTargetExists(resources recordMutationResources, projectID, recordID string) bool {
	for _, root := range resources.resources.RootObservations {
		if root.ProjectID == projectID && root.RecordID == recordID {
			return root.Exists
		}
	}
	return false
}

// Discovery supplies candidates only. The caller compares the complete result
// after acquisition; observing no reservation or root is not a birth permit.
func discoverEvidencePayloadResources(ctx context.Context, tx pgx.Tx, route evidence.CapturePayloadRoute, snapshot evidence.CanonicalSnapshot, prepared *preparedEvidencePayloadAuthorization, allocation *recordbirth.Allocation) (recordMutationResources, error) {
	projectID := string(route.Actor.ProjectID)
	resources := records.RevisionMutationResources{
		Records:  []records.RevisionMutationRecord{{ProjectID: projectID, RecordID: route.RecordID}},
		Payloads: []records.RevisionMutationPayload{{PayloadDigest: snapshot.Hash()}},
	}
	appendEvidenceAllocationResource(&resources, allocation)
	var user records.RevisionMutationUser
	if err := tx.QueryRow(ctx, `select user_id,role,disabled_at is not null from public.users where user_id=$1`, route.Actor.UserID).Scan(&user.UserID, &user.Role, &user.Disabled); err != nil {
		return recordMutationResources{}, err
	}
	resources.Users = append(resources.Users, user)
	groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, route.Actor.ProjectID, route.Actor.UserID)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, groupID := range groups {
		resources.Groups = append(resources.Groups, records.RevisionMutationGroup{ProjectID: projectID, GroupID: groupID})
		resources.Memberships = append(resources.Memberships, records.RevisionMutationMembership{GroupID: groupID, UserID: route.Actor.UserID})
	}
	for _, envelope := range prepared.envelopes {
		appendRevisionMutationSource(&resources, envelope.Authorization)
	}
	if route.CaptureIntentID != "" {
		var intent records.RevisionMutationCaptureIntent
		var kind string
		var version int64
		var previewDigest, sourceDigest []byte
		var allocationID string
		err := tx.QueryRow(ctx, `select intent_id,record_id,coalesce(snapshot_id,''),kind,schema_version,preview_digest,source_digest,selection::text,preview::text,estimated_size_bytes,created_at,valid_until,coalesce(birth_allocation_id,'')
			from public.evidence_capture_intents where intent_id=$1 and valid_until>clock_timestamp()`, route.CaptureIntentID).Scan(
			&intent.IntentID, &intent.RecordID, &intent.SnapshotID, &kind, &version, &previewDigest, &sourceDigest,
			&intent.SelectionJSON, &intent.PreviewJSON, &intent.EstimatedSizeBytes, &intent.CreatedAt, &intent.ValidUntil, &allocationID)
		if err != nil {
			return recordMutationResources{}, evidence.ErrCaptureIntentUnavailable
		}
		if len(previewDigest) != sha256.Size || len(sourceDigest) != sha256.Size || version < 1 || version > 65535 {
			return recordMutationResources{}, ErrEvidencePersistenceConflict
		}
		intent.Kind = evidence.KindKey{Kind: evidence.KindName(kind), SchemaVersion: evidence.SchemaVersion(version)}
		copy(intent.PreviewDigest[:], previewDigest)
		copy(intent.SourceDigest[:], sourceDigest)
		envelope := snapshot.Envelope()
		if allocationID != route.AllocationID || intent.RecordID != route.RecordID || intent.SnapshotID != route.SnapshotID ||
			intent.Kind != envelope.Key || intent.SourceDigest != envelope.SourceDigest {
			return recordMutationResources{}, ErrEvidencePersistenceConflict
		}
		resources.CaptureIntents = append(resources.CaptureIntents, intent)
	} else {
		for _, item := range route.ComparisonClaims.Items {
			resources.Records = append(resources.Records, records.RevisionMutationRecord{ProjectID: projectID, RecordID: item.SourceRecordID})
			if item.RevisionID != "" {
				var owner string
				if err := tx.QueryRow(ctx, `select record_id from public.record_revisions where revision_id=$1`, item.RevisionID).Scan(&owner); err != nil || owner != item.RecordID {
					return recordMutationResources{}, evidence.ErrComparisonIntentStale
				}
				resources.Revisions = append(resources.Revisions, records.RevisionMutationRevision{RecordID: owner, RevisionID: item.RevisionID})
			}
			snapshotIDs := append([]string(nil), item.ChosenSnapshotIDs...)
			if item.SnapshotID != "" {
				snapshotIDs = append(snapshotIDs, item.SnapshotID)
			}
			for _, snapshotID := range snapshotIDs {
				parent, err := readRevisionMutationSnapshot(ctx, tx, snapshotID)
				if err != nil || parent.RecordID != item.SourceRecordID {
					return recordMutationResources{}, evidence.ErrComparisonIntentStale
				}
				resources.Snapshots = append(resources.Snapshots, parent)
				resources.Payloads = append(resources.Payloads, records.RevisionMutationPayload{PayloadDigest: parent.PayloadDigest})
				resources.Sources = append(resources.Sources, records.RevisionMutationSource{ProjectID: projectID, SourceKind: parent.SourceKind, SourceID: parent.SourceID})
				if comparison, ok := prepared.comparisonParents[snapshotID]; ok {
					items, err := evidence.ExtractComparisonDependencyItems(comparison)
					if err != nil {
						return recordMutationResources{}, evidence.ErrComparisonIntentStale
					}
					dependencies, complete, err := readCertifiedComparisonDependencies(ctx, tx, snapshotID, comparison.Hash(), items)
					if err != nil {
						return recordMutationResources{}, err
					}
					if !complete {
						return recordMutationResources{}, evidence.ErrComparisonIntentStale
					}
					appendRevisionMutationDependencies(&resources, snapshotID, comparison.Hash(), dependencies)
				}
			}
		}
	}
	normalized, err := records.CombineRevisionMutationResources(resources)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, record := range normalized.Records {
		if err := appendEvidenceCurrentRecordResources(ctx, tx, &normalized, record); err != nil {
			return recordMutationResources{}, err
		}
	}
	return normalizeRecordMutationDeclarations(normalized)
}

func appendEvidenceCurrentRecordResources(ctx context.Context, tx pgx.Tx, resources *records.RevisionMutationResources, record records.RevisionMutationRecord) error {
	var root records.RevisionMutationRootObservation
	root.ProjectID, root.RecordID = record.ProjectID, record.RecordID
	var visibility, visibilityDigest []byte
	err := tx.QueryRow(ctx, `select coalesce(current_revision_id,''),lifecycle,lock_version,authorization_epoch,current_visibility_scope,current_visibility_digest from public.records where project_id=$1 and record_id=$2`, record.ProjectID, record.RecordID).Scan(&root.CurrentRevisionID, &root.Lifecycle, &root.LockVersion, &root.AuthorizationEpoch, &visibility, &visibilityDigest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	root.Exists = err == nil
	resources.RootObservations = append(resources.RootObservations, root)
	revisionID := root.CurrentRevisionID
	if err == nil && len(visibility) != 0 {
		scope, err := decodeStoredRecordVisibility(visibility, visibilityDigest)
		if err != nil {
			return err
		}
		appendRevisionMutationVisibility(resources, scope)
	}
	if revisionID != "" {
		resources.Revisions = append(resources.Revisions, records.RevisionMutationRevision{RecordID: record.RecordID, RevisionID: revisionID})
		rows, err := tx.Query(ctx, `select capture_authorization,capture_authorization_digest from public.record_revision_subjects where revision_id=$1 order by ordinal`, revisionID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var raw, digest []byte
			var authorization recordauth.SourceAuthorization
			if err := rows.Scan(&raw, &digest); err != nil {
				rows.Close()
				return err
			}
			if err := decodeStoredRecordJSON(raw, &authorization); err != nil {
				rows.Close()
				return err
			}
			authorization, err = normalizeCanonicalRecordSubjectAuthorization(authorization)
			if err != nil || !sameStoredRecordDigest(authorization.Digest, digest) {
				rows.Close()
				return records.ErrInvalidRevisionMutationResources
			}
			appendRevisionMutationSource(resources, authorization)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	reservations, err := readRevisionMutationRows[records.RevisionMutationReservation](ctx, tx, `select project_id,object_id,reservation_id,state,fence_epoch,owner_id,owner_generation,
		coalesce(owner_expires_at,'0001-01-01 00:00:00+00'::timestamptz) from public.deletion_reservations
		where project_id=$1 and object_kind='record' and object_id=$2 and state in ('previewed','fenced','committed') order by reservation_id`, record.ProjectID, record.RecordID)
	if err != nil {
		return err
	}
	resources.Reservations = append(resources.Reservations, reservations...)
	return nil
}

func (repository *PostgresEvidenceRepository) ReadProjectEvidenceCapacity(
	ctx context.Context,
	projectID string,
) (evidence.ProjectCapacityUsage, error) {
	if ctx == nil || recordauth.ValidateProjectID(recordauth.ProjectID(projectID)) != nil {
		return evidence.ProjectCapacityUsage{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.ProjectCapacityUsage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var values [8]int64
	err = tx.QueryRow(ctx, `
		with project_snapshots as materialized (
			select snapshot.logical_size_bytes, snapshot.payload_digest
			from public.evidence_snapshots as snapshot
			join public.records as record on record.record_id = snapshot.record_id
			where record.project_id = $1
		), project_payloads as materialized (
			select distinct snapshot.payload_digest,
			       payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_snapshots as snapshot
			join public.records as record on record.record_id = snapshot.record_id
			join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
			where record.project_id = $1
		), global_orphans as materialized (
			select payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_payloads as payload
			where not exists (
				select 1 from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			)
		)
		select
			(select count(*)::bigint from project_snapshots),
			(select coalesce(sum(logical_size_bytes), 0)::bigint from project_snapshots),
			(select count(*)::bigint from project_payloads),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from project_payloads),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from project_payloads),
			(select count(*)::bigint from global_orphans),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from global_orphans),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from global_orphans)`,
		projectID,
	).Scan(
		&values[0], &values[1], &values[2], &values[3],
		&values[4], &values[5], &values[6], &values[7],
	)
	if err != nil {
		return evidence.ProjectCapacityUsage{}, fmt.Errorf("read project evidence capacity: %w", err)
	}
	unsigned, err := evidenceAccountingUint64(values[:]...)
	if err != nil {
		return evidence.ProjectCapacityUsage{}, err
	}
	usage := evidence.ProjectCapacityUsage{
		ProjectID:            projectID,
		LogicalSnapshotCount: unsigned[0], LogicalSnapshotBytes: unsigned[1],
		PhysicalPayloadCount: unsigned[2], PhysicalCanonicalBytes: unsigned[3], PhysicalCompressedBytes: unsigned[4],
		OrphanPayloadCount: unsigned[5], OrphanCanonicalBytes: unsigned[6], OrphanCompressedBytes: unsigned[7],
	}
	if usage.Validate(projectID) != nil {
		return evidence.ProjectCapacityUsage{}, ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.ProjectCapacityUsage{}, fmt.Errorf("commit project evidence capacity read: %w", err)
	}
	return usage, nil
}

func (repository *PostgresEvidenceRepository) ReadEvidenceCapacityAggregate(
	ctx context.Context,
) (evidence.EvidenceCapacityAggregate, error) {
	if ctx == nil {
		return evidence.EvidenceCapacityAggregate{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.EvidenceCapacityAggregate{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var values [8]int64
	err = tx.QueryRow(ctx, `
		with project_totals as materialized (
			select record.project_id,
			       coalesce(sum(snapshot.logical_size_bytes), 0)::bigint as logical_size_bytes
			from public.records as record
			left join public.evidence_snapshots as snapshot on snapshot.record_id = record.record_id
			group by record.project_id
		), referenced_payloads as materialized (
			select distinct snapshot.payload_digest,
			       payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_snapshots as snapshot
			join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
		), global_orphans as materialized (
			select payload.canonical_size_bytes, payload.compressed_size_bytes
			from public.evidence_payloads as payload
			where not exists (
				select 1 from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			)
		)
		select
			(select count(*)::bigint from project_totals),
			(select coalesce(sum(logical_size_bytes), 0)::bigint from project_totals),
			(select coalesce(max(logical_size_bytes), 0)::bigint from project_totals),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from referenced_payloads),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from referenced_payloads),
			(select count(*)::bigint from global_orphans),
			(select coalesce(sum(canonical_size_bytes), 0)::bigint from global_orphans),
			(select coalesce(sum(compressed_size_bytes), 0)::bigint from global_orphans)`,
	).Scan(
		&values[0], &values[1], &values[2], &values[3],
		&values[4], &values[5], &values[6], &values[7],
	)
	if err != nil {
		return evidence.EvidenceCapacityAggregate{}, fmt.Errorf("read evidence capacity aggregate: %w", err)
	}
	unsigned, err := evidenceAccountingUint64(values[:]...)
	if err != nil {
		return evidence.EvidenceCapacityAggregate{}, err
	}
	aggregate := evidence.EvidenceCapacityAggregate{
		ProjectCount: unsigned[0], LogicalSnapshotBytes: unsigned[1], HighestProjectLogicalBytes: unsigned[2],
		PhysicalCanonicalBytes: unsigned[3], PhysicalCompressedBytes: unsigned[4],
		OrphanPayloadCount: unsigned[5], OrphanCanonicalBytes: unsigned[6], OrphanCompressedBytes: unsigned[7],
	}
	if aggregate.Validate() != nil {
		return evidence.EvidenceCapacityAggregate{}, ErrEvidencePersistenceConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.EvidenceCapacityAggregate{}, fmt.Errorf("commit evidence capacity aggregate read: %w", err)
	}
	return aggregate, nil
}

func (repository *PostgresEvidenceRepository) ReadEvidenceLifecycleBacklog(
	ctx context.Context,
	limit uint64,
) (evidence.EvidenceLifecycleBacklog, error) {
	if ctx == nil || limit == 0 || limit > maxEvidenceLifecycleBatchSize {
		return evidence.EvidenceLifecycleBacklog{}, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return evidence.EvidenceLifecycleBacklog{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	probeLimit := int64(limit + 1)
	var expiredCount, orphanCount int64
	err = tx.QueryRow(ctx, `
		with expired_intents as materialized (
			select intent_id
			from public.evidence_capture_intents
			where valid_until <= transaction_timestamp()
			order by valid_until, intent_id
			limit $1
		), eligible_orphans as materialized (
			select payload.payload_digest
			from public.evidence_payloads as payload
			where payload.created_at <= transaction_timestamp() - ($2 * interval '1 microsecond')
			  and not exists (
				select 1 from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			  )
			order by payload.created_at, payload.payload_digest
			limit $1
		)
		select (select count(*)::bigint from expired_intents),
		       (select count(*)::bigint from eligible_orphans)`,
		probeLimit,
		EvidencePayloadOrphanGracePeriod.Microseconds(),
	).Scan(&expiredCount, &orphanCount)
	if err != nil {
		return evidence.EvidenceLifecycleBacklog{}, fmt.Errorf("read evidence lifecycle backlog: %w", err)
	}
	if expiredCount < 0 || orphanCount < 0 || expiredCount > probeLimit || orphanCount > probeLimit {
		return evidence.EvidenceLifecycleBacklog{}, ErrEvidencePersistenceConflict
	}
	backlog := evidence.EvidenceLifecycleBacklog{
		ExpiredIntentCount:         min(uint64(expiredCount), limit),
		EligibleOrphanPayloadCount: min(uint64(orphanCount), limit),
		MoreExpiredIntents:         uint64(expiredCount) > limit,
		MoreEligibleOrphanPayloads: uint64(orphanCount) > limit,
	}
	if err := tx.Commit(ctx); err != nil {
		return evidence.EvidenceLifecycleBacklog{}, fmt.Errorf("commit evidence lifecycle backlog read: %w", err)
	}
	return backlog, nil
}

func evidenceAccountingUint64(values ...int64) ([]uint64, error) {
	converted := make([]uint64, len(values))
	for index, value := range values {
		if value < 0 {
			return nil, ErrEvidencePersistenceConflict
		}
		converted[index] = uint64(value)
	}
	return converted, nil
}

func (repository *PostgresEvidenceRepository) DeleteExpiredCaptureIntents(
	ctx context.Context,
	limit uint64,
) ([]string, error) {
	if ctx == nil || limit == 0 || limit > maxEvidenceLifecycleBatchSize {
		return nil, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startEvidenceMutationTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		delete from public.evidence_capture_intents
		where intent_id in (
			select intent_id
			from public.evidence_capture_intents
				where valid_until <= transaction_timestamp()
				order by valid_until, intent_id
				limit $1
			)
		returning intent_id`, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("delete expired evidence capture intents: %w", err)
	}
	deleted := make([]string, 0)
	for rows.Next() {
		var intentID string
		if err := rows.Scan(&intentID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan expired evidence capture intent: %w", err)
		}
		if !evidence.ValidCaptureIntentID(intentID) {
			rows.Close()
			return nil, ErrEvidencePersistenceConflict
		}
		deleted = append(deleted, intentID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate expired evidence capture intents: %w", err)
	}
	rows.Close()
	sort.Strings(deleted)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit expired evidence capture intent cleanup: %w", err)
	}
	return deleted, nil
}

func (repository *PostgresEvidenceRepository) CollectUnreferencedPayloads(
	ctx context.Context,
	limit uint64,
) ([]EvidencePayloadGCReceipt, error) {
	if ctx == nil || limit == 0 || limit > maxEvidenceLifecycleBatchSize {
		return nil, ErrInvalidEvidencePersistence
	}
	tx, err := repository.startEvidenceMutationTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	digests, err := acquireUnreferencedEvidencePayloads(ctx, tx, limit, EvidencePayloadOrphanGracePeriod)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		with deleted as (
			delete from public.evidence_payloads as payload
			where payload.payload_digest=any($1::bytea[])
			  and payload.created_at<=clock_timestamp()-($2::bigint * interval '1 microsecond')
			  and not exists (
				select 1
				from public.evidence_snapshots as snapshot
				where snapshot.payload_digest = payload.payload_digest
			  )
			returning payload.payload_digest, payload.payload_encoding,
			          payload.canonical_size_bytes, payload.compressed_size_bytes, payload.compressed_payload,
			          payload.created_at, transaction_timestamp() as deleted_at
		)
		select payload_digest, payload_encoding, canonical_size_bytes,
		       compressed_size_bytes, compressed_payload, created_at, deleted_at
		from deleted
		order by created_at, payload_digest`, digests, EvidencePayloadOrphanGracePeriod.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("delete unreferenced evidence payloads: %w", err)
	}
	type deletedPayload struct {
		digest                        [sha256.Size]byte
		encoding                      string
		canonicalSize, compressedSize uint64
		createdAt, deletedAt          time.Time
	}
	deleted := make([]deletedPayload, 0)
	for rows.Next() {
		var rawDigest, compressed []byte
		var canonicalSize, compressedSize int64
		var item deletedPayload
		if err := rows.Scan(&rawDigest, &item.encoding, &canonicalSize, &compressedSize, &compressed, &item.createdAt, &item.deletedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan unreferenced evidence payload: %w", err)
		}
		if len(rawDigest) != sha256.Size || item.encoding != EvidencePayloadEncodingCanonicalJSONGzipV1 ||
			canonicalSize < 1 || uint64(canonicalSize) > evidence.MaxCanonicalPayloadBytes ||
			compressedSize < 1 || uint64(compressedSize) > maxEvidenceCompressedBytes ||
			item.createdAt.IsZero() || item.deletedAt.IsZero() {
			rows.Close()
			return nil, ErrEvidencePersistenceConflict
		}
		copy(item.digest[:], rawDigest)
		if _, err := decodeEvidencePayloadBytes(compressed, canonicalSize, compressedSize, item.digest); err != nil {
			rows.Close()
			return nil, ErrEvidencePersistenceConflict
		}
		item.canonicalSize = uint64(canonicalSize)
		item.compressedSize = uint64(compressedSize)
		item.createdAt = item.createdAt.UTC()
		item.deletedAt = item.deletedAt.UTC()
		deleted = append(deleted, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate unreferenced evidence payloads: %w", err)
	}
	rows.Close()
	receipts := make([]EvidencePayloadGCReceipt, 0, len(deleted))
	for _, item := range deleted {
		versionDigest := evidencePayloadVersionDigest(item.digest, item.encoding, item.canonicalSize, item.compressedSize, item.createdAt)
		receiptDigest := evidencePayloadGCReceiptDigest(versionDigest, item.deletedAt)
		if _, err := tx.Exec(ctx, `
			insert into public.evidence_payload_gc_receipts (
				payload_version_digest, receipt_digest, deleted_at
			) values ($1, $2, $3)`, versionDigest[:], receiptDigest[:], item.deletedAt); err != nil {
			return nil, fmt.Errorf("insert evidence payload GC receipt: %w", err)
		}
		receipts = append(receipts, EvidencePayloadGCReceipt{
			PayloadVersionDigest: versionDigest,
			ReceiptDigest:        receiptDigest,
			DeletedAt:            item.deletedAt,
			CanonicalSizeBytes:   item.canonicalSize,
			CompressedSizeBytes:  item.compressedSize,
		})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit evidence payload GC: %w", err)
	}
	return receipts, nil
}

func (repository *PostgresEvidenceRepository) startAdmittedTransaction(ctx context.Context) (pgx.Tx, error) {
	if repository == nil || repository.platform == nil {
		return nil, ErrRecordPlatformAdmissionUnavailable
	}
	tx, err := repository.platform.startTransaction(ctx)
	if err != nil {
		return nil, err
	}
	if err := repository.platform.admit(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (repository *PostgresEvidenceRepository) runCaptureMutation(ctx context.Context, birth bool, callback func(context.Context, pgx.Tx, []byte) error) error {
	if repository == nil || repository.platform == nil || callback == nil {
		return ErrRecordPlatformAdmissionUnavailable
	}
	run := func(ctx context.Context, transaction *RecordPlatformTransaction, proof []byte) error {
		if err := lockComparisonDecisionForWriter(ctx, transaction.tx); err != nil {
			return err
		}
		return callback(ctx, transaction.tx, proof)
	}
	if birth {
		return repository.platform.runSourceAuthorizedBirthMutationTransaction(ctx, repository.continuity, run)
	}
	return repository.platform.runSourceAuthorizedTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		return run(ctx, transaction, nil)
	})
}

// Evidence mutations share the source graph and comparison decision fences.
// Acquire them before any record or evidence rows, and keep them through the
// transaction so a deletion decision cannot become stale before its gate.
func (repository *PostgresEvidenceRepository) startEvidenceMutationTransaction(ctx context.Context) (pgx.Tx, error) {
	if repository == nil || repository.platform == nil {
		return nil, ErrRecordPlatformAdmissionUnavailable
	}
	tx, err := repository.platform.startTransactionWithOptions(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	if err := repository.platform.admit(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func deterministicEvidenceGzip(canonical []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("create evidence gzip writer: %w", err)
	}
	writer.Header.ModTime = time.Unix(0, 0).UTC()
	writer.Header.OS = 255
	if _, err := writer.Write(canonical); err != nil {
		return nil, fmt.Errorf("compress evidence payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish evidence payload compression: %w", err)
	}
	return compressed.Bytes(), nil
}

func validateEvidencePayloadBinding(canonical []byte, digest [sha256.Size]byte, size uint64) error {
	if len(canonical) == 0 || uint64(len(canonical)) > evidence.MaxCanonicalPayloadBytes ||
		size != uint64(len(canonical)) || digest == [sha256.Size]byte{} || evidence.CanonicalPayloadDigest(canonical) != digest {
		return ErrInvalidEvidencePersistence
	}
	return nil
}

func evidencePostgresTimestampExact(value time.Time) bool {
	return !value.IsZero() && value.Equal(value.Truncate(time.Microsecond))
}

func normalizeEvidenceCaptureIntentTimestamps(
	intent evidence.Intent,
	preview evidence.Preview,
) (evidence.Intent, evidence.Preview) {
	intent.Selection.RequestedWindow = normalizeEvidenceTimeWindow(intent.Selection.RequestedWindow)
	intent.ValidUntil = normalizeEvidenceTime(intent.ValidUntil)
	preview.Selection.RequestedWindow = normalizeEvidenceTimeWindow(preview.Selection.RequestedWindow)
	preview.RequestedWindow = normalizeEvidenceTimeWindow(preview.RequestedWindow)
	preview.ActualWindow = normalizeEvidenceTimeWindow(preview.ActualWindow)
	preview.ObservedAt = normalizeEvidenceTime(preview.ObservedAt)
	preview.PreviewedAt = normalizeEvidenceTime(preview.PreviewedAt)
	preview.ValidUntil = normalizeEvidenceTime(preview.ValidUntil)
	return intent, preview
}

func normalizeEvidenceTimeWindow(window evidence.TimeWindow) evidence.TimeWindow {
	return evidence.TimeWindow{
		Start: normalizeEvidenceTime(window.Start),
		End:   normalizeEvidenceTime(window.End),
	}
}

func normalizeEvidenceTime(value time.Time) time.Time {
	return value.Round(0).UTC()
}

func evidenceCaptureIntentTimestampsExact(intent evidence.Intent, preview evidence.Preview) bool {
	values := []time.Time{
		intent.Selection.RequestedWindow.Start,
		intent.Selection.RequestedWindow.End,
		intent.ValidUntil,
		preview.Selection.RequestedWindow.Start,
		preview.Selection.RequestedWindow.End,
		preview.RequestedWindow.Start,
		preview.RequestedWindow.End,
		preview.ActualWindow.Start,
		preview.ActualWindow.End,
		preview.ObservedAt,
		preview.PreviewedAt,
		preview.ValidUntil,
	}
	for _, value := range values {
		if !evidencePostgresTimestampExact(value) {
			return false
		}
	}
	return true
}

func evidencePayloadVersionDigest(
	payloadDigest [sha256.Size]byte,
	encoding string,
	canonicalSize uint64,
	compressedSize uint64,
	createdAt time.Time,
) [sha256.Size]byte {
	hasher := sha256.New()
	writeEvidenceDigestField(hasher, []byte(evidencePayloadVersionDigestDomainV1))
	writeEvidenceDigestField(hasher, payloadDigest[:])
	writeEvidenceDigestField(hasher, []byte(encoding))
	writeEvidenceDigestUint64(hasher, canonicalSize)
	writeEvidenceDigestUint64(hasher, compressedSize)
	writeEvidenceDigestUint64(hasher, uint64(createdAt.UTC().UnixMicro()))
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func evidencePayloadGCReceiptDigest(versionDigest [sha256.Size]byte, deletedAt time.Time) [sha256.Size]byte {
	hasher := sha256.New()
	writeEvidenceDigestField(hasher, []byte(evidencePayloadGCReceiptDigestDomainV1))
	writeEvidenceDigestField(hasher, versionDigest[:])
	writeEvidenceDigestUint64(hasher, uint64(deletedAt.UTC().UnixMicro()))
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

type evidenceDigestWriter interface {
	Write([]byte) (int, error)
}

func writeEvidenceDigestField(writer evidenceDigestWriter, value []byte) {
	writeEvidenceDigestUint64(writer, uint64(len(value)))
	_, _ = writer.Write(value)
}

func writeEvidenceDigestUint64(writer evidenceDigestWriter, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}

func validEvidenceStoreID(value string, prefix string) bool {
	if len(value) < len(prefix)+1 || len(value) > len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}
