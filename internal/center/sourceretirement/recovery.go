package sourceretirement

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5"
)

// RecoveryStatus is the durable state of one exact recovery identity. Fields
// established by sealing and credential staging remain nil until those steps.
type RecoveryStatus struct {
	ID                       string
	Generation               uint64
	EndpointDigest           [32]byte
	Phase                    string
	CoverageSeal             *[32]byte
	PrepareHighWater         *uint64
	PrepareCount             *uint64
	PrepareDigest            *[32]byte
	TerminalSequence         *uint64
	TerminalHash             *[32]byte
	GuardDigest              *[32]byte
	CredentialManifestDigest *[32]byte
	NamespaceHandoffDigest   *[32]byte
}

// RecoveryUnknown records an unresolved prepare without inventing a floor.
type RecoveryUnknown struct {
	RetirementID        string
	RecoveryID          string
	InstalledGeneration uint64
}

// LegacyBlocker identifies W1 source evidence that cannot be treated as a W2
// terminal. Unlocatable also requires a wider project or domain admission stop.
type LegacyBlocker struct {
	Source      SourceKey
	Unlocatable bool
}

// RecoveryInventory is a complete, verified view from one W2 snapshot.
type RecoveryInventory struct {
	State          Observation
	Prepares       []Prepare
	Terminals      []Terminal
	Unknowns       []RecoveryUnknown
	LegacyBlockers []LegacyBlocker
}

const recoveryColumns = `recovery_id, generation, endpoint_digest, phase,
	coverage_seal, prepare_high_water, prepare_count, prepare_digest,
	terminal_sequence, terminal_hash, guard_digest, credential_manifest_digest,
	namespace_handoff_digest`

var recoveryPurposes = [...]string{"runtime", "admin", "worker", "migrator", "producer"}

// recoveryMutation authenticates the physical domain before invoking a
// SECURITY DEFINER function. A commit error is uncertain and is never retried.
func (authority *Authority) recoveryMutation(ctx context.Context, operation string, apply func(pgx.Tx) error) error {
	if err := authority.checkContext(ctx); err != nil {
		return err
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return authorityFailure("begin "+operation, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := authority.snapshot(ctx, tx); err != nil {
		return authorityFailure("read "+operation+" identity", err)
	}
	if err := apply(tx); err != nil {
		return authorityFailure(operation, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return authorityFailure("commit "+operation+" (outcome uncertain)", err)
	}
	return nil
}

// BindPrincipal binds an existing direct-login role to its immutable purpose.
func (authority *Authority) BindPrincipal(ctx context.Context, principal, purpose string, generation uint64, recoveryID string) error {
	if generation == 0 || generation > math.MaxInt64 || principal == "" ||
		(purpose != "reader" && purpose != "producer" && purpose != "recovery") ||
		(purpose == "recovery" && !validOpaqueID(recoveryID)) ||
		(purpose != "recovery" && recoveryID != "") {
		return invalidTerminal("principal binding")
	}
	var rid any
	if recoveryID != "" {
		rid = recoveryID
	}
	return authority.recoveryMutation(ctx, "bind principal", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `select public.source_authority_bind($1::name, $2, $3, $4)`, principal, purpose, int64(generation), rid)
		return err
	})
}

// EnrollCredentials records the initial five generation-one commitments and secret equality witnesses.
func (authority *Authority) EnrollCredentials(ctx context.Context, principals [5]string, commitments, fingerprints [5][32]byte) error {
	return authority.recoveryMutation(ctx, "enroll credentials", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `select public.source_enrollment_credentials($1::text[], $2::text[], $3::bytea[], $4::bytea[])`,
			recoveryPurposes[:], principals[:], recoveryCommitments(commitments), recoveryCommitments(fingerprints))
		return err
	})
}

// BeginRecovery transitions the witness to a new generation for the exact RID.
func (authority *Authority) BeginRecovery(ctx context.Context, recoveryID string, endpointDigest [32]byte, recoveryPrincipal string) (RecoveryStatus, error) {
	if !validOpaqueID(recoveryID) || recoveryPrincipal == "" {
		return RecoveryStatus{}, invalidTerminal("recovery identity")
	}
	var status RecoveryStatus
	err := authority.recoveryMutation(ctx, "begin recovery", func(tx pgx.Tx) error {
		var err error
		status, err = scanRecoveryStatus(tx.QueryRow(ctx, `select `+recoveryColumns+` from public.source_recovery_begin($1, $2, $3::name)`, recoveryID, endpointDigest[:], recoveryPrincipal), recoveryID)
		if err == nil && status.EndpointDigest != endpointDigest {
			return invalidTerminal("recovery endpoint projection")
		}
		return err
	})
	if err != nil {
		return RecoveryStatus{}, err
	}
	return status, nil
}

// MarkCoverageUnknown preserves an unresolved prepare as durable unknown.
func (authority *Authority) MarkCoverageUnknown(ctx context.Context, recoveryID, retirementID string) error {
	if !validOpaqueID(recoveryID) || !validOpaqueID(retirementID) {
		return invalidTerminal("recovery or retirement identity")
	}
	return authority.recoveryMutation(ctx, "mark coverage unknown", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `select public.source_recovery_unknown($1, $2)`, recoveryID, retirementID)
		return err
	})
}

// SealCoverage seals the complete witness inventory and namespace handoff.
func (authority *Authority) SealCoverage(ctx context.Context, recoveryID string, namespaceDigest [32]byte) (RecoveryStatus, error) {
	if !validOpaqueID(recoveryID) {
		return RecoveryStatus{}, invalidTerminal("recovery identity")
	}
	var status RecoveryStatus
	err := authority.recoveryMutation(ctx, "seal coverage", func(tx pgx.Tx) error {
		var err error
		status, err = scanRecoveryStatus(tx.QueryRow(ctx, `select `+recoveryColumns+` from public.source_recovery_seal($1, $2)`, recoveryID, namespaceDigest[:]), recoveryID)
		if err == nil && (status.NamespaceHandoffDigest == nil || *status.NamespaceHandoffDigest != namespaceDigest) {
			return invalidTerminal("namespace handoff projection")
		}
		return err
	})
	if err != nil {
		return RecoveryStatus{}, err
	}
	return status, nil
}

// StageCredentials commits credential bindings, secret equality witnesses, and the published-manifest digest.
func (authority *Authority) StageCredentials(ctx context.Context, recoveryID string, principals [5]string, commitments, fingerprints [5][32]byte, manifest [32]byte) error {
	if !validOpaqueID(recoveryID) {
		return invalidTerminal("recovery identity")
	}
	return authority.recoveryMutation(ctx, "stage credentials", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `select public.source_recovery_credentials($1, $2::text[], $3::text[], $4::bytea[], $5::bytea[], $6)`,
			recoveryID, recoveryPurposes[:], principals[:], recoveryCommitments(commitments), recoveryCommitments(fingerprints), manifest[:])
		return err
	})
}

// AdvanceRecovery applies one SQL-enforced exact handoff transition.
func (authority *Authority) AdvanceRecovery(ctx context.Context, recoveryID, expectedPhase, nextPhase string, seal, manifest, evidence [32]byte) (RecoveryStatus, error) {
	if !validOpaqueID(recoveryID) {
		return RecoveryStatus{}, invalidTerminal("recovery identity")
	}
	var status RecoveryStatus
	err := authority.recoveryMutation(ctx, "advance recovery", func(tx pgx.Tx) error {
		var err error
		status, err = scanRecoveryStatus(tx.QueryRow(ctx, `select `+recoveryColumns+` from public.source_recovery_advance($1, $2, $3, $4, $5, $6)`,
			recoveryID, expectedPhase, nextPhase, seal[:], manifest[:], evidence[:]), recoveryID)
		if err == nil && (status.CoverageSeal == nil || *status.CoverageSeal != seal ||
			status.CredentialManifestDigest == nil || *status.CredentialManifestDigest != manifest ||
			recoveryPhaseIndex(status.Phase) < recoveryPhaseIndex(nextPhase) || recoveryPhaseIndex(nextPhase) < 0) {
			return invalidTerminal("recovery transition projection")
		}
		return err
	})
	if err != nil {
		return RecoveryStatus{}, err
	}
	return status, nil
}

// ReadRecovery reads the exact RID, never the latest recovery.
func (authority *Authority) ReadRecovery(ctx context.Context, recoveryID string) (RecoveryStatus, error) {
	if err := authority.checkContext(ctx); err != nil {
		return RecoveryStatus{}, err
	}
	if !validOpaqueID(recoveryID) {
		return RecoveryStatus{}, invalidTerminal("recovery identity")
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return RecoveryStatus{}, authorityFailure("begin read recovery", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := authority.snapshot(ctx, tx); err != nil {
		return RecoveryStatus{}, authorityFailure("read recovery identity", err)
	}
	status, err := scanRecoveryStatus(tx.QueryRow(ctx, `select `+recoveryColumns+` from public.source_authority_recoveries where recovery_id = $1`, recoveryID), recoveryID)
	if err != nil {
		return RecoveryStatus{}, authorityFailure("read recovery", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RecoveryStatus{}, authorityFailure("commit read recovery", err)
	}
	return status, nil
}

func recoveryCommitments(commitments [5][32]byte) [][]byte {
	values := make([][]byte, len(commitments))
	for i := range commitments {
		values[i] = commitments[i][:]
	}
	return values
}

func recoveryPhaseIndex(phase string) int {
	switch phase {
	case "recovering":
		return 0
	case "coverage_sealed":
		return 1
	case "credentials_staged":
		return 2
	case "witness_activated":
		return 3
	case "app_active_login_closed":
		return 4
	case "credentials_published":
		return 5
	case "logins_open":
		return 6
	case "completed":
		return 7
	default:
		return -1
	}
}

func scanRecoveryStatus(row authorityRow, expectedID string) (RecoveryStatus, error) {
	var status RecoveryStatus
	var generation int64
	var endpoint, seal, pd, th, gd, manifest, namespace []byte
	var highWater, count, sequence *int64
	if err := row.Scan(&status.ID, &generation, &endpoint, &status.Phase, &seal,
		&highWater, &count, &pd, &sequence, &th, &gd, &manifest, &namespace); err != nil {
		return RecoveryStatus{}, authorityScanError(err)
	}
	phase := recoveryPhaseIndex(status.Phase)
	if status.ID != expectedID || !validOpaqueID(status.ID) || generation <= 1 || len(endpoint) != 32 || phase < 0 {
		return RecoveryStatus{}, invalidTerminal("recovery status projection")
	}
	status.Generation = uint64(generation)
	copy(status.EndpointDigest[:], endpoint)
	var err error
	if status.CoverageSeal, err = recoveryDigest(seal); err != nil {
		return RecoveryStatus{}, err
	}
	if status.PrepareDigest, err = recoveryDigest(pd); err != nil {
		return RecoveryStatus{}, err
	}
	if status.TerminalHash, err = recoveryDigest(th); err != nil {
		return RecoveryStatus{}, err
	}
	if status.GuardDigest, err = recoveryDigest(gd); err != nil {
		return RecoveryStatus{}, err
	}
	if status.CredentialManifestDigest, err = recoveryDigest(manifest); err != nil {
		return RecoveryStatus{}, err
	}
	if status.NamespaceHandoffDigest, err = recoveryDigest(namespace); err != nil {
		return RecoveryStatus{}, err
	}
	if status.PrepareHighWater, err = recoveryUint(highWater); err != nil {
		return RecoveryStatus{}, err
	}
	if status.PrepareCount, err = recoveryUint(count); err != nil {
		return RecoveryStatus{}, err
	}
	if status.TerminalSequence, err = recoveryUint(sequence); err != nil {
		return RecoveryStatus{}, err
	}
	sealed := phase >= 1
	if (status.CoverageSeal != nil) != sealed || (status.PrepareHighWater != nil) != sealed ||
		(status.PrepareCount != nil) != sealed || (status.PrepareDigest != nil) != sealed ||
		(status.TerminalSequence != nil) != sealed || (status.TerminalHash != nil) != sealed ||
		(status.GuardDigest != nil) != sealed || (status.NamespaceHandoffDigest != nil) != sealed ||
		(status.CredentialManifestDigest != nil) != (phase >= 2) {
		return RecoveryStatus{}, invalidTerminal("recovery nullable projection")
	}
	if sealed && ((*status.PrepareCount == 0) != (*status.PrepareHighWater == 0) ||
		*status.PrepareCount > *status.PrepareHighWater ||
		(*status.TerminalSequence == 0) != (*status.TerminalHash == ([32]byte{}))) {
		return RecoveryStatus{}, invalidTerminal("recovery seal projection")
	}
	return status, nil
}

func recoveryDigest(raw []byte) (*[32]byte, error) {
	if raw == nil {
		return nil, nil
	}
	if len(raw) != 32 {
		return nil, invalidTerminal("recovery digest projection")
	}
	var value [32]byte
	copy(value[:], raw)
	return &value, nil
}

func recoveryUint(raw *int64) (*uint64, error) {
	if raw == nil {
		return nil, nil
	}
	if *raw < 0 {
		return nil, invalidTerminal("recovery integer projection")
	}
	value := uint64(*raw)
	return &value, nil
}

// Inventory reads every prepare, terminal, unknown, and legacy source blocker
// under one authenticated repeatable-read snapshot and verifies the entire W2
// chain, including its genesis and trusted head.
func (authority *Authority) Inventory(ctx context.Context) (RecoveryInventory, error) {
	if err := authority.checkContext(ctx); err != nil {
		return RecoveryInventory{}, err
	}
	tx, err := authority.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return RecoveryInventory{}, authorityFailure("begin inventory", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := authority.snapshot(ctx, tx)
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory identity", err)
	}
	inventory := RecoveryInventory{State: state}
	prepares := make(map[string]Prepare)
	rows, err := tx.Query(ctx, `select `+prepareSelectColumns+` from public.source_retirement_prepares order by prepare_sequence`)
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory prepares", err)
	}
	var previousPrepare uint64
	for rows.Next() {
		prepared, scanErr := scanAuthorityPrepare(rows)
		if scanErr != nil {
			rows.Close()
			return RecoveryInventory{}, authorityFailure("scan inventory prepare", scanErr)
		}
		if prepared.Sequence <= previousPrepare || prepared.OriginGeneration > state.Generation || prepared.Fingerprint == ([32]byte{}) {
			rows.Close()
			return RecoveryInventory{}, invalidTerminal("inventory prepare projection")
		}
		if _, exists := prepares[prepared.ID]; exists {
			rows.Close()
			return RecoveryInventory{}, invalidTerminal("duplicate inventory prepare")
		}
		previousPrepare = prepared.Sequence
		prepares[prepared.ID] = prepared
		inventory.Prepares = append(inventory.Prepares, prepared)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory prepares", err)
	}

	rows, err = tx.Query(ctx, `select `+terminalSelectColumns+` from public.source_retirement_terminals order by sequence`)
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory terminals", err)
	}
	var sequence uint64
	var hash [32]byte
	terminalSources := make(map[SourceKey]struct{})
	for rows.Next() {
		terminal, nextHash, scanErr := scanAuthorityTerminal(rows, authority.expected)
		if scanErr != nil {
			rows.Close()
			return RecoveryInventory{}, authorityFailure("scan inventory terminal", scanErr)
		}
		prepared, exists := prepares[terminal.RetirementID]
		if !exists || validateAuthoritySource(terminal.Source) != nil || prepared.Source != terminal.Source ||
			prepared.OriginGeneration != terminal.OriginGeneration || terminal.OriginGeneration > state.Generation ||
			terminal.Sequence != sequence+1 || terminal.PreviousHash != hash || terminal.Sequence > state.HeadSequence {
			rows.Close()
			return RecoveryInventory{}, invalidTerminal("inventory terminal chain or prepare")
		}
		if _, exists := terminalSources[terminal.Source]; exists {
			rows.Close()
			return RecoveryInventory{}, invalidTerminal("duplicate inventory terminal source")
		}
		terminalSources[terminal.Source] = struct{}{}
		sequence, hash = terminal.Sequence, nextHash
		inventory.Terminals = append(inventory.Terminals, terminal)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory terminals", err)
	}
	if sequence != state.HeadSequence || hash != state.HeadHash {
		return RecoveryInventory{}, invalidTerminal("inventory trusted head")
	}

	rows, err = tx.Query(ctx, `select retirement_id, recovery_id, installed_generation from public.source_retirement_unknown order by retirement_id collate "C"`)
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory unknowns", err)
	}
	var previousID string
	for rows.Next() {
		var unknown RecoveryUnknown
		var generation int64
		if err := rows.Scan(&unknown.RetirementID, &unknown.RecoveryID, &generation); err != nil {
			rows.Close()
			return RecoveryInventory{}, authorityFailure("scan inventory unknown", authorityScanError(err))
		}
		prepared, exists := prepares[unknown.RetirementID]
		if !exists || !validOpaqueID(unknown.RecoveryID) || unknown.RetirementID <= previousID ||
			generation <= 0 || uint64(generation) > state.Generation || uint64(generation) < prepared.OriginGeneration {
			rows.Close()
			return RecoveryInventory{}, invalidTerminal("inventory unknown projection")
		}
		unknown.InstalledGeneration = uint64(generation)
		previousID = unknown.RetirementID
		inventory.Unknowns = append(inventory.Unknowns, unknown)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory unknowns", err)
	}

	// Record-route/record-kind entries belong exclusively to the W1 record
	// ledger. Any source-kind entry or other route must not disappear merely
	// because its source identity is malformed or unsupported.
	rows, err = tx.Query(ctx, `select deployment_id, project_id, object_kind, object_id
		from public.deletion_witness_entries
		where route <> 'record_permanent_delete' or object_kind <> 'record' order by sequence`)
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory legacy entries", err)
	}
	for rows.Next() {
		var blocker LegacyBlocker
		if err := rows.Scan(&blocker.Source.DeploymentID, &blocker.Source.ProjectID, &blocker.Source.Kind, &blocker.Source.ID); err != nil {
			rows.Close()
			return RecoveryInventory{}, authorityFailure("scan inventory legacy entry", authorityScanError(err))
		}
		blocker.Unlocatable = validateAuthoritySource(blocker.Source) != nil
		inventory.LegacyBlockers = append(inventory.LegacyBlockers, blocker)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return RecoveryInventory{}, authorityFailure("read inventory legacy entries", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RecoveryInventory{}, authorityFailure("commit inventory", err)
	}
	return inventory, nil
}
