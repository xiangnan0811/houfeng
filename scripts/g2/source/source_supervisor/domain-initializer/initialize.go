//go:build linux

package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/secure/precis"

	ledgerMigrations "houfeng/db/deletionledger/migrations"
	controlMigrations "houfeng/db/recoverycontrol/migrations"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recorddeletion"
)

const (
	w1ReaderRole            = "g2_w1_reader"
	w1WriterRole            = "g2_w1_writer"
	w2OwnerRole             = "g2_w2_owner"
	w2RecordReaderRole      = "g2_w2_record_reader"
	w2RecordWriterRole      = "g2_w2_record_writer"
	w2SourceReaderRole      = "g2_w2_source_reader"
	w2SourceProducerRole    = "g2_w2_source_producer"
	w2SourceProvisionerRole = "g2_w2_source_provisioner"
)

func initialize(ctx context.Context, input privateInput) (initializeOutput, error) {
	var result initializeOutput
	w1, err := openDomain(ctx, input.w1)
	if err != nil {
		return result, stageError("w1_connection_unavailable")
	}
	defer w1.pool.Close()
	w2, err := openDomain(ctx, input.w2)
	if err != nil {
		return result, stageError("w2_connection_unavailable")
	}
	defer w2.pool.Close()
	control, err := openDomain(ctx, input.control)
	if err != nil {
		return result, stageError("control_connection_unavailable")
	}
	defer control.pool.Close()

	// Nothing is created until all three independent clusters pass both the
	// fresh catalog census and the physical-ID separation check.
	w1Physical, err := w1.preflight(ctx)
	if err != nil {
		return result, stageError("w1_freshness_unavailable")
	}
	w2Physical, err := w2.preflight(ctx)
	if err != nil {
		return result, stageError("w2_freshness_unavailable")
	}
	controlPhysical, err := control.preflight(ctx)
	if err != nil {
		return result, stageError("control_freshness_unavailable")
	}
	if w1Physical.systemID == w2Physical.systemID || w1Physical.systemID == controlPhysical.systemID ||
		w2Physical.systemID == controlPhysical.systemID ||
		w1.verifyPhysical(ctx) != nil || w2.verifyPhysical(ctx) != nil || control.verifyPhysical(ctx) != nil {
		return result, stageError("domain_separation_unavailable")
	}

	c := input.credentials
	w1Roles := platformmigrate.RecordLedgerScopeRoles{
		Reader: platformmigrate.SourceRoleCredential{Principal: w1ReaderRole, Password: c.w1Reader},
		Writer: platformmigrate.SourceRoleCredential{Principal: w1WriterRole, Password: c.w1Writer},
	}
	w2Roles := platformmigrate.RecordLedgerScopeRoles{
		Reader: platformmigrate.SourceRoleCredential{Principal: w2RecordReaderRole, Password: c.w2RecordReader},
		Writer: platformmigrate.SourceRoleCredential{Principal: w2RecordWriterRole, Password: c.w2RecordWriter},
	}
	sourceRoles := platformmigrate.SourceAuthorityRoles{
		Owner: w2OwnerRole, Reader: w2SourceReaderRole,
		Producer: w2SourceProducerRole, Provisioner: w2SourceProvisionerRole,
	}
	if w1Roles.Validate() != nil || w2Roles.Validate() != nil || sourceRoles.Validate() != nil {
		return result, stageError("roles_unavailable")
	}

	if err := platformmigrate.Apply(ctx, w1.pool, ledgerMigrations.FS); err != nil {
		return result, stageError("w1_migration_unavailable")
	}
	w1Identity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, w1.pool, platformmigrate.DomainKindDeletionLedger)
	if err != nil {
		return result, stageError("w1_identity_unavailable")
	}
	if err := createRoles(ctx, w1, false, []platformmigrate.SourceRoleCredential{w1Roles.Reader, w1Roles.Writer}); err != nil {
		return result, stageError("w1_roles_unavailable")
	}
	w1ScopeIdentity, err := platformmigrate.ProvisionRecordLedgerScope(ctx, w1.pool, platformmigrate.DomainKindDeletionLedger, w1Roles)
	if err != nil || w1ScopeIdentity != w1Identity {
		return result, stageError("w1_scope_unavailable")
	}

	w2Credentials := []platformmigrate.SourceRoleCredential{
		{Principal: w2SourceReaderRole, Password: c.w2SourceReader},
		{Principal: w2SourceProducerRole, Password: c.w2SourceProducer},
		{Principal: w2SourceProvisionerRole, Password: c.w2SourceProvisioner},
		w2Roles.Reader, w2Roles.Writer,
	}
	if err := createRoles(ctx, w2, true, w2Credentials); err != nil {
		return result, stageError("w2_roles_unavailable")
	}
	w2Identity, err := platformmigrate.ProvisionSourceAuthority(ctx, w2.pool, sourceRoles)
	if err != nil {
		return result, stageError("w2_authority_unavailable")
	}
	w2ScopeIdentity, err := platformmigrate.ProvisionRecordLedgerScope(ctx, w2.pool, platformmigrate.DomainKindDeletionWitness, w2Roles)
	if err != nil || w2ScopeIdentity != w2Identity {
		return result, stageError("w2_scope_unavailable")
	}

	if err := platformmigrate.Apply(ctx, control.pool, controlMigrations.FS); err != nil {
		return result, stageError("control_migration_unavailable")
	}
	controlIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, control.pool, platformmigrate.DomainKindRecoveryControl)
	if err != nil {
		return result, stageError("control_identity_unavailable")
	}

	w1Actual, err := w1.readDomainIdentity(ctx, platformmigrate.DomainKindDeletionLedger)
	if err != nil || w1Actual != w1Identity {
		return result, stageError("w1_readback_unavailable")
	}
	w2Actual, err := w2.readDomainIdentity(ctx, platformmigrate.DomainKindDeletionWitness)
	if err != nil || w2Actual != w2Identity {
		return result, stageError("w2_readback_unavailable")
	}
	controlActual, err := control.readDomainIdentity(ctx, platformmigrate.DomainKindRecoveryControl)
	if err != nil || controlActual != controlIdentity {
		return result, stageError("control_readback_unavailable")
	}
	if checkW1Genesis(ctx, w1) != nil || checkW2Genesis(ctx, w2) != nil || checkControlGenesis(ctx, control) != nil {
		return result, stageError("genesis_unavailable")
	}
	if err := provisionSourceMaintenance(w2Actual, input); err != nil {
		return result, stageError("source_maintenance_unavailable")
	}
	result.Domains.W1, err = ledgerIdentity(w1Actual)
	if err != nil {
		return initializeOutput{}, stageError("w1_readback_unavailable")
	}
	result.Domains.W2, err = ledgerIdentity(w2Actual)
	if err != nil {
		return initializeOutput{}, stageError("w2_readback_unavailable")
	}
	result.Domains.Control = controlActual
	result.Version = 1
	return result, nil
}

func ledgerIdentity(identity platformmigrate.DomainIdentity) (recorddeletion.LedgerPhysicalIdentity, error) {
	if identity.IdentityEpoch != 1 || identity.Mode != "postgres_system" {
		return recorddeletion.LedgerPhysicalIdentity{}, errDomain
	}
	converted := recorddeletion.LedgerPhysicalIdentity{
		ID: identity.ID, Kind: string(identity.Kind), SystemIdentifier: identity.PostgresSystemIdentifier,
		DatabaseOID: identity.DatabaseOID, DatabaseName: identity.DatabaseName, Epoch: uint64(identity.IdentityEpoch),
	}
	if converted.Validate() != nil {
		return recorddeletion.LedgerPhysicalIdentity{}, errDomain
	}
	return converted, nil
}

// This uses the same SCRAM construction and password preparation as the
// existing ledger role management code. Only the salted verifier, never the
// supplied password, appears in a CREATE ROLE statement or database log.
func passwordVerifier(password string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", errDomain
	}
	prepared, err := precis.OpaqueString.String(password)
	if err != nil {
		prepared = password
	}
	const iterations = 4096
	salted := pbkdf2.Key([]byte(prepared), salt[:], iterations, sha256.Size, sha256.New)
	defer clear(salted)
	client := hmac.New(sha256.New, salted)
	_, _ = client.Write([]byte("Client Key"))
	stored := sha256.Sum256(client.Sum(nil))
	server := hmac.New(sha256.New, salted)
	_, _ = server.Write([]byte("Server Key"))
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations,
		base64.StdEncoding.EncodeToString(salt[:]), base64.StdEncoding.EncodeToString(stored[:]),
		base64.StdEncoding.EncodeToString(server.Sum(nil))), nil
}

// The only caller supplies the fixed role manifests above. All roles for one
// cluster are created atomically and the actual SCRAM verifiers, attributes,
// and absence of memberships are checked by the existing provisioning API.
func createRoles(ctx context.Context, db *domainDB, owner bool, credentials []platformmigrate.SourceRoleCredential) error {
	if err := db.verifyPhysical(ctx); err != nil {
		return errDomain
	}
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return errDomain
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if owner {
		if _, err := tx.Exec(ctx, `create role g2_w2_owner nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
			return errDomain
		}
	}
	for _, credential := range credentials {
		verifier, err := passwordVerifier(credential.Password)
		if err != nil {
			return errDomain
		}
		// The role identifiers are fixed internal constants. A verifier contains
		// only the fixed SCRAM prefix and base64/digit punctuation, never quotes.
		statement := "create role " + pgx.Identifier{credential.Principal}.Sanitize() +
			" login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '" + verifier + "'"
		if _, err := tx.Exec(ctx, statement); err != nil {
			return errDomain
		}
	}
	if err := platformmigrate.VerifySourceRoleCredentials(ctx, tx, credentials); err != nil {
		return errDomain
	}
	if err := tx.Commit(ctx); err != nil {
		return errDomain
	}
	return nil
}
