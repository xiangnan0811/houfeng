package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

// PostgresRecordDeletionIssuer binds preview and execution to the admitted APP
// repository. Its optional source witness is needed for enrolled source lineages.
type PostgresRecordDeletionIssuer struct {
	repository   *PostgresRecordDeletionRepository
	deploymentID recordplatform.DeploymentID
	birth        *recordbirth.Coordinator
	backup       recorddeletion.DeletionBackupInventorySource
	options      recorddeletion.ServiceOptions
	witness      atomic.Pointer[sourceretirement.Authority]
}

func NewPostgresRecordDeletionIssuer(repository *PostgresRecordDeletionRepository, deploymentID recordplatform.DeploymentID, birth *recordbirth.Coordinator, backup recorddeletion.DeletionBackupInventorySource, options recorddeletion.ServiceOptions) (*PostgresRecordDeletionIssuer, error) {
	if repository == nil || repository.platform == nil || repository.platform.beginTx == nil || nilRecordSubjectDependency(repository.platform.gate) || repository.pool == nil ||
		repository.newReservationID == nil || repository.newOperationID == nil || birth == nil || nilRecordSubjectDependency(backup) ||
		recordplatform.ValidateDeploymentID(deploymentID) != nil ||
		(recordplatform.LeaseClaimInputV1{OwnerID: options.OwnerID, LeaseDuration: options.OwnerLeaseDuration}).Validate() != nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return &PostgresRecordDeletionIssuer{repository: repository, deploymentID: deploymentID, birth: birth, backup: backup, options: options}, nil
}

// ConfigureWitness installs the source authority used for subsequent requests.
// A nil authority leaves enrolled source lineages unavailable.
func (issuer *PostgresRecordDeletionIssuer) ConfigureWitness(witness *sourceretirement.Authority) error {
	if issuer == nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	issuer.witness.Store(witness)
	return nil
}

func (issuer *PostgresRecordDeletionIssuer) authorizationSource() (*PostgresCurrentRecordAuthorizationSource, error) {
	if issuer == nil || issuer.repository == nil || issuer.repository.pool == nil || issuer.repository.platform == nil {
		return nil, recorddeletion.ErrDeletionSafetyUnavailable
	}
	live := transactionRecordSubjectSource{db: issuer.repository.pool}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		newVPSRecordSubjectAdapter(live),
		newMonitoringInstanceRecordSubjectAdapter(live),
		newTargetRecordSubjectAdapter(live),
	})
	if err != nil {
		return nil, err
	}
	tombstones, err := NewWitnessedRecordSubjectTombstoneReader(issuer.deploymentID, recordauth.ProjectIDDefault, issuer.witness.Load(), issuer.repository.pool)
	if err != nil {
		return nil, err
	}
	return NewPostgresCurrentRecordAuthorizationSource(issuer.repository.pool, NewRecordSubjectReadResolver(subjects, tombstones), issuer.repository.platform.gate), nil
}

type issuerPreparation struct {
	proof         []byte
	observation   recordbirth.Observation
	authorization *preparedCurrentRecordAuthorization
	backup        recorddeletion.BackupInventorySnapshot
}

func (issuer *PostgresRecordDeletionIssuer) prepare(ctx context.Context, object recordplatform.ObjectRef) (issuerPreparation, error) {
	var prepared issuerPreparation
	var err error
	prepared.proof, err = issuer.birth.ReadContinuity(ctx)
	if err != nil {
		return prepared, err
	}
	if prepared.proof == nil {
		prepared.proof = []byte{}
	}
	if recordidentity.Classify(object.ObjectID) == recordidentity.Certifiable {
		prepared.observation, err = issuer.birth.ReadCertificationObservation(ctx, object.ObjectID)
		if err != nil {
			return prepared, err
		}
	}
	prepared.backup, err = recorddeletion.ReadDeletionBackupInventory(ctx, issuer.backup, object)
	if err != nil {
		return prepared, err
	}
	source, err := issuer.authorizationSource()
	if err != nil {
		return prepared, err
	}
	resolver, err := source.prepareCurrentAuthorization(ctx, []string{object.ObjectID})
	if err != nil {
		return prepared, err
	}
	var ok bool
	prepared.authorization, ok = resolver.(*preparedCurrentRecordAuthorization)
	if !ok || prepared.authorization == nil {
		return prepared, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return prepared, nil
}

func lockIssuerActorSources(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, prepared *preparedCurrentRecordAuthorization, recordID string) error {
	var role string
	var disabled *time.Time
	if err := tx.QueryRow(ctx, `select role,disabled_at from public.users where user_id=$1 for update`, actor.UserID).Scan(&role, &disabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return recordauth.ErrDenied
		}
		return err
	}
	if role != "admin" || disabled != nil || actor.Role != recordauth.RoleProjectAdmin {
		return recordauth.ErrDenied
	}
	rows, err := tx.Query(ctx, recordAuthorizationGroupIDsSQL, string(actor.ProjectID), actor.UserID)
	if err != nil {
		return err
	}
	var groups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, group)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	actual, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: actor.UserID, Role: recordauth.RoleProjectAdmin, ProjectID: actor.ProjectID, GroupIDs: groups})
	if err != nil || actual.CanonicalHash() != actor.CanonicalHash() {
		return recordauth.ErrDenied
	}
	for _, group := range groups {
		if _, err := tx.Exec(ctx, `select 1 from public.record_access_groups where project_id=$1 and group_id=$2 for update`, string(actor.ProjectID), group); err != nil {
			return err
		}
	}
	for _, group := range groups {
		if err := lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "membership", "group_id": group, "user_id": actor.UserID}); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, recordAuthorizationGroupIDsSQL, string(actor.ProjectID), actor.UserID)
	if err != nil {
		return err
	}
	var currentGroups []string
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			rows.Close()
			return err
		}
		currentGroups = append(currentGroups, group)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	currentActor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: actor.UserID, Role: recordauth.RoleProjectAdmin, ProjectID: actor.ProjectID, GroupIDs: currentGroups})
	if err != nil || currentActor.CanonicalHash() != actor.CanonicalHash() {
		return recordauth.ErrDenied
	}
	if prepared == nil {
		return nil
	}
	route, ok := prepared.routes[recordID]
	if !ok {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if route.denied != nil {
		if errors.Is(route.denied, records.ErrRecordNotFound) || errors.Is(route.denied, records.ErrRecordDeletionReserved) {
			return fmt.Errorf("%w: %w", recorddeletion.ErrDeletionPreviewStale, route.denied)
		}
		return route.denied
	}
	type sourceKey struct {
		kind records.SubjectKind
		id   string
	}
	sources := make([]sourceKey, 0, len(route.references))
	seen := make(map[sourceKey]struct{}, len(route.references))
	for ref := range route.references {
		key := sourceKey{ref.Kind, ref.SourceID}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			sources = append(sources, key)
		}
	}
	rank := func(kind records.SubjectKind) int {
		switch kind {
		case records.SubjectKindVPS:
			return 0
		case records.SubjectKindMonitoringInstance:
			return 1
		case records.SubjectKindTarget:
			return 2
		default:
			return 3
		}
	}
	sort.Slice(sources, func(i, j int) bool {
		if rank(sources[i].kind) != rank(sources[j].kind) {
			return rank(sources[i].kind) < rank(sources[j].kind)
		}
		return sources[i].id < sources[j].id
	})
	for _, source := range sources {
		var statement string
		switch source.kind {
		case records.SubjectKindVPS:
			statement = `select 1 from public.vps_assets where vps_id=$1 for update`
		case records.SubjectKindMonitoringInstance:
			statement = `select 1 from public.monitoring_instances where monitoring_instance_id=$1 for update`
		case records.SubjectKindTarget:
			statement = `select 1 from public.targets where target_id=$1 for update`
		default:
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		if _, err := tx.Exec(ctx, statement, source.id); err != nil {
			return err
		}
	}
	return nil
}

func lockIssuerRecordPrefix(ctx context.Context, tx pgx.Tx, recordID string) (recordplatform.ContentEpoch, error) {
	rows, err := tx.Query(ctx, `select reservation_id from public.deletion_reservations where project_id='default' and object_kind='record' and object_id=$1 order by reservation_id collate "C"`, recordID)
	if err != nil {
		return 0, err
	}
	var reservations []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		reservations = append(reservations, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range reservations {
		if err := lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "reservation", "project_id": "default", "record_id": recordID, "reservation_id": id}); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch) values('default','record',$1,0) on conflict(project_id,object_kind,object_id) do nothing`, recordID); err != nil {
		return 0, err
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id=$1 for update`, recordID).Scan(&epoch); err != nil {
		return 0, err
	}
	if epoch < 0 {
		return 0, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "deletion_fence", "project_id": "default", "record_id": recordID}); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `select 1 from public.object_content_leases where project_id='default' and object_kind='record' and object_id=$1 for update`, recordID); err != nil {
		return 0, err
	}
	if err := lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "gate", "project_id": "default", "record_id": recordID}); err != nil {
		return 0, err
	}
	return recordplatform.ContentEpoch(epoch), nil
}

func lockIssuerPrefix(ctx context.Context, tx pgx.Tx) error {
	if err := lockAssetGraphForSync(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock($1,$2)`, recordAdvisoryClassID, comparisonDecisionFenceKey)
	return err
}

type issuerSnapshot struct {
	binding               recorddeletion.IssuerPreviewBinding
	summary               recorddeletion.IssuerPreviewSummary
	authorizationEvidence []byte
}

func readIssuerSnapshot(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, object recordplatform.ObjectRef, prepared issuerPreparation) (issuerSnapshot, error) {
	var result issuerSnapshot
	if err := lockIssuerActorSources(ctx, tx, actor, prepared.authorization, object.ObjectID); err != nil {
		return result, err
	}
	epoch, err := lockIssuerRecordPrefix(ctx, tx, object.ObjectID)
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `select public.record_birth_continuity_assert($1::bytea)`, prepared.proof); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `select 1 from public.records where project_id='default' and record_id=$1 for update`, object.ObjectID); err != nil {
		return result, err
	}
	if recordidentity.Classify(object.ObjectID) == recordidentity.Certifiable {
		if _, err := verifyRecordBirthCertificationOnTx(ctx, tx, object.ObjectID, prepared.observation); err != nil {
			return result, err
		}
	}
	current, err := prepared.authorization.resolveCurrentAuthorizationInTransaction(ctx, tx, actor, object.ObjectID)
	if err != nil {
		return result, err
	}
	scope := recordauth.ResourceScope{Version: recordauth.ResourceScopeVersionV1, ProjectID: current.Evidence.ProjectID,
		Visibility: current.Evidence.Visibility, Sources: current.Evidence.Sources}
	if err := recordauth.Authorize(actor, recordauth.CapabilityRecordPermanentDelete, scope); err != nil {
		return result, err
	}
	if _, _, err := lockAttachmentQuotaAccount(ctx, tx, "default"); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `select public.record_revision_blob_fence()`); err != nil {
		return result, err
	}
	command, err := json.Marshal(struct {
		RecordID string `json:"record_id"`
	}{object.ObjectID})
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `select public.record_deletion_inventory_lock($1::bytea)`, command); err != nil {
		return result, err
	}
	observed, err := observeComparisonDeletionInTransaction(ctx, tx, object.ObjectID)
	if err != nil {
		return result, err
	}
	graph, processor, obligation, err := readIssuerInventories(ctx, tx, object.ObjectID, observed.IndexDigest)
	if err != nil {
		return result, err
	}
	target := recorddeletion.PreviewTarget{Object: object, CurrentRevisionID: current.CurrentRevisionID,
		LockVersion: current.LockVersion, AuthorizationEpoch: current.AuthorizationEpoch, ContentDeliveryEpoch: epoch, DependencyGraphDigest: graph}
	health := make(map[recorddeletion.AdapterName]recorddeletion.AdapterHealthSnapshot, 9)
	previews := make(map[recorddeletion.AdapterName]recorddeletion.AdapterPreviewSnapshot, 9)
	type adapterReader struct {
		name    recorddeletion.AdapterName
		health  func(context.Context, pgx.Tx) (recorddeletion.AdapterHealthSnapshot, error)
		preview func(context.Context, pgx.Tx, recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error)
	}
	readers := []adapterReader{
		{recorddeletion.AdapterNameRecordCore, recordCoreHealthInTransaction, previewRecordCoreInTransaction},
		{recorddeletion.AdapterNameRecordAttachments,
			func(ctx context.Context, tx pgx.Tx) (recorddeletion.AdapterHealthSnapshot, error) {
				return attachmentDeletionHealthInTransaction(ctx, tx)
			},
			func(ctx context.Context, tx pgx.Tx, target recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
				return previewAttachmentDeletionInTransaction(ctx, tx, target)
			}},
		{recorddeletion.AdapterNameRecordEvidence, evidenceDeletionHealthInTransaction, previewEvidenceDeletionInTransaction},
		{recorddeletion.AdapterNameRecordMarkdownClient, markdownClientDeletionHealthInTransaction, previewMarkdownClientDeletionInTransaction},
		{recorddeletion.AdapterNameRecordSearch, searchDeletionHealthInTransaction, previewSearchDeletionInTransaction},
		{recorddeletion.AdapterNameRecordActivityProjection, activityDeletionHealthInTransaction, previewActivityDeletionInTransaction},
		{recorddeletion.AdapterNameRecordCollaboration, collaborationDeletionHealthInTransaction, previewCollaborationDeletionInTransaction},
		{recorddeletion.AdapterNameRecordPortability, portabilityDeletionHealthInTransaction, previewPortabilityDeletionInTransaction},
	}
	for _, reader := range readers {
		h, err := reader.health(ctx, tx)
		if err != nil {
			return result, err
		}
		p, err := reader.preview(ctx, tx, target)
		if err != nil {
			return result, err
		}
		health[reader.name], previews[reader.name] = h, p
	}
	health[recorddeletion.AdapterNameRecordComparison] = observed.Health
	previews[recorddeletion.AdapterNameRecordComparison], err = deriveComparisonPreviewFromObserved(observed, target)
	if err != nil {
		return result, err
	}
	readiness, preview, summary, err := recorddeletion.DigestIssuerAdapterSnapshots(health, previews)
	if err != nil {
		return result, err
	}
	summary.BackupInventory = prepared.backup
	if err := summary.Validate(); err != nil {
		return result, err
	}
	binding := recorddeletion.IssuerPreviewBinding{Actor: actor, Object: object, Lifecycle: string(current.Lifecycle),
		CurrentRevisionID: current.CurrentRevisionID, LockVersion: current.LockVersion, AuthorizationEpoch: current.AuthorizationEpoch,
		ContentDeliveryEpoch: epoch, Authorization: scope, AdapterReadinessDigest: readiness, AdapterPreviewDigest: preview,
		DependencyGraphDigest: graph, ProcessorInventoryDigest: processor, ObligationInventoryDigest: obligation, BackupInventory: prepared.backup}
	if err := binding.Validate(); err != nil {
		return result, err
	}
	evidence, err := json.Marshal(scope)
	if err != nil {
		return result, err
	}
	return issuerSnapshot{binding: binding, summary: summary, authorizationEvidence: evidence}, nil
}

func issuerFingerprint(deploymentID recordplatform.DeploymentID, actor recordauth.ActorScope, object recordplatform.ObjectRef, binding [32]byte) ([32]byte, error) {
	hash := sha256.New()
	field := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	field("houfeng.record-deletion.request-scope.v1")
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], 1)
	_, _ = hash.Write(version[:])
	field(string(deploymentID))
	field(object.ProjectID)
	field("record_permanent_delete")
	field(object.ObjectKind)
	field(object.ObjectID)
	var scope [32]byte
	copy(scope[:], hash.Sum(nil))
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version: recordplatform.RequestFingerprintVersionV1, OperationKind: recordplatform.OperationKindRecordPermanentDelete,
		ProjectID: recordplatform.ProjectID(actor.ProjectID), ActorScopeDigest: actor.CanonicalHash(), RequestScopeDigest: scope, PayloadDigest: binding})
	if err != nil {
		return [32]byte{}, err
	}
	return fingerprint.PersistedBytes()
}

func issuerRequest(ctx context.Context, issuer *PostgresRecordDeletionIssuer, actorInput recordauth.ActorScope, recordID string) (recordauth.ActorScope, recordplatform.ObjectRef, error) {
	if ctx == nil || issuer == nil || issuer.repository == nil {
		return recordauth.ActorScope{}, recordplatform.ObjectRef{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return recordauth.ActorScope{}, recordplatform.ObjectRef{}, err
	}
	actor, err := recordauth.NormalizeActorScope(actorInput)
	if err != nil || !recordidentity.IsValid(recordID) {
		return actor, recordplatform.ObjectRef{}, recorddeletion.ErrInvalidDeletionPreview
	}
	object := recordplatform.ObjectRef{ProjectID: string(actor.ProjectID), ObjectKind: "record", ObjectID: recordID}
	if object.Validate() != nil {
		return actor, object, recorddeletion.ErrInvalidDeletionPreview
	}
	return actor, object, nil
}

func issuerError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "55000" {
		switch postgresError.Message {
		case "record deletion has a live prior lease":
			return fmt.Errorf("%w: %w", recordplatform.ErrObjectContentLeaseLive, err)
		case "deletion preview snapshot changed", "deletion preview expired":
			return fmt.Errorf("%w: %w", recorddeletion.ErrDeletionPreviewStale, err)
		}
	}
	return err
}

func (issuer *PostgresRecordDeletionIssuer) Preview(ctx context.Context, request recorddeletion.PreviewRequest) (recorddeletion.IssuerPreviewResult, error) {
	var result recorddeletion.IssuerPreviewResult
	actor, object, err := issuerRequest(ctx, issuer, request.Actor, request.RecordID)
	if err != nil {
		return result, err
	}
	prepared, err := issuer.prepare(ctx, object)
	if err != nil {
		return result, err
	}
	reservationID, err := issuer.repository.newReservationID()
	if err != nil {
		return result, err
	}
	token, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		return result, err
	}
	commitment, err := token.Commitment(issuer.deploymentID, recordplatform.ProjectID(actor.ProjectID))
	if err != nil {
		return result, err
	}
	err = issuer.repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		tx := transaction.tx
		if err := lockIssuerPrefix(ctx, tx); err != nil {
			return err
		}
		snapshot, err := readIssuerSnapshot(ctx, tx, actor, object, prepared)
		if err != nil {
			return err
		}
		bindingDigest, err := recorddeletion.DigestIssuerPreviewBinding(snapshot.binding)
		if err != nil {
			return err
		}
		fingerprint, err := issuerFingerprint(issuer.deploymentID, actor, object, bindingDigest)
		if err != nil {
			return err
		}
		var backupDigest any
		if prepared.backup.InventoryDigest != nil {
			backupDigest = hex.EncodeToString(prepared.backup.InventoryDigest[:])
		}
		actorDigest := actor.CanonicalHash()
		body, err := json.Marshal(map[string]any{
			"deployment_id": string(issuer.deploymentID), "record_id": object.ObjectID, "actor_id": actor.UserID,
			"reservation_id": reservationID, "actor_scope_digest": hex.EncodeToString(actorDigest[:]),
			"token_commitment": hex.EncodeToString(commitment[:]), "request_fingerprint": hex.EncodeToString(fingerprint[:]),
			"binding_digest": hex.EncodeToString(bindingDigest[:]), "authorization_evidence": hex.EncodeToString(snapshot.authorizationEvidence),
			"lifecycle": snapshot.binding.Lifecycle, "revision_id": snapshot.binding.CurrentRevisionID,
			"lock_version": snapshot.binding.LockVersion, "authorization_epoch": snapshot.binding.AuthorizationEpoch,
			"content_epoch": uint64(snapshot.binding.ContentDeliveryEpoch), "dependency_digest": hex.EncodeToString(snapshot.binding.DependencyGraphDigest[:]),
			"processor_digest":         hex.EncodeToString(snapshot.binding.ProcessorInventoryDigest[:]),
			"obligation_digest":        hex.EncodeToString(snapshot.binding.ObligationInventoryDigest[:]),
			"adapter_readiness_digest": hex.EncodeToString(snapshot.binding.AdapterReadinessDigest[:]),
			"adapter_preview_digest":   hex.EncodeToString(snapshot.binding.AdapterPreviewDigest[:]), "backup_inventory_digest": backupDigest,
		})
		if err != nil {
			return err
		}
		var storedReservation string
		var storedCommitment []byte
		var created, expires time.Time
		if err := tx.QueryRow(ctx, `select reservation_id,deletion_token_commitment,created_at,expires_at from public.record_deletion_preview_create($1::bytea)`, body).Scan(&storedReservation, &storedCommitment, &created, &expires); err != nil {
			return err
		}
		if storedReservation != reservationID || len(storedCommitment) != len(commitment) || string(storedCommitment) != string(commitment[:]) ||
			created.IsZero() || expires.Sub(created) != recorddeletion.DeletionPreviewTTL {
			return recorddeletion.ErrDeletionSafetyUnavailable
		}
		result = recorddeletion.IssuerPreviewResult{ReservationID: reservationID, Token: token, ExpiresAt: expires.UTC(), Summary: snapshot.summary}
		return nil
	})
	if err != nil {
		return recorddeletion.IssuerPreviewResult{}, issuerError(err)
	}
	return result, nil
}

type issuerStoredPreview struct {
	commitment    [32]byte
	actorDigest   [32]byte
	fingerprint   [32]byte
	binding       [32]byte
	authorization []byte
	operationID   string
	deploymentID  string
}

func lockIssuerPreview(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, object recordplatform.ObjectRef, reservationID string) (issuerStoredPreview, error) {
	var result issuerStoredPreview
	body, err := json.Marshal(map[string]string{"reservation_id": reservationID, "actor_id": actor.UserID, "record_id": object.ObjectID})
	if err != nil {
		return result, err
	}
	var commitment, actorDigest, fingerprint, binding []byte
	var deployment, project, record, reservation string
	err = tx.QueryRow(ctx, `select reservation_id,project_id,record_id,deployment_id,deletion_token_commitment,
		actor_scope_digest,request_fingerprint,binding_digest,authorization_evidence,coalesce(operation_id,'')
		from public.record_deletion_preview_lock($1::bytea)`, body).Scan(&reservation, &project, &record, &deployment,
		&commitment, &actorDigest, &fingerprint, &binding, &result.authorization, &result.operationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, recorddeletion.ErrDeletionPreviewNotFound
	}
	if err != nil {
		return result, err
	}
	if reservation != reservationID || project != object.ProjectID || record != object.ObjectID ||
		len(commitment) != 32 || len(actorDigest) != 32 || len(fingerprint) != 32 || len(binding) != 32 || len(result.authorization) == 0 || deployment == "" {
		return result, recorddeletion.ErrDeletionSafetyUnavailable
	}
	copy(result.commitment[:], commitment)
	copy(result.actorDigest[:], actorDigest)
	copy(result.fingerprint[:], fingerprint)
	copy(result.binding[:], binding)
	result.deploymentID = deployment
	return result, nil
}

func validateIssuerStoredPreview(issuer *PostgresRecordDeletionIssuer, actor recordauth.ActorScope, object recordplatform.ObjectRef, request recorddeletion.ExecuteRequest, stored issuerStoredPreview) error {
	if stored.deploymentID != string(issuer.deploymentID) || !request.Token.MatchesCommitment(issuer.deploymentID, recordplatform.ProjectID(actor.ProjectID), stored.commitment) || stored.actorDigest != actor.CanonicalHash() {
		return recorddeletion.ErrDeletionRequestTokenReused
	}
	fingerprint, err := issuerFingerprint(issuer.deploymentID, actor, object, stored.binding)
	if err != nil || fingerprint != stored.fingerprint {
		return recorddeletion.ErrDeletionRequestTokenReused
	}
	var scope recordauth.ResourceScope
	if err := json.Unmarshal(stored.authorization, &scope); err != nil {
		return recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := recordauth.Authorize(actor, recordauth.CapabilityRecordPermanentDelete, scope); err != nil {
		return err
	}
	return nil
}

func (issuer *PostgresRecordDeletionIssuer) issueLocked(ctx context.Context, tx pgx.Tx, actor recordauth.ActorScope, object recordplatform.ObjectRef, reservationID, operationID string, stored issuerStoredPreview) (recorddeletion.DeletionOperation, error) {
	var operation recorddeletion.DeletionOperation
	body, err := json.Marshal(map[string]any{
		"token_commitment": hex.EncodeToString(stored.commitment[:]), "actor_id": actor.UserID,
		"actor_scope_digest": hex.EncodeToString(stored.actorDigest[:]), "record_id": object.ObjectID,
		"binding_digest": hex.EncodeToString(stored.binding[:]), "authorization_evidence": hex.EncodeToString(stored.authorization),
		"request_fingerprint": hex.EncodeToString(stored.fingerprint[:]), "reservation_id": reservationID,
		"operation_id": operationID, "owner_id": issuer.options.OwnerID, "confirmed": true,
	})
	if err != nil {
		return operation, err
	}
	var id, reservation, project, state, reason string
	var version int64
	var receipt []byte
	if err := tx.QueryRow(ctx, `select operation_id,reservation_id,project_id,operation_state,reason_code,deletion_contract_version,
		coalesce(receipt_digest,''::bytea) from public.record_deletion_issue($1::bytea)`, body).Scan(&id, &reservation, &project, &state, &reason, &version, &receipt); err != nil {
		return operation, err
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `select fence_epoch from public.deletion_reservations where reservation_id=$1 and project_id=$2 and object_kind='record' and object_id=$3`, reservationID, object.ProjectID, object.ObjectID).Scan(&epoch); err != nil {
		return operation, err
	}
	if reservation != reservationID || project != object.ProjectID || version != int64(recorddeletion.DeletionContractV2) || epoch <= 0 || (len(receipt) != 0 && len(receipt) != 32) || id != operationID || (stored.operationID != "" && id != stored.operationID) {
		return operation, recorddeletion.ErrDeletionSafetyUnavailable
	}
	operation = recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: id, ReservationID: reservation,
		Object: object, ReasonCode: recorddeletion.DeletionReasonCode(reason), State: recorddeletion.DeletionState(state), FenceEpoch: recordplatform.ContentEpoch(epoch)}
	copy(operation.ReceiptDigest[:], receipt)
	if operation.Validate() != nil {
		return recorddeletion.DeletionOperation{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	return operation, nil
}

func (issuer *PostgresRecordDeletionIssuer) replayBound(ctx context.Context, actor recordauth.ActorScope, object recordplatform.ObjectRef, request recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, bool, error) {
	var operation recorddeletion.DeletionOperation
	var bound bool
	err := issuer.repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		tx := transaction.tx
		if err := lockIssuerPrefix(ctx, tx); err != nil {
			return err
		}
		stored, err := lockIssuerPreview(ctx, tx, actor, object, request.ReservationID)
		if err != nil {
			return err
		}
		if stored.operationID == "" {
			return nil
		}
		bound = true
		if err := validateIssuerStoredPreview(issuer, actor, object, request, stored); err != nil {
			return err
		}
		if err := lockIssuerActorSources(ctx, tx, actor, nil, object.ObjectID); err != nil {
			return err
		}
		operation, err = issuer.issueLocked(ctx, tx, actor, object, request.ReservationID, stored.operationID, stored)
		return err
	})
	return operation, bound, err
}

func (issuer *PostgresRecordDeletionIssuer) Execute(ctx context.Context, request recorddeletion.ExecuteRequest) (recorddeletion.DeletionOperation, error) {
	var operation recorddeletion.DeletionOperation
	actor, object, err := issuerRequest(ctx, issuer, request.Actor, request.RecordID)
	if err != nil {
		return operation, err
	}
	if request.ReasonCode != recorddeletion.DeletionReasonUserConfirmed || (recorddeletion.PreviewLookup{ReservationID: request.ReservationID, Object: object, Token: request.Token}).Validate() != nil {
		return operation, recorddeletion.ErrInvalidDeletionPreview
	}
	var bound bool
	err = issuer.repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		err := transaction.tx.QueryRow(ctx, `select operation_id is not null from public.record_deletion_previews where reservation_id=$1 and actor_id=$2 and project_id=$3 and record_id=$4`, request.ReservationID, actor.UserID, object.ProjectID, object.ObjectID).Scan(&bound)
		if errors.Is(err, pgx.ErrNoRows) {
			return recorddeletion.ErrDeletionPreviewNotFound
		}
		return err
	})
	if err != nil {
		return operation, err
	}
	var prepared issuerPreparation
	if !bound {
		prepared, err = issuer.prepare(ctx, object)
		if err != nil {
			replayed, found, replayErr := issuer.replayBound(ctx, actor, object, request)
			if found && replayErr == nil {
				return replayed, nil
			}
			if errors.Is(err, records.ErrRecordNotFound) || errors.Is(err, records.ErrRecordDeletionReserved) {
				return operation, errors.Join(recorddeletion.ErrDeletionPreviewStale, err, replayErr)
			}
			return operation, errors.Join(err, replayErr)
		}
	}
	operationID := ""
	if !bound {
		operationID, err = issuer.repository.newOperationID()
		if err != nil {
			return operation, err
		}
	}
	issued := false
	err = issuer.repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		tx := transaction.tx
		if err := lockIssuerPrefix(ctx, tx); err != nil {
			return err
		}
		stored, err := lockIssuerPreview(ctx, tx, actor, object, request.ReservationID)
		if err != nil {
			return err
		}
		if err := validateIssuerStoredPreview(issuer, actor, object, request, stored); err != nil {
			return err
		}
		if stored.operationID != "" {
			if err := lockIssuerActorSources(ctx, tx, actor, nil, object.ObjectID); err != nil {
				return err
			}
			operationID = stored.operationID
		} else {
			if prepared.authorization == nil {
				return recorddeletion.ErrDeletionPreviewStale
			}
			snapshot, err := readIssuerSnapshot(ctx, tx, actor, object, prepared)
			if err != nil {
				return err
			}
			if err := recorddeletion.CheckIssuerPreviewBinding(stored.binding, snapshot.binding); err != nil {
				return err
			}
			if string(snapshot.authorizationEvidence) != string(stored.authorization) {
				return recorddeletion.ErrDeletionPreviewStale
			}
		}
		operation, err = issuer.issueLocked(ctx, tx, actor, object, request.ReservationID, operationID, stored)
		if err == nil {
			issued = true
		}
		return err
	})
	if err == nil {
		return operation, nil
	}
	if issued {
		var resolved recorddeletion.DeletionOperation
		resolveErr := issuer.repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			tx := transaction.tx
			if err := lockIssuerPrefix(ctx, tx); err != nil {
				return err
			}
			stored, err := lockIssuerPreview(ctx, tx, actor, object, request.ReservationID)
			if err != nil {
				return err
			}
			if stored.operationID == "" || stored.operationID != operation.OperationID {
				return recorddeletion.ErrDeletionSafetyUnavailable
			}
			if err := validateIssuerStoredPreview(issuer, actor, object, request, stored); err != nil {
				return err
			}
			if err := lockIssuerActorSources(ctx, tx, actor, nil, object.ObjectID); err != nil {
				return err
			}
			resolved, err = issuer.issueLocked(ctx, tx, actor, object, request.ReservationID, operationID, stored)
			return err
		})
		if resolveErr == nil {
			return resolved, nil
		}
		return recorddeletion.DeletionOperation{}, errors.Join(issuerError(err), resolveErr)
	}
	return recorddeletion.DeletionOperation{}, issuerError(err)
}
