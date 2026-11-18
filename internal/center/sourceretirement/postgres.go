package sourceretirement

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
)

const terminalSelectColumns = `retirement_id, deployment_id, project_id,
	source_kind, source_id, origin_generation, barrier_digest, last_live,
	final_floor, sequence, previous_hash, canonical_entry, entry_hash`

const prepareSelectColumns = `retirement_id, prepare_sequence, deployment_id,
	project_id, source_kind, source_id, origin_generation, request_fingerprint`

const terminalBatchSize = 128

// Authority accesses only the configured independent W2 database. The pool's
// direct-login principal determines which methods PostgreSQL permits; possessing
// an Authority does not grant producer or recovery privileges.
type Authority struct {
	pool     *pgxpool.Pool
	expected DomainIdentity
}

// Prepare is immutable intent, not evidence that an APP barrier committed.
type Prepare struct {
	ID               string
	Sequence         uint64
	Source           SourceKey
	OriginGeneration uint64
	Fingerprint      [32]byte
}

// Observation is one coherent W2 snapshot. Mode and handoff state are exposed
// for the caller's admission decision, never converted into offline authority.
// Pending prepares alone do not establish retirement. A verified terminal
// dominates W2 coverage-unknown, but never undecodable W1 source history.
type Observation struct {
	Domain           DomainIdentity
	Generation       uint64
	Mode             string
	RecoveryID       string
	HandoffCompleted bool
	HeadSequence     uint64
	HeadHash         [32]byte
	Terminal         *Terminal
	TerminalHash     [32]byte
	CoverageUnknown  bool
	LegacyBlocked    bool
	PrepareCount     uint64
}

// NewAuthority binds the client to a complete expected physical domain. It does
// not open connections or take ownership of the pool.
func NewAuthority(pool *pgxpool.Pool, expected DomainIdentity) (*Authority, error) {
	if pool == nil {
		return nil, errors.New("source authority pool is required")
	}
	if err := validateAuthorityDomain(expected); err != nil {
		return nil, err
	}
	return &Authority{pool: pool, expected: expected}, nil
}

// Observe verifies the requested terminal through the trusted head without
// traversing the unrelated chain prefix or retaining the suffix in memory.
func (authority *Authority) Observe(ctx context.Context, key SourceKey) (Observation, error) {
	if err := authority.checkContext(ctx); err != nil {
		return Observation{}, err
	}
	if err := validateAuthoritySource(key); err != nil {
		return Observation{}, err
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Observation{}, authorityFailure("begin observation", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	observation, err := authority.snapshot(ctx, tx)
	if err != nil {
		return Observation{}, authorityFailure("read observation identity", err)
	}
	if observation.Mode != "active" || !observation.HandoffCompleted {
		return Observation{}, invalidTerminal("source authority admission")
	}
	enrollment, err := authority.readApplicationEnrollment(ctx, tx, observation, key.DeploymentID)
	if err != nil {
		return Observation{}, authorityFailure("read application enrollment", err)
	}
	if enrollment.ActivationDigest == nil {
		return Observation{}, invalidTerminal("application enrollment is not activated")
	}
	terminal, hash, err := scanAuthorityTerminal(tx.QueryRow(ctx, `select `+terminalSelectColumns+`
		from public.source_retirement_terminals
		where deployment_id = $1 and project_id = $2 and source_kind = $3 and source_id = $4`,
		key.DeploymentID, string(key.ProjectID), string(key.Kind), key.ID), authority.expected)
	if err == nil {
		if terminal.Source != key || terminal.OriginGeneration > observation.Generation {
			return Observation{}, invalidTerminal("observed source or origin generation")
		}
		if err := authority.verifySuffix(ctx, tx, terminal, hash, observation); err != nil {
			return Observation{}, authorityFailure("verify terminal suffix", err)
		}
		observation.Terminal = &terminal
		observation.TerminalHash = hash
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Observation{}, authorityFailure("read terminal", err)
	}

	var count int64
	if err := tx.QueryRow(ctx, `select count(*) from public.source_retirement_prepares
		where deployment_id = $1 and project_id = $2 and source_kind = $3 and source_id = $4`,
		key.DeploymentID, string(key.ProjectID), string(key.Kind), key.ID).Scan(&count); err != nil {
		return Observation{}, authorityFailure("count source prepares", err)
	}
	if count < 0 || (observation.Terminal != nil && count == 0) {
		return Observation{}, invalidTerminal("prepare count")
	}
	observation.PrepareCount = uint64(count)
	if err := tx.QueryRow(ctx, `select exists (
		select 1 from public.source_retirement_unknown u
		join public.source_retirement_prepares p using (retirement_id)
		where p.deployment_id = $1 and p.project_id = $2 and p.source_kind = $3 and p.source_id = $4
	), public.source_retirement_legacy_blocked($1,$2,$3,$4)`,
		key.DeploymentID, string(key.ProjectID), string(key.Kind), key.ID).Scan(
		&observation.CoverageUnknown, &observation.LegacyBlocked,
	); err != nil {
		return Observation{}, authorityFailure("read source coverage", err)
	}
	if observation.Terminal != nil {
		observation.CoverageUnknown = false
	}
	if err := tx.Commit(ctx); err != nil {
		return Observation{}, authorityFailure("commit observation", err)
	}
	return observation, nil
}

// Prepare persists the original operation identity exactly once. Errors,
// including uncertain commit results, are returned without allocating a new ID
// or retrying the mutation.
func (authority *Authority) Prepare(ctx context.Context, key SourceKey, retirementID string, fingerprint [32]byte) (Prepare, error) {
	if err := authority.checkContext(ctx); err != nil {
		return Prepare{}, err
	}
	if err := validateAuthoritySource(key); err != nil {
		return Prepare{}, err
	}
	if !validOpaqueID(retirementID) {
		return Prepare{}, invalidTerminal("retirement identity")
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Prepare{}, authorityFailure("begin prepare", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	observation, err := authority.snapshot(ctx, tx)
	if err != nil {
		return Prepare{}, authorityFailure("read prepare identity", err)
	}
	prepared, err := scanAuthorityPrepare(tx.QueryRow(ctx, `select `+prepareSelectColumns+`
		from public.source_retirement_prepare($1, $2, $3, $4, $5, $6)`,
		retirementID, key.DeploymentID, string(key.ProjectID), string(key.Kind), key.ID, fingerprint[:]))
	if err != nil {
		return Prepare{}, authorityFailure("prepare retirement", err)
	}
	if prepared.ID != retirementID || prepared.Source != key || prepared.Fingerprint != fingerprint ||
		prepared.OriginGeneration > observation.Generation {
		return Prepare{}, invalidTerminal("prepare replay projection")
	}
	if err := tx.Commit(ctx); err != nil {
		return Prepare{}, authorityFailure("commit prepare", err)
	}
	return prepared, nil
}

// Append asserts committed APP barrier facts as the trusted producer. The
// caller, not this client, must establish that those facts actually committed.
func (authority *Authority) Append(ctx context.Context, retirementID string, barrier [32]byte, lastLive, finalFloor recordauth.VisibilityScope) (Terminal, error) {
	return authority.append(ctx, "", retirementID, barrier, lastLive, finalFloor)
}

// Finalize uses the recovery-only entrypoint; PostgreSQL requires the bound
// current recovery principal, exact RID, recovering mode, and unsealed coverage.
func (authority *Authority) Finalize(ctx context.Context, recoveryID, retirementID string, barrier [32]byte, lastLive, finalFloor recordauth.VisibilityScope) (Terminal, error) {
	if !validOpaqueID(recoveryID) {
		return Terminal{}, invalidTerminal("recovery identity")
	}
	return authority.append(ctx, recoveryID, retirementID, barrier, lastLive, finalFloor)
}

func (authority *Authority) append(ctx context.Context, recoveryID, retirementID string, barrier [32]byte, lastLive, finalFloor recordauth.VisibilityScope) (Terminal, error) {
	if err := authority.checkContext(ctx); err != nil {
		return Terminal{}, err
	}
	if !validOpaqueID(retirementID) || barrier == ([32]byte{}) {
		return Terminal{}, invalidTerminal("retirement identity or barrier")
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Terminal{}, authorityFailure("begin append", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	observation, err := authority.snapshot(ctx, tx)
	if err != nil {
		return Terminal{}, authorityFailure("read append identity", err)
	}
	prepared, err := scanAuthorityPrepare(tx.QueryRow(ctx, `select `+prepareSelectColumns+`
		from public.source_retirement_prepares where retirement_id = $1`, retirementID))
	if err != nil {
		return Terminal{}, authorityFailure("read original prepare", err)
	}
	if prepared.ID != retirementID || prepared.OriginGeneration > observation.Generation {
		return Terminal{}, invalidTerminal("original prepare projection")
	}
	// Sequence one and its zero previous hash are only a local validation
	// envelope. They are never returned, stored, or used as authority evidence.
	payload := Terminal{
		Domain: authority.expected, Source: prepared.Source, RetirementID: prepared.ID,
		OriginGeneration: prepared.OriginGeneration, BarrierDigest: barrier,
		LastLive: lastLive, FinalFloor: finalFloor, Sequence: 1,
	}
	lastLiveBytes, floorBytes, err := validateTerminal(payload)
	if err != nil {
		return Terminal{}, err
	}
	var row pgx.Row
	if recoveryID == "" {
		row = tx.QueryRow(ctx, `select `+terminalSelectColumns+`
			from public.source_retirement_append($1, $2, $3, $4)`,
			retirementID, barrier[:], lastLiveBytes, floorBytes)
	} else {
		row = tx.QueryRow(ctx, `select `+terminalSelectColumns+`
			from public.source_retirement_finalize($1, $2, $3, $4, $5)`,
			recoveryID, retirementID, barrier[:], lastLiveBytes, floorBytes)
	}
	terminal, _, err := scanAuthorityTerminal(row, authority.expected)
	if err != nil {
		return Terminal{}, authorityFailure("append retirement", err)
	}
	if terminal.RetirementID != prepared.ID || terminal.Source != prepared.Source ||
		terminal.OriginGeneration != prepared.OriginGeneration || terminal.BarrierDigest != barrier ||
		!sameAuthorityScope(terminal.LastLive, lastLive) || !sameAuthorityScope(terminal.FinalFloor, finalFloor) {
		return Terminal{}, invalidTerminal("appended payload projection")
	}
	if err := tx.Commit(ctx); err != nil {
		// Do not retry, delete, or otherwise compensate an uncertain commit.
		return Terminal{}, authorityFailure("commit append", err)
	}
	return terminal, nil
}

func (authority *Authority) snapshot(ctx context.Context, tx pgx.Tx) (Observation, error) {
	var observation Observation
	var epoch, generation, sequence int64
	var recoveryID *string
	var headHash []byte
	if err := tx.QueryRow(ctx, `select domain_id, system_identifier, database_oid,
		database_name, identity_epoch, generation, mode, recovery_id,
		handoff_completed, head_sequence, head_hash
		from public.source_authority_snapshot()`).Scan(
		&observation.Domain.ID, &observation.Domain.SystemIdentifier, &observation.Domain.DatabaseOID,
		&observation.Domain.DatabaseName, &epoch, &generation, &observation.Mode, &recoveryID,
		&observation.HandoffCompleted, &sequence, &headHash,
	); err != nil {
		return Observation{}, err
	}
	if epoch <= 0 || generation <= 0 || sequence < 0 || len(headHash) != 32 {
		return Observation{}, invalidTerminal("authority snapshot projection")
	}
	observation.Domain.Epoch = uint64(epoch)
	observation.Generation = uint64(generation)
	observation.HeadSequence = uint64(sequence)
	copy(observation.HeadHash[:], headHash)
	if observation.Domain != authority.expected {
		return Observation{}, invalidTerminal("authority physical domain")
	}
	if (sequence == 0) != (observation.HeadHash == ([32]byte{})) {
		return Observation{}, invalidTerminal("authority head")
	}
	if recoveryID != nil {
		if !validOpaqueID(*recoveryID) {
			return Observation{}, invalidTerminal("authority recovery identity")
		}
		observation.RecoveryID = *recoveryID
	}
	if (observation.Mode != "active" && observation.Mode != "recovering") ||
		(observation.Mode == "recovering" && recoveryID == nil) {
		return Observation{}, invalidTerminal("authority mode")
	}
	return observation, nil
}

func (authority *Authority) verifySuffix(ctx context.Context, tx pgx.Tx, terminal Terminal, hash [32]byte, observation Observation) error {
	if terminal.Sequence > observation.HeadSequence {
		return invalidTerminal("terminal beyond trusted head")
	}
	sequence := terminal.Sequence
	for sequence < observation.HeadSequence {
		rows, err := tx.Query(ctx, `select `+terminalSelectColumns+`
			from public.source_retirement_terminals where sequence > $1 and sequence <= $2
			order by sequence limit $3`, int64(sequence), int64(observation.HeadSequence), terminalBatchSize)
		if err != nil {
			return err
		}
		before := sequence
		for rows.Next() {
			next, nextHash, err := scanAuthorityTerminal(rows, authority.expected)
			if err != nil {
				rows.Close()
				return err
			}
			if next.Sequence != sequence+1 || next.PreviousHash != hash || next.OriginGeneration > observation.Generation {
				rows.Close()
				return invalidTerminal("terminal chain link or origin generation")
			}
			sequence, hash = next.Sequence, nextHash
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if sequence == before {
			return invalidTerminal("terminal chain gap")
		}
	}
	if hash != observation.HeadHash {
		return invalidTerminal("terminal trusted head hash")
	}
	return nil
}

type authorityRow interface {
	Scan(...any) error
}

func scanAuthorityPrepare(row authorityRow) (Prepare, error) {
	var prepared Prepare
	var sequence, generation int64
	var fingerprint []byte
	if err := row.Scan(&prepared.ID, &sequence, &prepared.Source.DeploymentID,
		&prepared.Source.ProjectID, &prepared.Source.Kind, &prepared.Source.ID,
		&generation, &fingerprint); err != nil {
		return Prepare{}, authorityScanError(err)
	}
	if sequence <= 0 || generation <= 0 || len(fingerprint) != 32 || !validOpaqueID(prepared.ID) {
		return Prepare{}, invalidTerminal("prepare projection")
	}
	if err := validateAuthoritySource(prepared.Source); err != nil {
		return Prepare{}, err
	}
	prepared.Sequence = uint64(sequence)
	prepared.OriginGeneration = uint64(generation)
	copy(prepared.Fingerprint[:], fingerprint)
	return prepared, nil
}

func scanAuthorityTerminal(row authorityRow, expected DomainIdentity) (Terminal, [32]byte, error) {
	terminal := Terminal{Domain: expected}
	var generation, sequence int64
	var barrier, lastLive, floor, previous, canonical, hash []byte
	if err := row.Scan(&terminal.RetirementID, &terminal.Source.DeploymentID,
		&terminal.Source.ProjectID, &terminal.Source.Kind, &terminal.Source.ID,
		&generation, &barrier, &lastLive, &floor, &sequence, &previous, &canonical, &hash); err != nil {
		return Terminal{}, [32]byte{}, authorityScanError(err)
	}
	if generation <= 0 || sequence <= 0 || len(barrier) != 32 || len(previous) != 32 || len(hash) != 32 ||
		len(lastLive) == 0 || len(lastLive) > maxScopeBytes || len(floor) == 0 || len(floor) > maxScopeBytes {
		return Terminal{}, [32]byte{}, invalidTerminal("terminal projection")
	}
	terminal.OriginGeneration = uint64(generation)
	terminal.Sequence = uint64(sequence)
	copy(terminal.BarrierDigest[:], barrier)
	copy(terminal.PreviousHash[:], previous)
	var entryHash [32]byte
	copy(entryHash[:], hash)
	var err error
	terminal.LastLive, err = parseTerminalScope(lastLive)
	if err != nil {
		return Terminal{}, [32]byte{}, err
	}
	terminal.FinalFloor, err = parseTerminalScope(floor)
	if err != nil {
		return Terminal{}, [32]byte{}, err
	}
	if err := VerifyTerminal(canonical, terminal, entryHash); err != nil {
		return Terminal{}, [32]byte{}, err
	}
	return terminal, entryHash, nil
}

func sameAuthorityScope(left, right recordauth.VisibilityScope) bool {
	return left.Version == right.Version && left.Kind == right.Kind && left.ProjectID == right.ProjectID &&
		left.PolicyVersion == right.PolicyVersion && left.PolicyRevision == right.PolicyRevision &&
		left.CanonicalHash == right.CanonicalHash && slices.Equal(left.AllowedRoles, right.AllowedRoles) &&
		slices.Equal(left.AllowedGroupIDs, right.AllowedGroupIDs)
}

func validateAuthorityDomain(domain DomainIdentity) error {
	if !validIdentity(domain.ID, "rd-") || domain.DatabaseOID == 0 || domain.Epoch == 0 {
		return invalidTerminal("configured authority domain")
	}
	identifier := domain.SystemIdentifier
	if len(identifier) == 0 || len(identifier) > 20 || identifier[0] < '1' || identifier[0] > '9' {
		return invalidTerminal("configured authority system identifier")
	}
	for index := range identifier {
		if identifier[index] < '0' || identifier[index] > '9' {
			return invalidTerminal("configured authority system identifier")
		}
	}
	if _, err := strconv.ParseUint(identifier, 10, 64); err != nil {
		return invalidTerminal("configured authority system identifier")
	}
	name := domain.DatabaseName
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return invalidTerminal("configured authority database name")
	}
	for index := range name {
		if !lowerAlphaNumeric(name[index]) && name[index] != '_' {
			return invalidTerminal("configured authority database name")
		}
	}
	return nil
}

func validateAuthoritySource(key SourceKey) error {
	if !validIdentity(key.DeploymentID, "dp-") || key.ProjectID != recordauth.ProjectIDDefault {
		return invalidTerminal("source identity")
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
		return invalidTerminal("source kind")
	}
	if len(key.ID) != len(prefix)+16 || !strings.HasPrefix(key.ID, prefix) {
		return invalidTerminal("source identity")
	}
	for index := len(prefix); index < len(key.ID); index++ {
		if (key.ID[index] < '0' || key.ID[index] > '9') && (key.ID[index] < 'a' || key.ID[index] > 'f') {
			return invalidTerminal("source identity")
		}
	}
	return nil
}

func (authority *Authority) checkContext(ctx context.Context) error {
	if authority == nil || authority.pool == nil || ctx == nil {
		return errors.New("source authority and context are required")
	}
	return ctx.Err()
}

// A malformed stored projection is invalid evidence, while PostgreSQL and
// context failures retain their original identity for errors.Is/errors.As.
func authorityScanError(err error) error {
	var scanError pgx.ScanArgError
	if errors.As(err, &scanError) {
		return fmt.Errorf("%w: stored projection: %w", ErrInvalidTerminal, err)
	}
	return err
}

// Connection errors can contain connection parameters. Keep Error() safe for
// logs while preserving the complete typed cause without printing DSNs/secrets.
type authorityError struct {
	operation string
	cause     error
}

func (err *authorityError) Error() string {
	return "source authority " + err.operation + " failed"
}

func (err *authorityError) Unwrap() error {
	return err.cause
}

func authorityFailure(operation string, cause error) error {
	return &authorityError{operation: operation, cause: cause}
}
