package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
)

// WitnessedRecordSubjectTombstoneReader accepts only W2 authority with an exact
// committed APP barrier and current-generation pin. A legacy floor is not proof.
type WitnessedRecordSubjectTombstoneReader struct {
	deploymentID recordplatform.DeploymentID
	projectID    recordauth.ProjectID
	witness      *sourceretirement.Authority
	local        currentRecordAuthorizationDB
}

var _ WitnessedRecordSubjectTombstoneSource = (*WitnessedRecordSubjectTombstoneReader)(nil)

func NewWitnessedRecordSubjectTombstoneReader(
	deploymentID recordplatform.DeploymentID,
	projectID recordauth.ProjectID,
	witness *sourceretirement.Authority,
	local currentRecordAuthorizationDB,
) (*WitnessedRecordSubjectTombstoneReader, error) {
	if deploymentID != "" {
		if err := recordplatform.ValidateDeploymentID(deploymentID); err != nil {
			return nil, fmt.Errorf("%w: deployment id", ErrRecordSubjectUnavailable)
		}
	}
	if projectID != recordauth.ProjectIDDefault {
		return nil, fmt.Errorf("%w: project id", ErrRecordSubjectUnavailable)
	}
	if nilRecordSubjectDependency(local) {
		local = nil
	}
	return &WitnessedRecordSubjectTombstoneReader{
		deploymentID: deploymentID, projectID: projectID, witness: witness, local: local,
	}, nil
}

// PreparedSourceAuthority retains private, reader-bound observations obtained
// before an APP business transaction. It is not an offline authority token.
type PreparedSourceAuthority struct {
	reader       *WitnessedRecordSubjectTombstoneReader
	observations map[sourceretirement.SourceKey]preparedSourceObservation
}

type preparedSourceObservation struct {
	admission sourceAuthorityAdmission
	// external distinguishes an observed W2 absence from explicit unenrollment.
	external bool
	witness  sourceretirement.Observation
}

type sourceAuthorityAdmission struct {
	enrolled     bool
	deploymentID string
	domain       sourceretirement.DomainIdentity
	generation   int64
	mode         string
	recoveryID   string
}

type sourceAuthorityFacts struct {
	admission                                       sourceAuthorityAdmission
	barrier                                         bool
	barrierDeployment, retirementID                 string
	originGeneration                                int64
	barrierDigest, lastLive, finalFloor             []byte
	pin                                             bool
	pinRetirementID                                 string
	pinGeneration, pinOriginGeneration, pinSequence int64
	pinBarrierDigest, pinCanonical, pinHash         []byte
	anyPin, unknown, legacy                         bool
	legacyDigest                                    []byte
}

// Prefetch must run outside APP business transactions. Duplicate exact source
// keys share one W2 observation; each later use still rereads local APP facts.
func (reader *WitnessedRecordSubjectTombstoneReader) Prefetch(ctx context.Context, keys []sourceretirement.SourceKey) (*PreparedSourceAuthority, error) {
	if ctx == nil || reader == nil || nilRecordSubjectDependency(reader.local) {
		return nil, ErrRecordSubjectUnavailable
	}
	prepared := &PreparedSourceAuthority{
		reader:       reader,
		observations: make(map[sourceretirement.SourceKey]preparedSourceObservation, len(keys)),
	}
	for _, key := range keys {
		if _, exists := prepared.observations[key]; exists {
			continue
		}
		if !reader.validKey(key) {
			return nil, ErrRecordSubjectUnavailable
		}
		facts, err := loadSourceAuthorityFacts(ctx, reader.local, key)
		if err != nil {
			return nil, err
		}
		observation, err := reader.observe(ctx, key, facts)
		if err != nil {
			return nil, err
		}
		prepared.observations[key] = observation
	}
	return prepared, nil
}

// ForTransaction never performs W2 I/O. Missing prefetched keys and changed
// enrollment identities fail closed rather than discovering new authority.
func (prepared *PreparedSourceAuthority) ForTransaction(tx pgx.Tx) WitnessedRecordSubjectTombstoneSource {
	return &preparedSourceAuthorityTransaction{prepared: prepared, tx: tx}
}

type preparedSourceAuthorityTransaction struct {
	prepared *PreparedSourceAuthority
	tx       pgx.Tx
}

func (source *preparedSourceAuthorityTransaction) ResolveWitnessedRecordSubjectTombstone(ctx context.Context, projectID recordauth.ProjectID, kind recordauth.SourceKind, sourceID string) (WitnessedRecordSubjectTombstone, error) {
	if ctx == nil || source == nil || source.prepared == nil || source.prepared.reader == nil || nilRecordSubjectDependency(source.tx) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	reader := source.prepared.reader
	key := reader.sourceKey(projectID, kind, sourceID)
	observation, ok := source.prepared.observations[key]
	if !ok || !reader.validKey(key) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	facts, err := loadSourceAuthorityFacts(ctx, source.tx, key)
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, err
	}
	return reader.resolveObserved(key, facts, observation)
}

func (reader *WitnessedRecordSubjectTombstoneReader) ResolveWitnessedRecordSubjectTombstone(ctx context.Context, projectID recordauth.ProjectID, kind recordauth.SourceKind, sourceID string) (WitnessedRecordSubjectTombstone, error) {
	if ctx == nil || reader == nil || nilRecordSubjectDependency(reader.local) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	key := reader.sourceKey(projectID, kind, sourceID)
	if !reader.validKey(key) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	facts, err := loadSourceAuthorityFacts(ctx, reader.local, key)
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, err
	}
	observation, err := reader.observe(ctx, key, facts)
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, err
	}
	return reader.resolveObserved(key, facts, observation)
}

func (reader *WitnessedRecordSubjectTombstoneReader) sourceKey(projectID recordauth.ProjectID, kind recordauth.SourceKind, sourceID string) sourceretirement.SourceKey {
	return sourceretirement.SourceKey{DeploymentID: string(reader.deploymentID), ProjectID: projectID, Kind: kind, ID: sourceID}
}

func (reader *WitnessedRecordSubjectTombstoneReader) validKey(key sourceretirement.SourceKey) bool {
	if key.DeploymentID != string(reader.deploymentID) || key.ProjectID != reader.projectID {
		return false
	}
	var prefix string
	switch key.Kind {
	case recordauth.SourceKindVPS:
		prefix = "vps_"
	case recordauth.SourceKindMonitoringInstance:
		prefix = "mi_"
	case recordauth.SourceKindTarget:
		prefix = "tg_"
	default:
		return false
	}
	if len(key.ID) != len(prefix)+16 || key.ID[:len(prefix)] != prefix {
		return false
	}
	for _, character := range key.ID[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func (reader *WitnessedRecordSubjectTombstoneReader) validateLocal(facts sourceAuthorityFacts) error {
	admission := facts.admission
	if !admission.enrolled {
		if reader.witness != nil || facts.barrier || facts.anyPin || facts.unknown || facts.legacy {
			return ErrRecordSubjectUnavailable
		}
		return nil
	}
	if reader.witness == nil || admission.mode != "active" || admission.generation <= 0 ||
		admission.deploymentID != string(reader.deploymentID) || admission.deploymentID == "" ||
		admission.domain.ID == "" || admission.domain.SystemIdentifier == "" || admission.domain.DatabaseOID == 0 ||
		admission.domain.DatabaseName == "" || admission.domain.Epoch == 0 || facts.unknown || (facts.barrier && !facts.pin) {
		return ErrRecordSubjectUnavailable
	}
	return nil
}

func (reader *WitnessedRecordSubjectTombstoneReader) observe(ctx context.Context, key sourceretirement.SourceKey, facts sourceAuthorityFacts) (preparedSourceObservation, error) {
	if err := reader.validateLocal(facts); err != nil {
		return preparedSourceObservation{}, err
	}
	prepared := preparedSourceObservation{admission: facts.admission}
	if !facts.admission.enrolled {
		return prepared, nil
	}
	observation, err := reader.witness.Observe(ctx, key)
	if err != nil {
		return preparedSourceObservation{}, fmt.Errorf("%w: source authority observation", ErrRecordSubjectUnavailable)
	}
	prepared.external, prepared.witness = true, observation
	return prepared, nil
}

func (reader *WitnessedRecordSubjectTombstoneReader) resolveObserved(key sourceretirement.SourceKey, facts sourceAuthorityFacts, prepared preparedSourceObservation) (WitnessedRecordSubjectTombstone, error) {
	if err := reader.validateLocal(facts); err != nil {
		return WitnessedRecordSubjectTombstone{}, err
	}
	if facts.admission != prepared.admission || facts.admission.enrolled != prepared.external {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	if !prepared.external {
		return WitnessedRecordSubjectTombstone{}, ErrWitnessedRecordSubjectTombstoneNotFound
	}
	observation := prepared.witness
	if observation.Mode != "active" || !observation.HandoffCompleted || observation.Generation != uint64(facts.admission.generation) ||
		observation.Domain != facts.admission.domain || observation.LegacyBlocked || observation.CoverageUnknown {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	terminal := observation.Terminal
	if terminal == nil {
		if facts.barrier || facts.anyPin || facts.unknown || facts.legacy {
			return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
		}
		return WitnessedRecordSubjectTombstone{}, ErrWitnessedRecordSubjectTombstoneNotFound
	}
	if !facts.barrier || !facts.pin || terminal.Source != key || terminal.Domain != observation.Domain ||
		terminal.OriginGeneration == 0 || terminal.OriginGeneration > observation.Generation ||
		terminal.Sequence == 0 || terminal.Sequence > observation.HeadSequence ||
		(terminal.Sequence == observation.HeadSequence && observation.TerminalHash != observation.HeadHash) ||
		facts.barrierDeployment != key.DeploymentID || facts.retirementID != terminal.RetirementID ||
		facts.originGeneration <= 0 || uint64(facts.originGeneration) != terminal.OriginGeneration ||
		!bytes.Equal(facts.barrierDigest, terminal.BarrierDigest[:]) ||
		!bytes.Equal(facts.lastLive, terminal.LastLive.CanonicalBytes()) || !bytes.Equal(facts.finalFloor, terminal.FinalFloor.CanonicalBytes()) ||
		facts.pinGeneration != facts.admission.generation || facts.pinRetirementID != terminal.RetirementID ||
		facts.pinOriginGeneration != facts.originGeneration || !bytes.Equal(facts.pinBarrierDigest, terminal.BarrierDigest[:]) ||
		facts.pinSequence <= 0 || uint64(facts.pinSequence) != terminal.Sequence || !bytes.Equal(facts.pinHash, observation.TerminalHash[:]) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	// Decode a fresh value so callers cannot mutate the retained observation's
	// scope slices through the returned projection.
	verified, err := sourceretirement.DecodeTerminal(facts.pinCanonical)
	if err != nil {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	canonical, err := sourceretirement.EncodeTerminal(*terminal)
	if err != nil || !bytes.Equal(canonical, facts.pinCanonical) || sha256.Sum256(canonical) != observation.TerminalHash {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	if facts.legacy && !bytes.Equal(facts.legacyDigest, terminal.FinalFloor.CanonicalHash[:]) {
		return WitnessedRecordSubjectTombstone{}, ErrRecordSubjectUnavailable
	}
	return WitnessedRecordSubjectTombstone{
		Version:   WitnessedRecordSubjectTombstoneVersionV1,
		ProjectID: key.ProjectID, Kind: key.Kind, SourceID: key.ID,
		AuthorizationFloor: verified.FinalFloor, LastLiveScope: verified.LastLive,
		AuthorizationFloorDigest: verified.FinalFloor.CanonicalHash,
	}, nil
}

// One statement supplies one APP snapshot, even in READ COMMITTED transactions.
// Old-generation pins are counted as history but never selected as current proof.
func loadSourceAuthorityFacts(ctx context.Context, db currentRecordAuthorizationDB, key sourceretirement.SourceKey) (sourceAuthorityFacts, error) {
	var facts sourceAuthorityFacts
	var epoch int64
	err := db.QueryRow(ctx, `
		select a.singleton is not null, coalesce(a.deployment_id, ''),
		       coalesce(a.witness_domain_id, ''), coalesce(a.witness_system_identifier, ''),
		       coalesce(a.witness_database_oid, 0::oid), coalesce(a.witness_database_name, ''),
		       coalesce(a.witness_identity_epoch, 0), coalesce(a.generation, 0),
		       coalesce(a.mode, ''), coalesce(a.recovery_id, ''),
		       b.source_id is not null, coalesce(b.deployment_id, ''), coalesce(b.retirement_id, ''),
		       coalesce(b.origin_generation, 0), b.barrier_digest, b.last_live, b.final_floor,
		       p.source_id is not null, coalesce(p.retirement_id, ''), coalesce(p.installed_generation, 0),
		       coalesce(p.origin_generation, 0), coalesce(p.terminal_sequence, 0),
		       p.barrier_digest, p.canonical_terminal, p.terminal_hash,
		       exists (select 1 from public.source_retirement_pins history
		               where (history.project_id, history.source_kind, history.source_id) = ($1, $2, $3)),
		       u.source_id is not null, l.source_id is not null, l.authorization_floor_digest
		from (values (true)) as requested(singleton)
		left join public.source_authority_admission a on a.singleton = requested.singleton
		left join public.source_retirement_barriers b
		  on (b.project_id, b.source_kind, b.source_id) = ($1, $2, $3)
		left join public.source_retirement_pins p
		  on (p.project_id, p.source_kind, p.source_id) = ($1, $2, $3)
		 and p.installed_generation = a.generation
		left join public.source_retirement_coverage_unknown u
		  on (u.project_id, u.source_kind, u.source_id) = ($1, $2, $3)
		left join public.source_deletion_tombstones l
		  on (l.project_id, l.source_kind, l.source_id) = ($1, $2, $3)
	`, string(key.ProjectID), string(key.Kind), key.ID).Scan(
		&facts.admission.enrolled, &facts.admission.deploymentID,
		&facts.admission.domain.ID, &facts.admission.domain.SystemIdentifier,
		&facts.admission.domain.DatabaseOID, &facts.admission.domain.DatabaseName,
		&epoch, &facts.admission.generation, &facts.admission.mode, &facts.admission.recoveryID,
		&facts.barrier, &facts.barrierDeployment, &facts.retirementID, &facts.originGeneration,
		&facts.barrierDigest, &facts.lastLive, &facts.finalFloor,
		&facts.pin, &facts.pinRetirementID, &facts.pinGeneration, &facts.pinOriginGeneration,
		&facts.pinSequence, &facts.pinBarrierDigest, &facts.pinCanonical, &facts.pinHash,
		&facts.anyPin, &facts.unknown, &facts.legacy, &facts.legacyDigest,
	)
	if err != nil || epoch < 0 {
		return sourceAuthorityFacts{}, fmt.Errorf("%w: APP source authority snapshot", ErrRecordSubjectUnavailable)
	}
	facts.admission.domain.Epoch = uint64(epoch)
	return facts, nil
}
