// Package sourceenrollment implements trusted, initial source authority enrollment.
// The caller must hold the outer Compose maintenance guard for the whole call.
package sourceenrollment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store/migrate"
)

// AdmissionGate is a trusted bootstrap deployment-membership check. It must
// validate the bound deployment in the supplied transaction without doing I/O
// outside that transaction. It is not a substitute for the maintenance guard.
type AdmissionGate interface {
	Admit(context.Context, pgx.Tx) error
}

type Config struct {
	DeploymentID  string
	Witness       sourceretirement.DomainIdentity
	Roles         platformmigrate.AppRoleSetV2
	AuthorityRole string
	Passwords     platformmigrate.ComposeRolePasswords
	Producer      platformmigrate.SourceRoleCredential
}

// Enroll performs only generation-one enrollment or exact explicit resume.
// Pools and the maintenance guard are owned by the caller. An uncertain commit
// is returned as an error and never causes activation, retry, or compensation.
func Enroll(ctx context.Context, appBootstrap, witnessBootstrap *pgxpool.Pool, witnessProvisioner *sourceretirement.Authority, gate AdmissionGate, config Config) (sourceretirement.ApplicationEnrollment, error) {
	var empty sourceretirement.ApplicationEnrollment
	if appBootstrap == nil || witnessBootstrap == nil || witnessProvisioner == nil || gate == nil || config.Roles.Validate() != nil || !canonicalID(config.DeploymentID, "dp-") || !canonicalID(config.Witness.ID, "rd-") || config.Witness.Epoch != 1 {
		return empty, fmt.Errorf("invalid source enrollment configuration")
	}
	credentials := []platformmigrate.SourceRoleCredential{
		{Principal: config.Roles.CenterRuntime, Password: config.Passwords.Runtime},
		{Principal: config.Roles.PlatformAdmin, Password: config.Passwords.PlatformAdmin},
		{Principal: config.Roles.DeletionWorker, Password: config.Passwords.DeletionWorker},
		{Principal: config.Roles.Migrator, Password: config.Passwords.Migrator},
		{Principal: config.AuthorityRole, Password: config.Passwords.Authority},
		config.Producer,
	}
	seen := make(map[string]bool, len(credentials))
	for _, credential := range credentials {
		if credential.Principal == "" || credential.Password == "" || uint64(len(credential.Password)) > uint64(^uint32(0)) || seen[credential.Principal] {
			return empty, fmt.Errorf("invalid source enrollment role configuration")
		}
		seen[credential.Principal] = true
	}

	app, err := readApplication(ctx, appBootstrap, gate, config, credentials[:5])
	if err != nil {
		return empty, err
	}
	if err := checkWitness(ctx, witnessBootstrap, config); err != nil {
		return empty, err
	}
	if app.SystemIdentifier == config.Witness.SystemIdentifier {
		return empty, fmt.Errorf("application and witness PostgreSQL systems must be independent")
	}
	if err := checkAuthority(ctx, witnessProvisioner, config.Witness); err != nil {
		return empty, err
	}

	purposes := [...]string{"runtime", "admin", "worker", "migrator", "authority", "producer"}
	var commitments [6][32]byte
	for i := range credentials {
		commitments[i] = CredentialCommitment(purposes[i], credentials[i].Principal, credentials[i].Password)
	}
	activation := activationDigest(config.DeploymentID, app, config.Witness, commitments)
	var witnessPrincipals [5]string
	var witnessCommitments [5][32]byte
	var witnessFingerprints [5][32]byte
	for i, source := range []int{0, 1, 2, 3, 5} {
		witnessPrincipals[i] = credentials[source].Principal
		witnessCommitments[i] = commitments[source]
		witnessFingerprints[i] = CredentialSecretFingerprint(credentials[source].Password)
	}
	if err := witnessProvisioner.EnrollApplication(ctx, config.DeploymentID, app); err != nil {
		return empty, fmt.Errorf("enroll witness application: %w", err)
	}
	if err := witnessProvisioner.EnrollCredentials(ctx, witnessPrincipals, witnessCommitments, witnessFingerprints); err != nil {
		return empty, fmt.Errorf("enroll witness credentials: %w", err)
	}
	enrollment, err := witnessProvisioner.ReadApplicationEnrollment(ctx, config.DeploymentID)
	if err != nil {
		return empty, fmt.Errorf("read witness enrollment: %w", err)
	}
	if err := checkEnrollment(enrollment, config.DeploymentID, app, activation, false); err != nil {
		return empty, err
	}
	if err := checkAuthority(ctx, witnessProvisioner, config.Witness); err != nil {
		return empty, err
	}

	if err := commitApplication(ctx, appBootstrap, gate, config, credentials[:5], commitments, app, enrollment.ActivationDigest != nil); err != nil {
		return empty, err
	}
	if err := witnessProvisioner.ActivateApplication(ctx, activation); err != nil {
		return empty, fmt.Errorf("activate witness application: %w", err)
	}
	enrollment, err = witnessProvisioner.ReadApplicationEnrollment(ctx, config.DeploymentID)
	if err != nil {
		return empty, fmt.Errorf("read activated witness enrollment: %w", err)
	}
	if err := checkEnrollment(enrollment, config.DeploymentID, app, activation, true); err != nil {
		return empty, err
	}
	if err := checkAuthority(ctx, witnessProvisioner, config.Witness); err != nil {
		return empty, err
	}
	return enrollment, nil
}

func canonicalID(value, prefix string) bool {
	if len(value) != len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	for _, c := range value[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// CredentialCommitment binds a verified plaintext credential to its purpose
// and principal. Callers must validate credentials before treating it as proof.
func CredentialCommitment(purpose, principal, password string) [32]byte {
	return hashFields("HOUFENG-SOURCE-CREDENTIAL-V2", []byte(purpose), []byte(principal), []byte(password))
}

// CredentialSecretFingerprint is an equality witness for trusted, verified
// high-entropy plaintext; it is not proof that a credential can authenticate.
func CredentialSecretFingerprint(password string) [32]byte {
	return hashFields("HOUFENG-SOURCE-CREDENTIAL-SECRET-V2", []byte(password))
}

// Every field, including the domain tag, has an unambiguous u32 length.
func hashFields(domain string, fields ...[]byte) [32]byte {
	h := sha256.New()
	write := func(value []byte) {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))
		_, _ = h.Write(length[:])
		_, _ = h.Write(value)
	}
	write([]byte(domain))
	for _, field := range fields {
		write(field)
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func activationDigest(deployment string, app, witness sourceretirement.DomainIdentity, commitments [6][32]byte) [32]byte {
	identity := func(d sourceretirement.DomainIdentity) [][]byte {
		var oid [4]byte
		var epoch [8]byte
		binary.BigEndian.PutUint32(oid[:], d.DatabaseOID)
		binary.BigEndian.PutUint64(epoch[:], d.Epoch)
		return [][]byte{[]byte(d.ID), []byte(d.SystemIdentifier), oid[:], []byte(d.DatabaseName), epoch[:]}
	}
	fields := [][]byte{[]byte(deployment)}
	fields = append(fields, identity(app)...)
	fields = append(fields, identity(witness)...)
	var generation [8]byte
	binary.BigEndian.PutUint64(generation[:], 1)
	fields = append(fields, generation[:])
	for i := range commitments {
		fields = append(fields, commitments[i][:])
	}
	return hashFields("HOUFENG-SOURCE-ACTIVATION-V2", fields...)
}

func checkAuthority(ctx context.Context, authority *sourceretirement.Authority, witness sourceretirement.DomainIdentity) error {
	state, err := authority.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("read witness authority: %w", err)
	}
	if state.Domain != witness || state.Generation != 1 || state.Mode != "active" || state.RecoveryID != "" || !state.HandoffCompleted {
		return fmt.Errorf("source witness authority identity or initial state mismatch")
	}
	return nil
}

func checkEnrollment(enrollment sourceretirement.ApplicationEnrollment, deployment string, app sourceretirement.DomainIdentity, digest [32]byte, activated bool) error {
	if enrollment.Generation != 1 || enrollment.DeploymentID != deployment || enrollment.Application != app ||
		(enrollment.ActivationDigest != nil && *enrollment.ActivationDigest != digest) || (activated && enrollment.ActivationDigest == nil) {
		return fmt.Errorf("source witness application enrollment mismatch")
	}
	return nil
}

func readApplication(ctx context.Context, pool *pgxpool.Pool, gate AdmissionGate, config Config, credentials []platformmigrate.SourceRoleCredential) (sourceretirement.DomainIdentity, error) {
	var empty sourceretirement.DomainIdentity
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return empty, fmt.Errorf("begin application enrollment observation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	identity, err := checkApplication(ctx, tx, gate, config, credentials)
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, fmt.Errorf("commit application enrollment observation: %w", err)
	}
	return identity, nil
}

func checkApplication(ctx context.Context, tx pgx.Tx, gate AdmissionGate, config Config, credentials []platformmigrate.SourceRoleCredential) (sourceretirement.DomainIdentity, error) {
	var empty sourceretirement.DomainIdentity
	physical, err := platformmigrate.ReadSourceApplicationIdentity(ctx, tx)
	if err != nil {
		return empty, fmt.Errorf("verify application identity: %w", err)
	}
	app := sourceretirement.DomainIdentity{ID: physical.ID, SystemIdentifier: physical.PostgresSystemIdentifier, DatabaseOID: physical.DatabaseOID, DatabaseName: physical.DatabaseName, Epoch: uint64(physical.IdentityEpoch)}
	if err := platformmigrate.VerifySourceRoleCredentials(ctx, tx, credentials); err != nil {
		return empty, fmt.Errorf("verify application credentials: %w", err)
	}
	if err := gate.Admit(ctx, tx); err != nil {
		return empty, fmt.Errorf("application deployment admission: %w", err)
	}
	var matches bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from public.deployment_contract_state where project_id='default' and deployment_id=$1 and activation_sequence=1 and active_profile in ('postgres_sync','s3_worm') and minimum_fence_contract_version > 0)`, config.DeploymentID).Scan(&matches); err != nil {
		return empty, fmt.Errorf("read application deployment: %w", err)
	}
	if !matches {
		return empty, fmt.Errorf("application deployment identity is not activated")
	}
	if err := verifyManifest(ctx, tx, config.Roles); err != nil {
		return empty, err
	}
	return app, nil
}

// Check the complete immutable chain and parse the current mandatory-worker
// privilege body in the same transaction as the other APP checks.
func verifyManifest(ctx context.Context, tx pgx.Tx, roles platformmigrate.AppRoleSetV2) error {
	var owner string
	if err := tx.QueryRow(ctx, `select pg_catalog.pg_get_userbyid(datdba) from pg_catalog.pg_database where datname=current_database()`).Scan(&owner); err != nil {
		return fmt.Errorf("read application database owner: %w", err)
	}
	if owner != roles.Migrator {
		return fmt.Errorf("application database owner does not match migrator")
	}
	rows, err := tx.Query(ctx, `select manifest_revision, migrator_catalog_role, previous_manifest_digest, canonical_migration_set, sorted_migration_set_digest, canonical_privilege_set, privilege_set_digest, manifest_digest from public.app_acl_manifest_revisions order by manifest_revision`)
	if err != nil {
		return fmt.Errorf("read application ACL manifest: %w", err)
	}
	var manifests []migrate.AppACLManifestPersistedV1
	for rows.Next() {
		var m migrate.AppACLManifestPersistedV1
		var revision int64
		var previous, migration, privilege, digest []byte
		if err := rows.Scan(&revision, &m.MigratorCatalogRole, &previous, &m.CanonicalMigrationSet, &migration, &m.CanonicalPrivilegeSet, &privilege, &digest); err != nil {
			rows.Close()
			return fmt.Errorf("scan application ACL manifest: %w", err)
		}
		if revision <= 0 || len(previous) != 32 || len(migration) != 32 || len(privilege) != 32 || len(digest) != 32 {
			rows.Close()
			return fmt.Errorf("invalid application ACL manifest projection")
		}
		m.ManifestRevision = uint64(revision)
		copy(m.PreviousManifestDigest[:], previous)
		copy(m.MigrationSetDigest[:], migration)
		copy(m.PrivilegeSetDigest[:], privilege)
		copy(m.ManifestDigest[:], digest)
		manifests = append(manifests, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("iterate application ACL manifest: %w", err)
	}
	var headRevision int64
	var headDigest []byte
	if err := tx.QueryRow(ctx, `select manifest_revision, manifest_digest from public.app_acl_manifest_head where singleton`).Scan(&headRevision, &headDigest); err != nil {
		return fmt.Errorf("read application ACL manifest head: %w", err)
	}
	if headRevision <= 0 || len(headDigest) != 32 {
		return fmt.Errorf("invalid application ACL manifest head")
	}
	head := migrate.AppACLManifestHeadV1{ManifestRevision: uint64(headRevision)}
	copy(head.ManifestDigest[:], headDigest)
	if err := migrate.ValidateAppACLManifestChainV1(manifests, head); err != nil {
		return fmt.Errorf("verify application ACL manifest chain: %w", err)
	}
	latest := manifests[len(manifests)-1]
	if latest.MigratorCatalogRole != roles.Migrator {
		return fmt.Errorf("application ACL migrator binding mismatch")
	}
	set, err := migrate.ParseCanonicalPrivilegeSetBodyV2(latest.CanonicalPrivilegeSet)
	if err != nil {
		return fmt.Errorf("parse current application ACL bindings: %w", err)
	}
	expected := map[migrate.AppACLSubject]string{migrate.AppACLSubjectCenterRuntime: roles.CenterRuntime, migrate.AppACLSubjectPlatformAdmin: roles.PlatformAdmin, migrate.AppACLSubjectRecordDeletionWorker: roles.DeletionWorker}
	for _, binding := range set.RoleBindings {
		if expected[binding.Subject] != binding.CatalogRole {
			return fmt.Errorf("application ACL principal binding mismatch")
		}
	}
	return nil
}

func checkWitness(ctx context.Context, pool *pgxpool.Pool, config Config) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin witness bootstrap observation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var valid bool
	err = tx.QueryRow(ctx, `select current_setting('server_version_num')::integer / 10000 = 16
		and session_user = current_user and (select oid=10 and rolsuper from pg_catalog.pg_roles where rolname=current_user)
		and (select count(*)=1 from public.record_platform_domain_identity)
		and exists (select 1 from public.record_platform_domain_identity d where d.domain_kind='deletion_witness' and d.identity_mode='postgres_system'
		 and d.domain_id=$1 and d.identity_epoch=$2 and d.postgres_system_identifier=$3 and d.database_oid=$4::oid and d.database_name=$5
		 and d.postgres_system_identifier=(select system_identifier::text from pg_catalog.pg_control_system())
		 and d.database_oid=(select oid from pg_catalog.pg_database where datname=current_database()) and d.database_name=current_database())
		and exists (select 1 from public.source_authority_state where singleton and generation=1 and mode='active' and recovery_id is null)
		and exists (select 1 from public.source_authority_principals where principal=$6::name and purpose='producer' and generation=1 and recovery_id is null)`,
		config.Witness.ID, int64(config.Witness.Epoch), config.Witness.SystemIdentifier, config.Witness.DatabaseOID, config.Witness.DatabaseName, config.Producer.Principal).Scan(&valid)
	if err != nil {
		return fmt.Errorf("read witness bootstrap identity: %w", err)
	}
	if !valid {
		return fmt.Errorf("witness bootstrap identity or producer binding mismatch")
	}
	if err := platformmigrate.VerifySourceRoleCredentials(ctx, tx, []platformmigrate.SourceRoleCredential{config.Producer}); err != nil {
		return fmt.Errorf("verify witness producer credentials: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit witness bootstrap observation: %w", err)
	}
	return nil
}

func commitApplication(ctx context.Context, pool *pgxpool.Pool, gate AdmissionGate, config Config, credentials []platformmigrate.SourceRoleCredential, commitments [6][32]byte, expected sourceretirement.DomainIdentity, activated bool) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin application enrollment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `lock table public.source_authority_admission in access exclusive mode`); err != nil {
		return fmt.Errorf("lock application source admission: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(1213154899,1)`); err != nil {
		return fmt.Errorf("lock application asset graph: %w", err)
	}
	actual, err := checkApplication(ctx, tx, gate, config, credentials)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("application identity changed during enrollment")
	}
	var admissionCount int
	if err := tx.QueryRow(ctx, `select count(*) from public.source_authority_admission`).Scan(&admissionCount); err != nil {
		return fmt.Errorf("read application source admission: %w", err)
	}
	var facts bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from public.source_retirement_barriers) or exists(select 1 from public.source_retirement_outbox) or exists(select 1 from public.source_retirement_pins) or exists(select 1 from public.source_retirement_coverage_unknown)`).Scan(&facts); err != nil {
		return fmt.Errorf("read application retirement facts: %w", err)
	}
	if admissionCount == 0 {
		var principalsExist bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from public.source_application_principals)`).Scan(&principalsExist); err != nil {
			return fmt.Errorf("read application source principals: %w", err)
		}
		if principalsExist || facts || activated {
			return fmt.Errorf("inconsistent initial application source enrollment")
		}
		_, err = tx.Exec(ctx, `insert into public.source_authority_admission(singleton,deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode) values (true,$1,$2,$3,$4::oid,$5,$6,1,'active')`, config.DeploymentID, config.Witness.ID, config.Witness.SystemIdentifier, config.Witness.DatabaseOID, config.Witness.DatabaseName, int64(config.Witness.Epoch))
		if err != nil {
			return fmt.Errorf("insert application source admission: %w", err)
		}
		for i, purpose := range []string{"runtime", "admin", "worker", "migrator", "authority"} {
			if _, err := tx.Exec(ctx, `insert into public.source_application_principals(principal,generation,purpose,credential_commitment) values ($1::name,1,$2,$3)`, credentials[i].Principal, purpose, commitments[i][:]); err != nil {
				return fmt.Errorf("insert application source principal: %w", err)
			}
		}
	} else {
		if admissionCount != 1 || (facts && !activated) {
			return fmt.Errorf("inconsistent existing application source enrollment")
		}
		var exact bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from public.source_authority_admission where singleton and deployment_id=$1 and witness_domain_id=$2 and witness_system_identifier=$3 and witness_database_oid=$4::oid and witness_database_name=$5 and witness_identity_epoch=$6 and generation=1 and mode='active' and recovery_id is null and coverage_seal is null and credential_manifest_digest is null and namespace_handoff_digest is null)`, config.DeploymentID, config.Witness.ID, config.Witness.SystemIdentifier, config.Witness.DatabaseOID, config.Witness.DatabaseName, int64(config.Witness.Epoch)).Scan(&exact); err != nil {
			return fmt.Errorf("read existing application source admission: %w", err)
		}
		if !exact {
			return fmt.Errorf("existing application source admission mismatch")
		}
		rows, err := tx.Query(ctx, `select principal::text,generation,purpose,credential_commitment from public.source_application_principals`)
		if err != nil {
			return fmt.Errorf("read existing application source principals: %w", err)
		}
		purposes := map[string]int{"runtime": 0, "admin": 1, "worker": 2, "migrator": 3, "authority": 4}
		count := 0
		for rows.Next() {
			var principal, purpose string
			var generation int64
			var commitment []byte
			if err := rows.Scan(&principal, &generation, &purpose, &commitment); err != nil {
				rows.Close()
				return fmt.Errorf("scan application source principal: %w", err)
			}
			index, ok := purposes[purpose]
			if !ok || generation != 1 || principal != credentials[index].Principal || !bytes.Equal(commitment, commitments[index][:]) {
				rows.Close()
				return fmt.Errorf("existing application source principal mismatch")
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("iterate application source principals: %w", err)
		}
		if count != 5 {
			return fmt.Errorf("incomplete application source principals: %d", count)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit application source enrollment (outcome uncertain): %w", err)
	}
	return nil
}
