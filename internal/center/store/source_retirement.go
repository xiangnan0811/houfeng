package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
)

// ErrSourceRetirementUnavailable identifies a failed or unproven retirement
// stage. An error never authorizes undoing an existing prepare or APP barrier.
var ErrSourceRetirementUnavailable = errors.New("source retirement unavailable")

// SourceRetirementService is an offline administrative producer. Its witness
// credentials must not be supplied to Center or to a deletion worker.
// The caller owns the APP pool and independent witness client.
type SourceRetirementService struct {
	app     *pgxpool.Pool
	gate    AdmissionGate
	witness *sourceretirement.Authority
}

type SourceRetirementRequest struct {
	Source           sourceretirement.SourceKey
	RetirementID     string
	ActorUserID      string
	ExpectedRevision [32]byte
	FinalFloor       recordauth.VisibilityScope
}

type SourceRetirementPreview struct {
	Generation uint64
	Source     sourceretirement.SourceKey
	Actor      recordauth.ActorScope
	Revision   [32]byte
	LastLive   recordauth.VisibilityScope
}

// PreparedSourceRetirement retains the original private observation. It cannot
// be constructed, refreshed, or modified into authority by an external caller.
type PreparedSourceRetirement struct {
	owner   *SourceRetirementService
	request SourceRetirementRequest
	preview SourceRetirementPreview
	prepare sourceretirement.Prepare
}

func NewSourceRetirementService(app *pgxpool.Pool, gate AdmissionGate, witness *sourceretirement.Authority) (*SourceRetirementService, error) {
	if app == nil || gate == nil || witness == nil {
		return nil, sourceRetirementFailure("configure", nil)
	}
	return &SourceRetirementService{app: app, gate: gate, witness: witness}, nil
}

// Preview observes the current actor and source after acquiring the restricted
// SQL admission/graph boundary. It performs no witness I/O.
func (service *SourceRetirementService) Preview(ctx context.Context, actorUserID string, key sourceretirement.SourceKey) (SourceRetirementPreview, error) {
	if err := service.checkSourceRetirementContext(ctx); err != nil {
		return SourceRetirementPreview{}, err
	}
	if err := recordauth.ValidateActorUserID(actorUserID); err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("validate actor", err)
	}
	lastLive, err := sourceRetirementLiveScope(key)
	if err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("validate source", err)
	}
	tx, err := service.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("begin preview", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.gate.Admit(ctx, tx); err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("admit preview", err)
	}
	var generation int64
	var deploymentID string
	var revision []byte
	var groupIDs []string
	if err := tx.QueryRow(ctx, `select generation, deployment_id, source_revision, group_ids
		from public.source_retirement_preview($1, $2, $3)`, actorUserID, string(key.Kind), key.ID).Scan(
		&generation, &deploymentID, &revision, &groupIDs,
	); err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("read preview", err)
	}
	if generation <= 0 || deploymentID != key.DeploymentID || len(revision) != sha256.Size {
		return SourceRetirementPreview{}, sourceRetirementFailure("validate preview projection", nil)
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: actorUserID, Role: recordauth.RoleProjectAdmin, ProjectID: key.ProjectID, GroupIDs: groupIDs,
	})
	if err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("normalize preview actor", err)
	}
	preview := SourceRetirementPreview{Generation: uint64(generation), Source: key, Actor: actor, LastLive: lastLive}
	copy(preview.Revision[:], revision)
	if preview.Revision == ([32]byte{}) {
		return SourceRetirementPreview{}, sourceRetirementFailure("validate preview revision", nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return SourceRetirementPreview{}, sourceRetirementFailure("commit preview", err)
	}
	return preview, nil
}

// Prepare first checks the caller's original CAS, then durably records only
// intent in W2. A lost response is not retried or assigned another identity.
func (service *SourceRetirementService) Prepare(ctx context.Context, request SourceRetirementRequest) (PreparedSourceRetirement, error) {
	if !sourceRetirementOpaqueID(request.RetirementID) || request.ExpectedRevision == ([32]byte{}) {
		return PreparedSourceRetirement{}, sourceRetirementFailure("validate request identity or revision", nil)
	}
	floor, err := sourceRetirementCanonicalScope(request.FinalFloor, request.Source.ProjectID)
	if err != nil {
		return PreparedSourceRetirement{}, sourceRetirementFailure("validate final floor", err)
	}
	// Keep defensive normalized copies, not caller-owned grant slices.
	request.FinalFloor = floor
	preview, err := service.Preview(ctx, request.ActorUserID, request.Source)
	if err != nil {
		return PreparedSourceRetirement{}, err
	}
	if request.ExpectedRevision != preview.Revision {
		return PreparedSourceRetirement{}, sourceRetirementFailure("compare original revision", nil)
	}
	if err := sourceRetirementTransition(request.Source, preview.LastLive, floor); err != nil {
		return PreparedSourceRetirement{}, sourceRetirementFailure("validate final transition", err)
	}
	fingerprint := sourceRetirementFingerprint(request)
	// Preview has ended its APP transaction before crossing the W2 boundary.
	prepared, err := service.witness.Prepare(ctx, request.Source, request.RetirementID, fingerprint)
	if err != nil {
		return PreparedSourceRetirement{}, sourceRetirementFailure("persist witness prepare", err)
	}
	if prepared.ID != request.RetirementID || prepared.Source != request.Source ||
		prepared.OriginGeneration != preview.Generation || prepared.Fingerprint != fingerprint || prepared.Sequence == 0 {
		return PreparedSourceRetirement{}, sourceRetirementFailure("validate durable prepare", nil)
	}
	return PreparedSourceRetirement{owner: service, request: request, preview: preview, prepare: prepared}, nil
}

// CommitBarrier permanently freezes the original scopes and creates the outbox
// in the same APP commit. On an uncertain result, use Deliver with the original
// retirement ID to resolve committed state; do not compensate or refresh CAS.
func (service *SourceRetirementService) CommitBarrier(ctx context.Context, prepared PreparedSourceRetirement) ([32]byte, error) {
	if err := service.checkSourceRetirementContext(ctx); err != nil {
		return [32]byte{}, err
	}
	if prepared.owner != service || prepared.preview.Generation == 0 || prepared.preview.Generation > math.MaxInt64 ||
		prepared.prepare.OriginGeneration != prepared.preview.Generation || prepared.request.ExpectedRevision != prepared.preview.Revision {
		return [32]byte{}, sourceRetirementFailure("validate prepared capability", nil)
	}
	tx, err := service.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return [32]byte{}, sourceRetirementFailure("begin barrier", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.gate.Admit(ctx, tx); err != nil {
		return [32]byte{}, sourceRetirementFailure("admit barrier", err)
	}
	request := prepared.request
	var digest []byte
	if err := tx.QueryRow(ctx, `select barrier_digest from public.source_retirement_commit(
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		int64(prepared.preview.Generation), request.Source.DeploymentID, request.RetirementID,
		string(request.Source.Kind), request.Source.ID, request.ActorUserID, request.ExpectedRevision[:],
		prepared.prepare.Fingerprint[:], prepared.preview.LastLive.CanonicalBytes(), request.FinalFloor.CanonicalBytes(),
	).Scan(&digest); err != nil {
		return [32]byte{}, sourceRetirementFailure("commit restricted barrier", err)
	}
	var result [32]byte
	copy(result[:], digest)
	if len(digest) != sha256.Size || result == ([32]byte{}) {
		return [32]byte{}, sourceRetirementFailure("validate barrier digest", nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return [32]byte{}, sourceRetirementFailure("commit barrier transaction", err)
	}
	return result, nil
}

// Deliver asserts only a committed barrier/outbox pair. W2 checks the producer
// principal and generation again at effect time; an APP read is not a permit.
func (service *SourceRetirementService) Deliver(ctx context.Context, retirementID string) error {
	barrier, err := service.readSourceRetirementBarrier(ctx, retirementID)
	if err != nil {
		return err
	}
	appended, err := service.witness.Append(ctx, retirementID, barrier.digest, barrier.lastLive, barrier.request.FinalFloor)
	if err != nil {
		return sourceRetirementFailure("append committed terminal", err)
	}
	observation, err := service.witness.Observe(ctx, barrier.request.Source)
	if err != nil {
		return sourceRetirementFailure("observe delivered terminal", err)
	}
	if observation.Terminal == nil {
		return sourceRetirementFailure("require delivered terminal", nil)
	}
	appendedBytes, err := sourceretirement.EncodeTerminal(appended)
	if err != nil {
		return sourceRetirementFailure("encode appended terminal", err)
	}
	observedBytes, err := sourceretirement.EncodeTerminal(*observation.Terminal)
	if err != nil {
		return sourceRetirementFailure("encode observed terminal", err)
	}
	if !bytes.Equal(appendedBytes, observedBytes) {
		return sourceRetirementFailure("compare appended and observed terminals", nil)
	}
	return service.installVerifiedPin(ctx, barrier, observation)
}

// InstallPin independently observes trusted W2 evidence for the original APP
// barrier. It never accepts caller-supplied terminal bytes or pending intent.
func (service *SourceRetirementService) InstallPin(ctx context.Context, retirementID string) error {
	barrier, err := service.readSourceRetirementBarrier(ctx, retirementID)
	if err != nil {
		return err
	}
	observation, err := service.witness.Observe(ctx, barrier.request.Source)
	if err != nil {
		return sourceRetirementFailure("observe terminal for pin", err)
	}
	return service.installVerifiedPin(ctx, barrier, observation)
}

type sourceRetirementBarrier struct {
	request    SourceRetirementRequest
	generation uint64
	origin     uint64
	domain     sourceretirement.DomainIdentity
	lastLive   recordauth.VisibilityScope
	digest     [32]byte
}

func (service *SourceRetirementService) readSourceRetirementBarrier(ctx context.Context, retirementID string) (sourceRetirementBarrier, error) {
	if err := service.checkSourceRetirementContext(ctx); err != nil {
		return sourceRetirementBarrier{}, err
	}
	if !sourceRetirementOpaqueID(retirementID) {
		return sourceRetirementBarrier{}, sourceRetirementFailure("validate original retirement identity", nil)
	}
	tx, err := service.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("begin committed barrier read", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var barrier sourceRetirementBarrier
	var generation, origin, epoch int64
	var revision, fingerprint, lastLive, floor, digest, outboxDigest []byte
	// This one statement coherently observes admission, direct-login purpose,
	// physical domain and committed payload. Do not call gate.Admit here: this
	// transaction is read-only and must not acquire source admission SHARE locks.
	err = tx.QueryRow(ctx, `select b.deployment_id, b.project_id, b.source_kind, b.source_id,
		b.retirement_id, b.actor_id, b.expected_source_revision, b.request_fingerprint,
		b.origin_generation, b.last_live, b.final_floor, b.barrier_digest, o.barrier_digest,
		a.generation, a.witness_domain_id, a.witness_system_identifier,
		a.witness_database_oid, a.witness_database_name, a.witness_identity_epoch
		from public.source_retirement_barriers b
		join public.source_retirement_outbox o on o.retirement_id = b.retirement_id
		join public.source_authority_admission a on a.singleton and a.mode = 'active'
			and a.deployment_id = b.deployment_id
		join public.source_application_principals p on p.generation = a.generation
			and p.principal = session_user and p.purpose = 'admin'
		where b.retirement_id = $1`, retirementID).Scan(
		&barrier.request.Source.DeploymentID, &barrier.request.Source.ProjectID,
		&barrier.request.Source.Kind, &barrier.request.Source.ID, &barrier.request.RetirementID,
		&barrier.request.ActorUserID, &revision, &fingerprint, &origin, &lastLive, &floor,
		&digest, &outboxDigest, &generation, &barrier.domain.ID, &barrier.domain.SystemIdentifier,
		&barrier.domain.DatabaseOID, &barrier.domain.DatabaseName, &epoch,
	)
	if err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("read committed barrier and admission", err)
	}
	if generation <= 0 || origin <= 0 || origin > generation || epoch <= 0 ||
		barrier.request.RetirementID != retirementID || len(revision) != sha256.Size ||
		len(fingerprint) != sha256.Size || len(digest) != sha256.Size || !bytes.Equal(digest, outboxDigest) ||
		len(lastLive) == 0 || len(lastLive) > 4096 || len(floor) == 0 || len(floor) > 4096 {
		return sourceRetirementBarrier{}, sourceRetirementFailure("validate committed barrier projection", nil)
	}
	barrier.generation, barrier.origin, barrier.domain.Epoch = uint64(generation), uint64(origin), uint64(epoch)
	copy(barrier.request.ExpectedRevision[:], revision)
	copy(barrier.digest[:], digest)
	if barrier.request.ExpectedRevision == ([32]byte{}) || barrier.digest == ([32]byte{}) {
		return sourceRetirementBarrier{}, sourceRetirementFailure("validate committed nonzero digests", nil)
	}
	if _, err := sourceRetirementLiveScope(barrier.request.Source); err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("validate committed source", err)
	}
	if err := recordauth.ValidateActorUserID(barrier.request.ActorUserID); err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("validate committed actor", err)
	}
	barrier.lastLive, err = recordauth.ParseCanonicalVisibilityScope(lastLive)
	if err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("parse committed last live", err)
	}
	barrier.request.FinalFloor, err = recordauth.ParseCanonicalVisibilityScope(floor)
	if err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("parse committed floor", err)
	}
	if err := sourceRetirementTransition(barrier.request.Source, barrier.lastLive, barrier.request.FinalFloor); err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("validate committed transition", err)
	}
	expectedFingerprint := sourceRetirementFingerprint(barrier.request)
	if !bytes.Equal(fingerprint, expectedFingerprint[:]) {
		return sourceRetirementBarrier{}, sourceRetirementFailure("compare committed request fingerprint", nil)
	}
	var originBytes [8]byte
	binary.BigEndian.PutUint64(originBytes[:], barrier.origin)
	expectedDigest := sourceRetirementHash("HOUFENG-SOURCE-BARRIER-V2", originBytes[:],
		[]byte(barrier.request.Source.DeploymentID), []byte(barrier.request.Source.ProjectID), []byte(retirementID),
		[]byte(barrier.request.Source.Kind), []byte(barrier.request.Source.ID), []byte(barrier.request.ActorUserID),
		revision, fingerprint, lastLive, floor)
	if expectedDigest != barrier.digest {
		return sourceRetirementBarrier{}, sourceRetirementFailure("compare committed barrier digest", nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return sourceRetirementBarrier{}, sourceRetirementFailure("end committed barrier read", err)
	}
	return barrier, nil
}

// Only Observe results obtained by the two entrypoints above reach this helper.
// All W2 verification ends before opening the admitted APP write transaction.
func (service *SourceRetirementService) installVerifiedPin(ctx context.Context, barrier sourceRetirementBarrier, observation sourceretirement.Observation) error {
	terminal := observation.Terminal
	if terminal == nil || observation.LegacyBlocked || observation.CoverageUnknown ||
		observation.Mode != "active" || !observation.HandoffCompleted || observation.Domain != barrier.domain ||
		observation.Generation != barrier.generation || observation.Generation > math.MaxInt64 ||
		terminal.Domain != observation.Domain || terminal.Source != barrier.request.Source ||
		terminal.RetirementID != barrier.request.RetirementID || terminal.OriginGeneration != barrier.origin ||
		terminal.BarrierDigest != barrier.digest || terminal.Sequence == 0 || terminal.Sequence > math.MaxInt64 ||
		terminal.Sequence > observation.HeadSequence ||
		!bytes.Equal(terminal.LastLive.CanonicalBytes(), barrier.lastLive.CanonicalBytes()) ||
		!bytes.Equal(terminal.FinalFloor.CanonicalBytes(), barrier.request.FinalFloor.CanonicalBytes()) {
		return sourceRetirementFailure("validate trusted terminal against barrier", nil)
	}
	canonical, err := sourceretirement.EncodeTerminal(*terminal)
	if err != nil {
		return sourceRetirementFailure("encode verified pin", err)
	}
	if err := sourceretirement.VerifyTerminal(canonical, *terminal, observation.TerminalHash); err != nil {
		return sourceRetirementFailure("verify pin projection", err)
	}
	tx, err := service.app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadWrite})
	if err != nil {
		return sourceRetirementFailure("begin pin", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.gate.Admit(ctx, tx); err != nil {
		return sourceRetirementFailure("admit pin", err)
	}
	if _, err := tx.Exec(ctx, `select public.source_retirement_install_pin($1, $2, $3, $4, $5, $6, $7)`,
		int64(observation.Generation), barrier.request.RetirementID, int64(barrier.origin),
		barrier.digest[:], canonical, observation.TerminalHash[:], int64(terminal.Sequence),
	); err != nil {
		return sourceRetirementFailure("install restricted pin", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return sourceRetirementFailure("commit pin", err)
	}
	return nil
}

func sourceRetirementLiveScope(key sourceretirement.SourceKey) (recordauth.VisibilityScope, error) {
	if err := recordplatform.ValidateDeploymentID(recordplatform.DeploymentID(key.DeploymentID)); err != nil {
		return recordauth.VisibilityScope{}, err
	}
	if err := recordauth.ValidateProjectID(key.ProjectID); err != nil {
		return recordauth.VisibilityScope{}, err
	}
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: key.ProjectID, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		return recordauth.VisibilityScope{}, err
	}
	_, err = recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: key.Kind, SourceID: key.ID,
		State: recordauth.SourceStateLive, CaptureScope: scope, CurrentScope: &scope,
	})
	if err != nil {
		return recordauth.VisibilityScope{}, err
	}
	return scope, nil
}

func sourceRetirementCanonicalScope(input recordauth.VisibilityScope, project recordauth.ProjectID) (recordauth.VisibilityScope, error) {
	normalized, err := recordauth.NormalizeVisibilityScope(input)
	if err != nil {
		return recordauth.VisibilityScope{}, err
	}
	if normalized.ProjectID != project || normalized.CanonicalHash != input.CanonicalHash ||
		!slices.Equal(normalized.AllowedRoles, input.AllowedRoles) ||
		!slices.Equal(normalized.AllowedGroupIDs, input.AllowedGroupIDs) || len(normalized.CanonicalBytes()) > 4096 {
		return recordauth.VisibilityScope{}, recordauth.ErrInvalidVisibilityScope
	}
	return normalized, nil
}

func sourceRetirementTransition(key sourceretirement.SourceKey, lastLive, floor recordauth.VisibilityScope) error {
	if lastLive.ProjectID != key.ProjectID || floor.ProjectID != key.ProjectID {
		return recordauth.ErrInvalidSourceAuthorization
	}
	_, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: key.Kind, SourceID: key.ID,
		State: recordauth.SourceStateTombstoned, CaptureScope: lastLive, LastLiveScope: &lastLive, FinalFloor: &floor,
	})
	return err
}

func sourceRetirementFingerprint(request SourceRetirementRequest) [32]byte {
	return sourceRetirementHash("HOUFENG-SOURCE-RETIREMENT-REQUEST-V2", nil,
		[]byte(request.Source.DeploymentID), []byte(request.Source.ProjectID), []byte(request.Source.Kind),
		[]byte(request.Source.ID), []byte(request.RetirementID), []byte(request.ActorUserID),
		request.ExpectedRevision[:], request.FinalFloor.CanonicalBytes())
}

func sourceRetirementHash(domain string, prefix []byte, fields ...[]byte) [32]byte {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(domain))
	_, _ = hasher.Write(prefix)
	var length [4]byte
	for _, field := range fields {
		binary.BigEndian.PutUint32(length[:], uint32(len(field)))
		_, _ = hasher.Write(length[:])
		_, _ = hasher.Write(field)
	}
	var result [32]byte
	hasher.Sum(result[:0])
	return result
}

func sourceRetirementOpaqueID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index := range value {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func (service *SourceRetirementService) checkSourceRetirementContext(ctx context.Context) error {
	if service == nil || service.app == nil || service.gate == nil || service.witness == nil || ctx == nil {
		return sourceRetirementFailure("validate service context", nil)
	}
	if err := ctx.Err(); err != nil {
		return sourceRetirementFailure("check context", err)
	}
	return nil
}

// Keep DSNs, passwords, source identities and untrusted SQL detail out of log
// messages while retaining typed causes for errors.Is/errors.As.
type sourceRetirementError struct {
	stage string
	cause error
}

func (err *sourceRetirementError) Error() string {
	return "source retirement unavailable: " + err.stage
}

func (err *sourceRetirementError) Is(target error) bool {
	return target == ErrSourceRetirementUnavailable
}

func (err *sourceRetirementError) Unwrap() error {
	return err.cause
}

func sourceRetirementFailure(stage string, cause error) error {
	return &sourceRetirementError{stage: stage, cause: cause}
}
