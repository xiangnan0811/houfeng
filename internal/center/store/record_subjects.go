package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

const (
	recordSubjectSourcePolicyRevisionV1      uint64 = 1
	WitnessedRecordSubjectTombstoneVersionV1 uint64 = 1
)

var (
	ErrRecordSubjectNotFound                   = errors.New("record subject not found")
	ErrRecordSubjectUnavailable                = errors.New("record subject unavailable")
	ErrWitnessedRecordSubjectTombstoneNotFound = errors.New("witnessed record subject tombstone not found")
)

type vpsRecordSubject struct {
	VPSID       string
	DisplayName string
	Provider    string
	Region      string
}

type monitoringRecordSubject struct {
	MonitoringInstanceID string
	DisplayName          string
	AgentVersion         string
}

type targetRecordSubject struct {
	TargetID    string
	DisplayName string
	TargetType  string
}

type vpsRecordSubjectSource interface {
	loadVPSRecordSubject(context.Context, string) (vpsRecordSubject, error)
}

type monitoringRecordSubjectSource interface {
	loadMonitoringRecordSubject(context.Context, string) (monitoringRecordSubject, error)
}

type targetRecordSubjectSource interface {
	loadTargetRecordSubject(context.Context, string) (targetRecordSubject, error)
}

// WitnessedRecordSubjectTombstone is supplied only by a source-deletion
// authority that has already verified the external full witness. The local
// digest-only tombstone projection is intentionally insufficient to create
// this value.
type WitnessedRecordSubjectTombstone struct {
	Version                  uint64
	ProjectID                recordauth.ProjectID
	Kind                     recordauth.SourceKind
	SourceID                 string
	AuthorizationFloor       recordauth.VisibilityScope
	LastLiveScope            recordauth.VisibilityScope
	AuthorizationFloorDigest [32]byte
}

type WitnessedRecordSubjectTombstoneSource interface {
	ResolveWitnessedRecordSubjectTombstone(
		context.Context,
		recordauth.ProjectID,
		recordauth.SourceKind,
		string,
	) (WitnessedRecordSubjectTombstone, error)
}

// RecordSubjectReadInput contains only immutable values loaded from a stored
// revision. It is not a transport DTO and must never be populated from client
// project, snapshot, or authorization fields.
type RecordSubjectReadInput struct {
	Reference            records.SubjectReference
	IdentitySnapshot     records.SubjectIdentitySnapshot
	CaptureAuthorization recordauth.SourceAuthorization
}

type RecordSubjectReadResolver struct {
	live       records.SubjectAdapterRegistry
	tombstones WitnessedRecordSubjectTombstoneSource
}

type currentRecordSubjectResolver interface {
	Resolve(
		context.Context,
		recordauth.ActorScope,
		RecordSubjectReadInput,
	) (records.ResolvedSubject, error)
}

type currentRecordAuthorizationDB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type currentRecordAuthorizationSnapshot struct {
	recordID           string
	projectID          recordauth.ProjectID
	lifecycle          records.Lifecycle
	currentRevisionID  string
	lockVersion        uint64
	authorizationEpoch uint64
	visibility         recordauth.VisibilityScope
	subjects           []RecordSubjectReadInput
}

type recordRevisionAuthorizationSnapshot struct {
	recordID           string
	projectID          recordauth.ProjectID
	lifecycle          records.Lifecycle
	revisionID         string
	currentRevisionID  string
	lockVersion        uint64
	authorizationEpoch uint64
	visibility         recordauth.VisibilityScope
	subjects           []RecordSubjectReadInput
}

type PostgresCurrentRecordAuthorizationSource struct {
	db           currentRecordAuthorizationDB
	platform     *PostgresRecordPlatformRepository
	load         func(context.Context, string) (currentRecordAuthorizationSnapshot, error)
	loadRevision func(context.Context, string, string) (recordRevisionAuthorizationSnapshot, error)
	resolver     currentRecordSubjectResolver
}

var _ records.CurrentRecordAuthorizationSource = (*PostgresCurrentRecordAuthorizationSource)(nil)
var _ records.RecordRevisionAuthorizationSource = (*PostgresCurrentRecordAuthorizationSource)(nil)

func NewVPSRecordSubjectAdapter(repository *PostgresVPSAssetRepository) records.SubjectSourceAdapter {
	return newVPSRecordSubjectAdapter(repository)
}

func NewMonitoringInstanceRecordSubjectAdapter(repository *PostgresMonitoringInstanceRepository) records.SubjectSourceAdapter {
	return newMonitoringInstanceRecordSubjectAdapter(repository)
}

func NewTargetRecordSubjectAdapter(repository *PostgresTargetRepository) records.SubjectSourceAdapter {
	return newTargetRecordSubjectAdapter(repository)
}

func NewRecordSubjectReadResolver(
	live records.SubjectAdapterRegistry,
	tombstones WitnessedRecordSubjectTombstoneSource,
) *RecordSubjectReadResolver {
	return &RecordSubjectReadResolver{live: live, tombstones: tombstones}
}

func NewPostgresCurrentRecordAuthorizationSource(
	pool *pgxpool.Pool,
	resolver *RecordSubjectReadResolver,
	gate AdmissionGate,
) *PostgresCurrentRecordAuthorizationSource {
	return newPostgresCurrentRecordAuthorizationSource(pool, resolver, gate)
}

func newPostgresCurrentRecordAuthorizationSource(
	pool *pgxpool.Pool,
	resolver currentRecordSubjectResolver,
	gate AdmissionGate,
) *PostgresCurrentRecordAuthorizationSource {
	source := &PostgresCurrentRecordAuthorizationSource{
		db:       pool,
		platform: NewPostgresRecordPlatformRepository(pool, gate),
		resolver: resolver,
	}
	source.load = source.loadAdmittedCurrentRecordAuthorizationSnapshot
	source.loadRevision = source.loadAdmittedRecordRevisionAuthorizationSnapshot
	return source
}

func (source *PostgresCurrentRecordAuthorizationSource) loadAdmittedCurrentRecordAuthorizationSnapshot(
	ctx context.Context,
	recordID string,
) (currentRecordAuthorizationSnapshot, error) {
	if ctx == nil || source == nil || source.platform == nil || !validStoredRecordIdentity(recordID, "rec_") {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	var snapshot currentRecordAuthorizationSnapshot
	err := source.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := assertRecordReadFence(ctx, transaction.tx, recordID); err != nil {
			return err
		}
		loader := &PostgresCurrentRecordAuthorizationSource{db: transaction.tx}
		loaded, err := loader.loadCurrentRecordAuthorizationSnapshot(ctx, recordID)
		if err != nil {
			return err
		}
		snapshot = loaded
		return nil
	})
	if err != nil {
		return currentRecordAuthorizationSnapshot{}, err
	}
	return snapshot, nil
}

func (source *PostgresCurrentRecordAuthorizationSource) loadAdmittedRecordRevisionAuthorizationSnapshot(
	ctx context.Context,
	recordID string,
	revisionID string,
) (recordRevisionAuthorizationSnapshot, error) {
	if ctx == nil || source == nil || source.platform == nil ||
		!validStoredRecordIdentity(recordID, "rec_") || !validStoredRecordIdentity(revisionID, "rrv_") {
		return recordRevisionAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	var snapshot recordRevisionAuthorizationSnapshot
	err := source.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := assertRecordReadFence(ctx, transaction.tx, recordID); err != nil {
			return err
		}
		loader := &PostgresCurrentRecordAuthorizationSource{db: transaction.tx}
		loaded, err := loader.loadRecordRevisionAuthorizationSnapshot(ctx, recordID, revisionID)
		if err != nil {
			return err
		}
		snapshot = loaded
		return nil
	})
	if err != nil {
		return recordRevisionAuthorizationSnapshot{}, err
	}
	return snapshot, nil
}

func (source *PostgresCurrentRecordAuthorizationSource) loadCurrentRecordAuthorizationSnapshot(
	ctx context.Context,
	recordID string,
) (currentRecordAuthorizationSnapshot, error) {
	if ctx == nil || source == nil || nilRecordSubjectDependency(source.db) ||
		!validStoredRecordIdentity(recordID, "rec_") {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}

	var (
		projectID              string
		lifecycle              string
		currentRevisionID      string
		lockVersion            int64
		authorizationEpoch     int64
		currentVisibilityJSON  []byte
		currentVisibilityHash  []byte
		revisionVisibilityJSON []byte
		revisionVisibilityHash []byte
		snapshot               currentRecordAuthorizationSnapshot
	)
	err := source.db.QueryRow(ctx, `
		select records.record_id,
		       records.project_id,
		       records.lifecycle,
		       records.current_revision_id,
		       records.lock_version,
		       records.authorization_epoch,
		       records.current_visibility_scope,
		       records.current_visibility_digest,
		       revisions.visibility_scope,
		       revisions.visibility_digest
		from public.records records
		join public.record_revisions revisions
		  on revisions.record_id = records.record_id
		 and revisions.revision_id = records.current_revision_id
		where records.record_id = $1`, recordID).Scan(
		&snapshot.recordID,
		&projectID,
		&lifecycle,
		&currentRevisionID,
		&lockVersion,
		&authorizationEpoch,
		&currentVisibilityJSON,
		&currentVisibilityHash,
		&revisionVisibilityJSON,
		&revisionVisibilityHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentRecordAuthorizationSnapshot{}, records.ErrRecordNotFound
	}
	if err != nil {
		return currentRecordAuthorizationSnapshot{}, fmt.Errorf("%w: load current record root: %w", ErrRecordSubjectUnavailable, err)
	}
	if lockVersion <= 0 || authorizationEpoch <= 0 {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}

	currentVisibility, err := decodeStoredRecordVisibility(currentVisibilityJSON, currentVisibilityHash)
	if err != nil {
		return currentRecordAuthorizationSnapshot{}, err
	}
	revisionVisibility, err := decodeStoredRecordVisibility(revisionVisibilityJSON, revisionVisibilityHash)
	if err != nil || !sameRecordSubjectVisibility(currentVisibility, revisionVisibility) {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	snapshot.projectID = recordauth.ProjectID(projectID)
	snapshot.lifecycle = records.Lifecycle(lifecycle)
	snapshot.currentRevisionID = currentRevisionID
	snapshot.lockVersion = uint64(lockVersion)
	snapshot.authorizationEpoch = uint64(authorizationEpoch)
	snapshot.visibility = currentVisibility

	rows, err := source.db.Query(ctx, `
		select ordinal,
		       registry_version,
		       subject_kind,
		       relation_role,
		       source_id,
		       is_primary,
		       identity_snapshot,
		       capture_authorization,
		       capture_authorization_digest
		from public.record_revision_subjects
		where revision_id = $1
		order by ordinal asc`, currentRevisionID)
	if err != nil {
		return currentRecordAuthorizationSnapshot{}, fmt.Errorf("%w: query current record subjects: %w", ErrRecordSubjectUnavailable, err)
	}
	if nilRecordSubjectDependency(rows) {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	defer rows.Close()

	for expectedOrdinal := int64(0); rows.Next(); expectedOrdinal++ {
		var (
			ordinal           int64
			registryVersion   int64
			subjectKind       string
			relationRole      string
			sourceID          string
			primary           bool
			identityJSON      []byte
			authorizationJSON []byte
			authorizationHash []byte
		)
		if err := rows.Scan(
			&ordinal,
			&registryVersion,
			&subjectKind,
			&relationRole,
			&sourceID,
			&primary,
			&identityJSON,
			&authorizationJSON,
			&authorizationHash,
		); err != nil {
			return currentRecordAuthorizationSnapshot{}, fmt.Errorf("%w: scan current record subject: %w", ErrRecordSubjectUnavailable, err)
		}
		if ordinal != expectedOrdinal || registryVersion <= 0 {
			return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
		}

		kind := records.SubjectKind(subjectKind)
		identityFields := make(map[string]string)
		if err := decodeStoredRecordJSON(identityJSON, &identityFields); err != nil {
			return currentRecordAuthorizationSnapshot{}, fmt.Errorf("%w: decode current record subject identity: %w", ErrRecordSubjectUnavailable, err)
		}
		identity, err := records.NewSubjectIdentitySnapshot(kind, identityFields)
		if err != nil {
			return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
		}
		var authorization recordauth.SourceAuthorization
		if err := decodeStoredRecordJSON(authorizationJSON, &authorization); err != nil {
			return currentRecordAuthorizationSnapshot{}, fmt.Errorf("%w: decode current record subject authorization: %w", ErrRecordSubjectUnavailable, err)
		}
		authorization, err = normalizeCanonicalRecordSubjectAuthorization(authorization)
		if err != nil || !sameStoredRecordDigest(authorization.Digest, authorizationHash) {
			return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
		}
		snapshot.subjects = append(snapshot.subjects, RecordSubjectReadInput{
			Reference: records.SubjectReference{
				RegistryVersion: uint64(registryVersion),
				Kind:            kind,
				Role:            records.RelationRole(relationRole),
				SourceID:        sourceID,
				Primary:         primary,
			},
			IdentitySnapshot:     identity,
			CaptureAuthorization: authorization,
		})
	}
	if err := rows.Err(); err != nil {
		return currentRecordAuthorizationSnapshot{}, fmt.Errorf("%w: iterate current record subjects: %w", ErrRecordSubjectUnavailable, err)
	}
	return normalizeCurrentRecordAuthorizationSnapshot(recordID, snapshot.projectID, snapshot)
}

func (source *PostgresCurrentRecordAuthorizationSource) loadRecordRevisionAuthorizationSnapshot(
	ctx context.Context,
	recordID string,
	revisionID string,
) (recordRevisionAuthorizationSnapshot, error) {
	if ctx == nil || source == nil || nilRecordSubjectDependency(source.db) ||
		!validStoredRecordIdentity(recordID, "rec_") || !validStoredRecordIdentity(revisionID, "rrv_") {
		return recordRevisionAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}

	var (
		projectID          string
		lifecycle          string
		lockVersion        int64
		authorizationEpoch int64
		visibilityJSON     []byte
		visibilityHash     []byte
		snapshot           recordRevisionAuthorizationSnapshot
	)
	err := source.db.QueryRow(ctx, `
		select records.record_id,
		       records.project_id,
		       records.lifecycle,
		       records.current_revision_id,
		       records.lock_version,
		       records.authorization_epoch,
		       revisions.visibility_scope,
		       revisions.visibility_digest
		from public.records records
		join public.record_revisions revisions
		  on revisions.record_id = records.record_id
		 and revisions.revision_id = $2
		where records.record_id = $1`, recordID, revisionID).Scan(
		&snapshot.recordID,
		&projectID,
		&lifecycle,
		&snapshot.currentRevisionID,
		&lockVersion,
		&authorizationEpoch,
		&visibilityJSON,
		&visibilityHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordRevisionAuthorizationSnapshot{}, records.ErrRecordNotFound
	}
	if err != nil {
		return recordRevisionAuthorizationSnapshot{}, fmt.Errorf("%w: load record revision authorization root: %w", ErrRecordSubjectUnavailable, err)
	}
	if lockVersion <= 0 || authorizationEpoch <= 0 {
		return recordRevisionAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	visibility, err := decodeStoredRecordVisibility(visibilityJSON, visibilityHash)
	if err != nil {
		return recordRevisionAuthorizationSnapshot{}, err
	}
	subjects, err := loadRecordRevisionAuthorizationSubjects(ctx, source.db, revisionID)
	if err != nil {
		return recordRevisionAuthorizationSnapshot{}, err
	}
	snapshot.projectID = recordauth.ProjectID(projectID)
	snapshot.lifecycle = records.Lifecycle(lifecycle)
	snapshot.revisionID = revisionID
	snapshot.lockVersion = uint64(lockVersion)
	snapshot.authorizationEpoch = uint64(authorizationEpoch)
	snapshot.visibility = visibility
	snapshot.subjects = subjects
	return normalizeRecordRevisionAuthorizationSnapshot(recordID, revisionID, snapshot.projectID, snapshot)
}

func loadRecordRevisionAuthorizationSubjects(
	ctx context.Context,
	db currentRecordAuthorizationDB,
	revisionID string,
) ([]RecordSubjectReadInput, error) {
	rows, err := db.Query(ctx, `
		select ordinal,
		       registry_version,
		       subject_kind,
		       relation_role,
		       source_id,
		       is_primary,
		       identity_snapshot,
		       capture_authorization,
		       capture_authorization_digest
		from public.record_revision_subjects
		where revision_id = $1
		order by ordinal asc`, revisionID)
	if err != nil {
		return nil, fmt.Errorf("%w: query record revision authorization subjects: %w", ErrRecordSubjectUnavailable, err)
	}
	if nilRecordSubjectDependency(rows) {
		return nil, ErrRecordSubjectUnavailable
	}
	defer rows.Close()

	var subjects []RecordSubjectReadInput
	for expectedOrdinal := int64(0); rows.Next(); expectedOrdinal++ {
		var (
			ordinal           int64
			registryVersion   int64
			subjectKind       string
			relationRole      string
			sourceID          string
			primary           bool
			identityJSON      []byte
			authorizationJSON []byte
			authorizationHash []byte
		)
		if err := rows.Scan(
			&ordinal,
			&registryVersion,
			&subjectKind,
			&relationRole,
			&sourceID,
			&primary,
			&identityJSON,
			&authorizationJSON,
			&authorizationHash,
		); err != nil {
			return nil, fmt.Errorf("%w: scan record revision authorization subject: %w", ErrRecordSubjectUnavailable, err)
		}
		if ordinal != expectedOrdinal || registryVersion <= 0 {
			return nil, ErrRecordSubjectUnavailable
		}
		kind := records.SubjectKind(subjectKind)
		identityFields := make(map[string]string)
		if err := decodeStoredRecordJSON(identityJSON, &identityFields); err != nil {
			return nil, fmt.Errorf("%w: decode record revision authorization identity: %w", ErrRecordSubjectUnavailable, err)
		}
		identity, err := records.NewSubjectIdentitySnapshot(kind, identityFields)
		if err != nil {
			return nil, ErrRecordSubjectUnavailable
		}
		var authorization recordauth.SourceAuthorization
		if err := decodeStoredRecordJSON(authorizationJSON, &authorization); err != nil {
			return nil, fmt.Errorf("%w: decode record revision capture authorization: %w", ErrRecordSubjectUnavailable, err)
		}
		authorization, err = normalizeCanonicalRecordSubjectAuthorization(authorization)
		if err != nil || !sameStoredRecordDigest(authorization.Digest, authorizationHash) {
			return nil, ErrRecordSubjectUnavailable
		}
		subjects = append(subjects, RecordSubjectReadInput{
			Reference: records.SubjectReference{
				RegistryVersion: uint64(registryVersion),
				Kind:            kind,
				Role:            records.RelationRole(relationRole),
				SourceID:        sourceID,
				Primary:         primary,
			},
			IdentitySnapshot:     identity,
			CaptureAuthorization: authorization,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: iterate record revision authorization subjects: %w", ErrRecordSubjectUnavailable, err)
	}
	return subjects, nil
}

func decodeStoredRecordVisibility(raw, digest []byte) (recordauth.VisibilityScope, error) {
	var visibility recordauth.VisibilityScope
	if err := decodeStoredRecordJSON(raw, &visibility); err != nil {
		return recordauth.VisibilityScope{}, fmt.Errorf("%w: decode current record visibility: %w", ErrRecordSubjectUnavailable, err)
	}
	visibility, err := normalizeCanonicalRecordSubjectVisibility(visibility)
	if err != nil || !sameStoredRecordDigest(visibility.CanonicalHash, digest) {
		return recordauth.VisibilityScope{}, ErrRecordSubjectUnavailable
	}
	return visibility, nil
}

func decodeStoredRecordJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func sameStoredRecordDigest(canonical [32]byte, persisted []byte) bool {
	return len(persisted) == len(canonical) && bytes.Equal(canonical[:], persisted)
}

func (source *PostgresCurrentRecordAuthorizationSource) ResolveCurrentRecordAuthorization(
	ctx context.Context,
	actor recordauth.ActorScope,
	recordID string,
) (records.CurrentRecordAuthorization, error) {
	if ctx == nil || source == nil || source.load == nil || nilRecordSubjectDependency(source.resolver) {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return records.CurrentRecordAuthorization{}, ErrRecordSubjectUnavailable
	}

	snapshot, err := source.load(ctx, recordID)
	if err != nil {
		return records.CurrentRecordAuthorization{}, fmt.Errorf("load current record authorization: %w", err)
	}
	normalized, err := normalizeCurrentRecordAuthorizationSnapshot(recordID, normalizedActor.ProjectID, snapshot)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}

	authorizations, err := source.resolveRecordAuthorizationSubjects(ctx, normalizedActor, normalized.projectID, normalized.subjects)
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}

	return records.CurrentRecordAuthorization{
		RecordID:           normalized.recordID,
		CurrentRevisionID:  normalized.currentRevisionID,
		LockVersion:        normalized.lockVersion,
		AuthorizationEpoch: normalized.authorizationEpoch,
		Lifecycle:          normalized.lifecycle,
		Evidence: records.RecordAuthorizationEvidence{
			ProjectID:  normalized.projectID,
			Visibility: normalized.visibility,
			Sources:    authorizations,
		},
	}, nil
}

func (source *PostgresCurrentRecordAuthorizationSource) ResolveRecordRevisionAuthorization(
	ctx context.Context,
	actor recordauth.ActorScope,
	recordID string,
	revisionID string,
) (records.RecordRevisionAuthorization, error) {
	if ctx == nil || source == nil || source.loadRevision == nil || nilRecordSubjectDependency(source.resolver) {
		return records.RecordRevisionAuthorization{}, ErrRecordSubjectUnavailable
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return records.RecordRevisionAuthorization{}, ErrRecordSubjectUnavailable
	}
	snapshot, err := source.loadRevision(ctx, recordID, revisionID)
	if err != nil {
		return records.RecordRevisionAuthorization{}, fmt.Errorf("load record revision authorization: %w", err)
	}
	normalized, err := normalizeRecordRevisionAuthorizationSnapshot(recordID, revisionID, normalizedActor.ProjectID, snapshot)
	if err != nil {
		return records.RecordRevisionAuthorization{}, err
	}
	authorizations, err := source.resolveRecordAuthorizationSubjects(ctx, normalizedActor, normalized.projectID, normalized.subjects)
	if err != nil {
		return records.RecordRevisionAuthorization{}, err
	}
	return records.RecordRevisionAuthorization{
		RecordID:           normalized.recordID,
		RevisionID:         normalized.revisionID,
		CurrentRevisionID:  normalized.currentRevisionID,
		LockVersion:        normalized.lockVersion,
		AuthorizationEpoch: normalized.authorizationEpoch,
		Lifecycle:          normalized.lifecycle,
		Evidence: records.RecordAuthorizationEvidence{
			ProjectID:  normalized.projectID,
			Visibility: normalized.visibility,
			Sources:    authorizations,
		},
	}, nil
}

func (source *PostgresCurrentRecordAuthorizationSource) resolveRecordAuthorizationSubjects(
	ctx context.Context,
	actor recordauth.ActorScope,
	projectID recordauth.ProjectID,
	subjects []RecordSubjectReadInput,
) ([]recordauth.SourceAuthorization, error) {
	authorizations := make([]recordauth.SourceAuthorization, 0, len(subjects))
	for _, subject := range subjects {
		resolved, err := source.resolver.Resolve(ctx, actor.Clone(), subject)
		if err != nil {
			return nil, fmt.Errorf("resolve current record subject: %w", err)
		}
		authorization, err := normalizeCanonicalRecordSubjectAuthorization(resolved.CaptureAuthorization)
		expectedKind, kindOK := recordSubjectSourceKind(subject.Reference.Kind)
		if err != nil || !kindOK || resolved.ProjectID != projectID ||
			resolved.StableID != subject.Reference.SourceID || authorization.Kind != expectedKind ||
			authorization.SourceID != subject.Reference.SourceID ||
			authorization.CaptureScope.ProjectID != projectID ||
			!sameRecordSubjectVisibility(authorization.CaptureScope, subject.CaptureAuthorization.CaptureScope) {
			return nil, ErrRecordSubjectUnavailable
		}
		authorizations = append(authorizations, authorization)
	}
	return authorizations, nil
}

func normalizeCurrentRecordAuthorizationSnapshot(
	recordID string,
	projectID recordauth.ProjectID,
	input currentRecordAuthorizationSnapshot,
) (currentRecordAuthorizationSnapshot, error) {
	if input.recordID != recordID || input.projectID != projectID ||
		!validStoredRecordIdentity(input.recordID, "rec_") ||
		!validStoredRecordIdentity(input.currentRevisionID, "rrv_") ||
		input.lockVersion == 0 || input.authorizationEpoch == 0 ||
		records.ValidateLifecycle(input.lifecycle) != nil {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	visibility, err := normalizeCanonicalRecordSubjectVisibility(input.visibility)
	if err != nil || visibility.ProjectID != projectID {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}

	subjects := make([]RecordSubjectReadInput, 0, len(input.subjects))
	references := make([]records.SubjectReference, 0, len(input.subjects))
	for _, subject := range input.subjects {
		normalized, _, err := normalizeRecordSubjectReadInput(subject)
		if err != nil || normalized.CaptureAuthorization.CaptureScope.ProjectID != projectID {
			return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
		}
		subjects = append(subjects, normalized)
		references = append(references, normalized.Reference)
	}
	if _, err := records.NormalizeSubjectReferences(references); err != nil {
		return currentRecordAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}

	input.visibility = visibility
	input.subjects = subjects
	return input, nil
}

func normalizeRecordRevisionAuthorizationSnapshot(
	recordID string,
	revisionID string,
	projectID recordauth.ProjectID,
	input recordRevisionAuthorizationSnapshot,
) (recordRevisionAuthorizationSnapshot, error) {
	if input.recordID != recordID || input.revisionID != revisionID ||
		!validStoredRecordIdentity(input.revisionID, "rrv_") {
		return recordRevisionAuthorizationSnapshot{}, ErrRecordSubjectUnavailable
	}
	current, err := normalizeCurrentRecordAuthorizationSnapshot(recordID, projectID, currentRecordAuthorizationSnapshot{
		recordID:           input.recordID,
		projectID:          input.projectID,
		lifecycle:          input.lifecycle,
		currentRevisionID:  input.currentRevisionID,
		lockVersion:        input.lockVersion,
		authorizationEpoch: input.authorizationEpoch,
		visibility:         input.visibility,
		subjects:           input.subjects,
	})
	if err != nil {
		return recordRevisionAuthorizationSnapshot{}, err
	}
	input.visibility = current.visibility
	input.subjects = current.subjects
	return input, nil
}

func validStoredRecordIdentity(value, prefix string) bool {
	if len(value) < len(prefix)+1 || len(value) > len(prefix)+64 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

type vpsRecordSubjectAdapter struct {
	source vpsRecordSubjectSource
}

func newVPSRecordSubjectAdapter(source vpsRecordSubjectSource) records.SubjectSourceAdapter {
	return &vpsRecordSubjectAdapter{source: source}
}

func (*vpsRecordSubjectAdapter) Kind() records.SubjectKind {
	return records.SubjectKindVPS
}

func (adapter *vpsRecordSubjectAdapter) Resolve(
	ctx context.Context,
	actor recordauth.ActorScope,
	reference records.SubjectReference,
) (records.ResolvedSubject, error) {
	if ctx == nil || adapter == nil || nilRecordSubjectDependency(adapter.source) {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	if err := validateRecordSubjectAdapterRequest(actor, reference, records.SubjectKindVPS); err != nil {
		return records.ResolvedSubject{}, err
	}
	record, err := adapter.source.loadVPSRecordSubject(ctx, reference.SourceID)
	if err != nil {
		return records.ResolvedSubject{}, mapRecordSubjectSourceError(err, vpsassets.ErrVPSAssetNotFound)
	}
	fields := map[string]string{"display_name": record.DisplayName}
	addRecordSubjectSnapshotField(fields, "provider", record.Provider)
	addRecordSubjectSnapshotField(fields, "region", record.Region)
	return newLiveRecordSubject(
		records.SubjectKindVPS,
		recordauth.SourceKindVPS,
		record.VPSID,
		"/vps/"+record.VPSID,
		fields,
	)
}

type monitoringRecordSubjectAdapter struct {
	source monitoringRecordSubjectSource
}

func newMonitoringInstanceRecordSubjectAdapter(source monitoringRecordSubjectSource) records.SubjectSourceAdapter {
	return &monitoringRecordSubjectAdapter{source: source}
}

func (*monitoringRecordSubjectAdapter) Kind() records.SubjectKind {
	return records.SubjectKindMonitoringInstance
}

func (adapter *monitoringRecordSubjectAdapter) Resolve(
	ctx context.Context,
	actor recordauth.ActorScope,
	reference records.SubjectReference,
) (records.ResolvedSubject, error) {
	if ctx == nil || adapter == nil || nilRecordSubjectDependency(adapter.source) {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	if err := validateRecordSubjectAdapterRequest(actor, reference, records.SubjectKindMonitoringInstance); err != nil {
		return records.ResolvedSubject{}, err
	}
	record, err := adapter.source.loadMonitoringRecordSubject(ctx, reference.SourceID)
	if err != nil {
		return records.ResolvedSubject{}, mapRecordSubjectSourceError(err, monitoringinstances.ErrMonitoringInstanceNotFound)
	}
	fields := map[string]string{"display_name": record.DisplayName}
	addRecordSubjectSnapshotField(fields, "version", record.AgentVersion)
	return newLiveRecordSubject(
		records.SubjectKindMonitoringInstance,
		recordauth.SourceKindMonitoringInstance,
		record.MonitoringInstanceID,
		"/monitoring/"+record.MonitoringInstanceID,
		fields,
	)
}

type targetRecordSubjectAdapter struct {
	source targetRecordSubjectSource
}

func newTargetRecordSubjectAdapter(source targetRecordSubjectSource) records.SubjectSourceAdapter {
	return &targetRecordSubjectAdapter{source: source}
}

func (*targetRecordSubjectAdapter) Kind() records.SubjectKind {
	return records.SubjectKindTarget
}

func (adapter *targetRecordSubjectAdapter) Resolve(
	ctx context.Context,
	actor recordauth.ActorScope,
	reference records.SubjectReference,
) (records.ResolvedSubject, error) {
	if ctx == nil || adapter == nil || nilRecordSubjectDependency(adapter.source) {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	if err := validateRecordSubjectAdapterRequest(actor, reference, records.SubjectKindTarget); err != nil {
		return records.ResolvedSubject{}, err
	}
	record, err := adapter.source.loadTargetRecordSubject(ctx, reference.SourceID)
	if err != nil {
		return records.ResolvedSubject{}, mapRecordSubjectSourceError(err, targets.ErrTargetNotFound)
	}
	fields := map[string]string{"display_name": record.DisplayName}
	addRecordSubjectSnapshotField(fields, "target_type", record.TargetType)
	return newLiveRecordSubject(
		records.SubjectKindTarget,
		recordauth.SourceKindTarget,
		record.TargetID,
		"/targets/"+record.TargetID,
		fields,
	)
}

func (resolver *RecordSubjectReadResolver) Resolve(
	ctx context.Context,
	actor recordauth.ActorScope,
	input RecordSubjectReadInput,
) (records.ResolvedSubject, error) {
	if ctx == nil || resolver == nil {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	normalizedInput, sourceKind, err := normalizeRecordSubjectReadInput(input)
	if err != nil || normalizedActor.ProjectID != normalizedInput.CaptureAuthorization.CaptureScope.ProjectID {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}

	current, err := resolver.live.Resolve(ctx, normalizedActor, normalizedInput.Reference)
	if err == nil {
		return refreshLiveRecordSubject(normalizedInput, current)
	}
	if !errors.Is(err, ErrRecordSubjectNotFound) {
		return records.ResolvedSubject{}, fmt.Errorf("%w: resolve live source", ErrRecordSubjectUnavailable)
	}
	if nilRecordSubjectDependency(resolver.tombstones) {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}

	evidence, err := resolver.tombstones.ResolveWitnessedRecordSubjectTombstone(
		ctx,
		normalizedInput.CaptureAuthorization.CaptureScope.ProjectID,
		sourceKind,
		normalizedInput.Reference.SourceID,
	)
	if err != nil {
		return records.ResolvedSubject{}, fmt.Errorf("%w: resolve witnessed source deletion", ErrRecordSubjectUnavailable)
	}
	return resolveTombstonedRecordSubject(normalizedInput, sourceKind, evidence)
}

func newLiveRecordSubject(
	subjectKind records.SubjectKind,
	sourceKind recordauth.SourceKind,
	stableID string,
	liveRoute string,
	fields map[string]string,
) (records.ResolvedSubject, error) {
	projectID := recordauth.ProjectIDDefault
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      projectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: recordSubjectSourcePolicyRevisionV1,
	})
	if err != nil {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         sourceKind,
		SourceID:     stableID,
		State:        recordauth.SourceStateLive,
		CaptureScope: scope,
		CurrentScope: &scope,
	})
	if err != nil {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	snapshot, err := records.NewSubjectIdentitySnapshot(subjectKind, fields)
	if err != nil {
		return records.ResolvedSubject{}, fmt.Errorf("%w: identity snapshot", ErrRecordSubjectUnavailable)
	}
	return records.ResolvedSubject{
		ProjectID:            projectID,
		StableID:             stableID,
		IdentitySnapshot:     snapshot,
		LiveRoute:            liveRoute,
		CaptureAuthorization: authorization,
	}, nil
}

func refreshLiveRecordSubject(
	input RecordSubjectReadInput,
	current records.ResolvedSubject,
) (records.ResolvedSubject, error) {
	currentAuthorization, err := normalizeCanonicalRecordSubjectAuthorization(current.CaptureAuthorization)
	if err != nil || currentAuthorization.State != recordauth.SourceStateLive || currentAuthorization.CurrentScope == nil ||
		current.ProjectID != input.CaptureAuthorization.CaptureScope.ProjectID || current.StableID != input.Reference.SourceID {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         currentAuthorization.Kind,
		SourceID:     currentAuthorization.SourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: input.CaptureAuthorization.CaptureScope,
		CurrentScope: currentAuthorization.CurrentScope,
	})
	if err != nil {
		return records.ResolvedSubject{}, fmt.Errorf("%w: current source scope", ErrRecordSubjectUnavailable)
	}
	return records.ResolvedSubject{
		ProjectID:            current.ProjectID,
		StableID:             current.StableID,
		IdentitySnapshot:     input.IdentitySnapshot,
		LiveRoute:            current.LiveRoute,
		CaptureAuthorization: authorization,
	}, nil
}

func resolveTombstonedRecordSubject(
	input RecordSubjectReadInput,
	sourceKind recordauth.SourceKind,
	evidence WitnessedRecordSubjectTombstone,
) (records.ResolvedSubject, error) {
	projectID := input.CaptureAuthorization.CaptureScope.ProjectID
	if evidence.Version != WitnessedRecordSubjectTombstoneVersionV1 || evidence.ProjectID != projectID ||
		evidence.Kind != sourceKind || evidence.SourceID != input.Reference.SourceID {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	floor, err := normalizeCanonicalRecordSubjectVisibility(evidence.AuthorizationFloor)
	if err != nil || floor.CanonicalHash != evidence.AuthorizationFloorDigest {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	lastLive, err := normalizeCanonicalRecordSubjectVisibility(evidence.LastLiveScope)
	if err != nil {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:       recordauth.SourceAuthorizationVersionV1,
		Kind:          sourceKind,
		SourceID:      input.Reference.SourceID,
		State:         recordauth.SourceStateTombstoned,
		CaptureScope:  input.CaptureAuthorization.CaptureScope,
		FinalFloor:    &floor,
		LastLiveScope: &lastLive,
	})
	if err != nil {
		return records.ResolvedSubject{}, fmt.Errorf("%w: tombstone authorization", ErrRecordSubjectUnavailable)
	}
	return records.ResolvedSubject{
		ProjectID:            projectID,
		StableID:             input.Reference.SourceID,
		IdentitySnapshot:     input.IdentitySnapshot,
		CaptureAuthorization: authorization,
	}, nil
}

func normalizeRecordSubjectReadInput(
	input RecordSubjectReadInput,
) (RecordSubjectReadInput, recordauth.SourceKind, error) {
	sourceKind, ok := recordSubjectSourceKind(input.Reference.Kind)
	if !ok {
		return RecordSubjectReadInput{}, "", ErrRecordSubjectUnavailable
	}
	snapshot, err := records.NewSubjectIdentitySnapshot(input.IdentitySnapshot.Kind(), input.IdentitySnapshot.Fields())
	if err != nil || snapshot.Kind() != input.Reference.Kind {
		return RecordSubjectReadInput{}, "", ErrRecordSubjectUnavailable
	}
	authorization, err := normalizeCanonicalRecordSubjectAuthorization(input.CaptureAuthorization)
	if err != nil || authorization.Kind != sourceKind || authorization.SourceID != input.Reference.SourceID {
		return RecordSubjectReadInput{}, "", ErrRecordSubjectUnavailable
	}
	return RecordSubjectReadInput{
		Reference:            input.Reference,
		IdentitySnapshot:     snapshot,
		CaptureAuthorization: authorization,
	}, sourceKind, nil
}

func normalizeCanonicalRecordSubjectAuthorization(
	input recordauth.SourceAuthorization,
) (recordauth.SourceAuthorization, error) {
	normalized, err := recordauth.NormalizeSourceAuthorization(input)
	if err != nil || normalized.Digest != input.Digest ||
		!sameRecordSubjectVisibility(input.CaptureScope, normalized.CaptureScope) {
		return recordauth.SourceAuthorization{}, ErrRecordSubjectUnavailable
	}
	switch input.State {
	case recordauth.SourceStateLive:
		if input.CurrentScope == nil || normalized.CurrentScope == nil ||
			!sameRecordSubjectVisibility(*input.CurrentScope, *normalized.CurrentScope) {
			return recordauth.SourceAuthorization{}, ErrRecordSubjectUnavailable
		}
	case recordauth.SourceStateTombstoned:
		if input.FinalFloor == nil || normalized.FinalFloor == nil || input.LastLiveScope == nil || normalized.LastLiveScope == nil ||
			!sameRecordSubjectVisibility(*input.FinalFloor, *normalized.FinalFloor) ||
			!sameRecordSubjectVisibility(*input.LastLiveScope, *normalized.LastLiveScope) {
			return recordauth.SourceAuthorization{}, ErrRecordSubjectUnavailable
		}
	default:
		return recordauth.SourceAuthorization{}, ErrRecordSubjectUnavailable
	}
	return normalized, nil
}

func normalizeCanonicalRecordSubjectVisibility(
	input recordauth.VisibilityScope,
) (recordauth.VisibilityScope, error) {
	normalized, err := recordauth.NormalizeVisibilityScope(input)
	if err != nil || !sameRecordSubjectVisibility(input, normalized) {
		return recordauth.VisibilityScope{}, ErrRecordSubjectUnavailable
	}
	return normalized, nil
}

func sameRecordSubjectVisibility(left, right recordauth.VisibilityScope) bool {
	if left.Version != right.Version || left.Kind != right.Kind || left.ProjectID != right.ProjectID ||
		left.PolicyVersion != right.PolicyVersion || left.PolicyRevision != right.PolicyRevision ||
		left.CanonicalHash != right.CanonicalHash || len(left.AllowedRoles) != len(right.AllowedRoles) ||
		len(left.AllowedGroupIDs) != len(right.AllowedGroupIDs) {
		return false
	}
	for index := range left.AllowedRoles {
		if left.AllowedRoles[index] != right.AllowedRoles[index] {
			return false
		}
	}
	for index := range left.AllowedGroupIDs {
		if left.AllowedGroupIDs[index] != right.AllowedGroupIDs[index] {
			return false
		}
	}
	return true
}

func recordSubjectSourceKind(kind records.SubjectKind) (recordauth.SourceKind, bool) {
	switch kind {
	case records.SubjectKindVPS:
		return recordauth.SourceKindVPS, true
	case records.SubjectKindMonitoringInstance:
		return recordauth.SourceKindMonitoringInstance, true
	case records.SubjectKindTarget:
		return recordauth.SourceKindTarget, true
	default:
		return "", false
	}
}

func validateRecordSubjectAdapterRequest(
	actor recordauth.ActorScope,
	reference records.SubjectReference,
	adapterKind records.SubjectKind,
) error {
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil || normalizedActor.ProjectID != recordauth.ProjectIDDefault {
		return ErrRecordSubjectUnavailable
	}
	if err := records.ValidateSubjectReference(reference); err != nil {
		return err
	}
	if reference.Kind != adapterKind {
		return fmt.Errorf("%w: adapter kind", records.ErrInvalidSubjectReference)
	}
	return nil
}

func mapRecordSubjectSourceError(err, notFound error) error {
	if errors.Is(err, notFound) {
		return fmt.Errorf("%w: %w", ErrRecordSubjectNotFound, notFound)
	}
	return fmt.Errorf("%w: source repository: %w", ErrRecordSubjectUnavailable, err)
}

func addRecordSubjectSnapshotField(fields map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		fields[key] = value
	}
}

func nilRecordSubjectDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
