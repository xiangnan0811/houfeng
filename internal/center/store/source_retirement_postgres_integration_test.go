package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceenrollment"
	"houfeng/internal/center/sourceretirement"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/vpsassets"
)

// Enrollment uses the production coordinator with independent APP and W2
// bootstrap pools and the restricted provisioner. The caller-owned outer
// maintenance guard and recovery/credential handoff are not exercised here.
type sourceRetirementPGFixture struct {
	app               recordPlatformPostgresFixture
	witnessDB         *pgxpool.Pool
	witnessReaderPool *pgxpool.Pool
	producer          *sourceretirement.Authority
	reader            *sourceretirement.Authority
	runtime           *pgxpool.Pool
	service           *SourceRetirementService
	gate              AdmissionGate
	deployment        string
	refresh           func()
}

func newSourceRetirementPGFixture(t *testing.T, ctx context.Context) *sourceRetirementPGFixture {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for PostgreSQL integration tests")
	}
	if os.Getenv("HOUFENG_DATABASE_URL") == "" || os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL") == "" || os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("source retirement requires the ownership-checked APP and independent witness PostgreSQL runner")
	}
	importAttachmentAuthorityMu.Lock()
	t.Cleanup(importAttachmentAuthorityMu.Unlock)
	f := &sourceRetirementPGFixture{app: newRecordPlatformPostgresBaseFixture(t, ctx)}

	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL"))
	if err != nil || config.ConnConfig.User != "postgres" {
		t.Fatalf("witness fixture requires a direct postgres bootstrap URL: %v", err)
	}
	witnessAdmin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(witnessAdmin.Close)
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(entropy[:])
	database := "hf_sr_" + suffix
	roles := platformmigrate.SourceAuthorityRoles{Owner: "hf_sr_o_" + suffix, Reader: "hf_sr_r_" + suffix, Producer: "hf_sr_p_" + suffix, Provisioner: "hf_sr_c_" + suffix}
	roleNames := []string{roles.Owner, roles.Reader, roles.Producer, roles.Provisioner}
	password := recordPlatformIntegrationPassword(t)
	if _, err := witnessAdmin.Exec(ctx, "create database "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	createdRoles := []string{}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if f.witnessDB != nil {
			f.witnessDB.Close()
		}
		if _, err := witnessAdmin.Exec(cleanup, "drop database "+pgx.Identifier{database}.Sanitize()+" with (force)"); err != nil {
			t.Errorf("drop owned witness database: %v", err)
		}
		for _, role := range createdRoles {
			if _, err := witnessAdmin.Exec(cleanup, "drop role "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Errorf("drop owned witness role: %v", err)
			}
		}
	})
	for _, role := range roleNames {
		attribute := "login"
		if role == roles.Owner {
			attribute = "nologin"
		}
		var statement string
		if err := witnessAdmin.QueryRow(ctx, `select format('create role %I `+attribute+` noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L', $1::text, $2::text)`, role, password).Scan(&statement); err != nil {
			t.Fatal(err)
		}
		if _, err := witnessAdmin.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
		createdRoles = append(createdRoles, role)
	}
	witnessConfig := config.Copy()
	witnessConfig.ConnConfig.Database = database
	f.witnessDB, err = pgxpool.NewWithConfig(ctx, witnessConfig)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := platformmigrate.ProvisionSourceAuthority(ctx, f.witnessDB, roles)
	if err != nil {
		t.Fatalf("provision W2: %v", err)
	}
	var appSystem string
	if err := f.app.db.QueryRow(ctx, `select system_identifier::text from pg_control_system()`).Scan(&appSystem); err != nil {
		t.Fatal(err)
	}
	if appSystem == identity.PostgresSystemIdentifier {
		t.Fatal("APP and W2 must be independent PostgreSQL systems")
	}
	domain := sourceretirement.DomainIdentity{ID: identity.ID, SystemIdentifier: identity.PostgresSystemIdentifier, DatabaseOID: identity.DatabaseOID, DatabaseName: identity.DatabaseName, Epoch: uint64(identity.IdentityEpoch)}
	openWitness := func(role string) *pgxpool.Pool {
		t.Helper()
		c := witnessConfig.Copy()
		c.ConnConfig.User, c.ConnConfig.Password = role, password
		pool, err := pgxpool.NewWithConfig(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		var session, current string
		if err := pool.QueryRow(ctx, `select session_user, current_user`).Scan(&session, &current); err != nil || session != role || current != role {
			t.Fatalf("direct W2 role %q: session=%q current=%q error=%v", role, session, current, err)
		}
		return pool
	}
	producerPool := openWitness(roles.Producer)
	f.witnessReaderPool = openWitness(roles.Reader)
	f.producer, err = sourceretirement.NewAuthority(producerPool, domain)
	if err != nil {
		t.Fatal(err)
	}
	f.reader, err = sourceretirement.NewAuthority(f.witnessReaderPool, domain)
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := sourceretirement.NewAuthority(openWitness(roles.Provisioner), domain)
	if err != nil {
		t.Fatal(err)
	}

	stateRoot := filepath.Join(t.TempDir(), "authority")
	state, err := recordauthority.CreateComposeState(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	const authorityRole = "houfeng_records_authority"
	var exists bool
	if err := f.app.db.QueryRow(ctx, `select pg_catalog.to_regrole($1) is not null`, authorityRole).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("source fixture requires absent authority role")
	}
	if err := platformmigrate.ProvisionComposeBootstrap(ctx, f.app.db, platformmigrate.ComposeBootstrapConfig{
		DatabaseName: f.app.databaseName, BootstrapRole: f.app.bootstrapOwner, AuthorityRole: authorityRole,
		Roles:     platformmigrate.AppRoleSetV2{CenterRuntime: f.app.runtime, PlatformAdmin: f.app.admin, DeletionWorker: f.app.deletionWorker, Migrator: f.app.migrator},
		Passwords: platformmigrate.ComposeRolePasswords{Runtime: f.app.passwords[f.app.runtime], PlatformAdmin: f.app.passwords[f.app.admin], DeletionWorker: f.app.passwords[f.app.deletionWorker], Migrator: f.app.passwords[f.app.migrator], Authority: state.DatabasePassword()},
	}); err != nil {
		t.Fatal(err)
	}
	f.app.passwords[authorityRole] = state.DatabasePassword()
	f.app.dropRole(t, authorityRole)
	migrator := f.app.openDirectRolePool(t, ctx, f.app.migrator, "source-retirement-migrator", 1)
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migrator, f.app.runtime, f.app.admin, f.app.deletionWorker); err != nil {
		t.Fatal(err)
	}
	command, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var receipt []byte
	if err := migrator.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, command).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if err := recordauthority.PersistActivationReceipt(stateRoot, state, receipt); err != nil {
		t.Fatal(err)
	}
	authority := f.app.openDirectRolePool(t, ctx, authorityRole, "source-retirement-heartbeat", 1)
	f.refresh = func() {
		t.Helper()
		cmd, want, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		var got time.Time
		if err := authority.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, cmd).Scan(&got); err != nil || !got.Equal(want) {
			t.Fatalf("signed heartbeat expiry=%v want=%v error=%v", got, want, err)
		}
	}
	f.refresh()
	f.runtime = f.app.openDirectRuntimePool(t, ctx, "source-retirement-runtime", 8)
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, f.runtime); err != nil {
		t.Fatal(err)
	}
	admin := f.app.openDirectRolePool(t, ctx, f.app.admin, "source-retirement-admin", 4)
	f.deployment = string(state.DeploymentID)
	membershipIdentity := DeploymentMembershipIdentity{InstanceID: "compose-center", DeploymentID: state.DeploymentID, ProjectID: recordplatform.ProjectIDDefault, InstanceKind: "api", Capability: "records.runtime"}
	f.gate, err = NewDeploymentMembershipAdmissionGate(membershipIdentity)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapGate, err := NewBootstrapDeploymentMembershipAdmissionGate(membershipIdentity)
	if err != nil {
		t.Fatal(err)
	}
	for _, principal := range []struct {
		name string
		pool *pgxpool.Pool
	}{{"runtime", f.runtime}, {"admin", admin}} {
		tx, err := principal.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		err = bootstrapGate.Admit(ctx, tx)
		_ = tx.Rollback(ctx)
		if !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
			t.Fatalf("%s principal used bootstrap enrollment gate: %v", principal.name, err)
		}
	}
	appIdentity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, f.app.db, platformmigrate.DomainKindApplication)
	if err != nil {
		t.Fatalf("provision APP identity: %v", err)
	}
	enrollmentConfig := sourceenrollment.Config{
		DeploymentID: f.deployment,
		Witness:      domain,
		Roles: platformmigrate.AppRoleSetV2{
			CenterRuntime: f.app.runtime, PlatformAdmin: f.app.admin,
			DeletionWorker: f.app.deletionWorker, Migrator: f.app.migrator,
		},
		AuthorityRole: authorityRole,
		Passwords: platformmigrate.ComposeRolePasswords{
			Runtime: f.app.passwords[f.app.runtime], PlatformAdmin: f.app.passwords[f.app.admin],
			DeletionWorker: f.app.passwords[f.app.deletionWorker], Migrator: f.app.passwords[f.app.migrator],
			Authority: state.DatabasePassword(),
		},
		Producer: platformmigrate.SourceRoleCredential{Principal: roles.Producer, Password: password},
	}
	enroll := func(config sourceenrollment.Config) (sourceretirement.ApplicationEnrollment, error) {
		return sourceenrollment.Enroll(ctx, f.app.db, f.witnessDB, provisioner, bootstrapGate, config)
	}
	badSecret := enrollmentConfig
	badSecret.Passwords.Runtime += "-incorrect"
	if _, err := enroll(badSecret); err == nil {
		t.Fatal("initial enrollment accepted an incorrect APP runtime secret")
	}
	badProducerSecret := enrollmentConfig
	badProducerSecret.Producer.Password += "-incorrect"
	if _, err := enroll(badProducerSecret); err == nil {
		t.Fatal("initial enrollment accepted an incorrect W2 producer secret")
	}
	wrongIdentity := enrollmentConfig
	wrongIdentity.Witness.DatabaseName += "_wrong"
	if _, err := enroll(wrongIdentity); err == nil {
		t.Fatal("initial enrollment accepted a wrong W2 identity")
	}
	wrongDeployment := enrollmentConfig
	wrongDeployment.DeploymentID = "dp-" + string(bytes.Repeat([]byte{'0'}, 64))
	if wrongDeployment.DeploymentID == f.deployment {
		wrongDeployment.DeploymentID = "dp-" + string(bytes.Repeat([]byte{'1'}, 64))
	}
	if _, err := enroll(wrongDeployment); err == nil {
		t.Fatal("initial enrollment accepted a wrong deployment identity")
	}
	var appBindings, witnessBindings int
	if err := f.app.db.QueryRow(ctx, `select (select count(*) from public.source_authority_admission) + (select count(*) from public.source_application_principals)`).Scan(&appBindings); err != nil {
		t.Fatal(err)
	}
	if err := f.witnessDB.QueryRow(ctx, `select count(*) from public.source_application_enrollments`).Scan(&witnessBindings); err != nil {
		t.Fatal(err)
	}
	if appBindings != 0 || witnessBindings != 0 {
		t.Fatalf("rejected initial enrollment left APP bindings=%d W2 bindings=%d", appBindings, witnessBindings)
	}
	// The unenrolled writer uses the same admission/graph path as a real source
	// writer. Its transaction must drain before enrollment can publish APP state.
	existing, key := f.newVPS(t, ctx, "pre-enrollment writer")
	_, err = f.producer.Prepare(ctx, key, "retirement_before_enrollment", [32]byte{1})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" || pgErr.Message != "source application enrollment is absent or inactive" {
		t.Fatalf("producer before enrollment was not rejected by the W2 activation guard: %v", err)
	}
	writer, err := f.runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	if err := lockAssetGraph(ctx, writer); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(ctx, `update public.vps_assets set display_name=$1 where vps_id=$2`, "writer drained", existing.VPSID); err != nil {
		t.Fatal(err)
	}
	var writerPID int32
	if err := writer.QueryRow(ctx, `select pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	type enrollmentResult struct {
		value sourceretirement.ApplicationEnrollment
		err   error
	}
	result := make(chan enrollmentResult, 1)
	go func() {
		value, err := enroll(enrollmentConfig)
		result <- enrollmentResult{value: value, err: err}
	}()
	waitCtx, cancelWait := context.WithTimeout(ctx, 30*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := f.app.db.QueryRow(waitCtx, `select exists (
			select 1 from pg_catalog.pg_locks l
			where l.relation = 'public.source_authority_admission'::regclass
			  and l.mode = 'AccessExclusiveLock' and not l.granted
			  and $1 = any(pg_catalog.pg_blocking_pids(l.pid))
		)`, writerPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe enrollment waiting for writer: %v", err)
		}
		if blocked {
			break
		}
		select {
		case early := <-result:
			t.Fatalf("enrollment finished before draining writer: %v", early.err)
		case <-waitCtx.Done():
			t.Fatal("enrollment did not reach the APP writer drain")
		case <-ticker.C:
		}
	}
	pending, err := provisioner.ReadApplicationEnrollment(ctx, f.deployment)
	if err != nil || pending.Generation != 1 || pending.ActivationDigest != nil {
		t.Fatalf("writer-drained enrollment must remain unactivated in W2: %+v, error=%v", pending, err)
	}
	_, err = f.producer.Prepare(ctx, key, "retirement_before_enrollment", [32]byte{1})
	pgErr = nil
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" || pgErr.Message != "source application enrollment is absent or inactive" {
		t.Fatalf("producer before APP commit was not rejected by the W2 activation guard: %v", err)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit pre-enrollment writer: %v", err)
	}
	var writtenName string
	if err := f.app.db.QueryRow(ctx, `select display_name from public.vps_assets where vps_id=$1`, existing.VPSID).Scan(&writtenName); err != nil || writtenName != "writer drained" {
		t.Fatalf("pre-enrollment writer did not commit: name=%q error=%v", writtenName, err)
	}
	first := <-result
	if first.err != nil {
		t.Fatalf("production initial enrollment: %v", first.err)
	}
	if first.value.Generation != 1 || first.value.DeploymentID != f.deployment || first.value.ActivationDigest == nil ||
		first.value.Application.ID != appIdentity.ID || first.value.Application.SystemIdentifier != appIdentity.PostgresSystemIdentifier ||
		first.value.Application.DatabaseOID != appIdentity.DatabaseOID || first.value.Application.DatabaseName != appIdentity.DatabaseName ||
		first.value.Application.Epoch != uint64(appIdentity.IdentityEpoch) {
		t.Fatalf("initial enrollment returned unexpected binding: %+v", first.value)
	}
	replay, err := enroll(enrollmentConfig)
	if err != nil || !reflect.DeepEqual(first.value, replay) {
		t.Fatalf("exact initial enrollment replay = %+v, error=%v", replay, err)
	}
	if _, err := enroll(badSecret); err == nil {
		t.Fatal("enrollment replay accepted a changed APP secret")
	}
	if _, err := f.app.db.Exec(ctx, `insert into public.users(user_id,username,password_hash,display_name,role) values ('usr_000000000000000000000001','source-retirement-admin','test-hash','Retirement admin','admin')`); err != nil {
		t.Fatal(err)
	}
	f.service, err = NewSourceRetirementService(admin, f.gate, f.producer)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *sourceRetirementPGFixture) newVPS(t *testing.T, ctx context.Context, name string) (vpsassets.Record, sourceretirement.SourceKey) {
	t.Helper()
	vps, err := NewPostgresVPSAssetRepository(f.runtime).CreateVPSAsset(ctx, vpsassets.CreateInput{DisplayName: name, LifecycleStatus: vpsassets.LifecycleActive, UsageStatus: vpsassets.UsageInUse})
	if err != nil {
		t.Fatalf("create real VPS: %v", err)
	}
	return vps, sourceretirement.SourceKey{DeploymentID: f.deployment, ProjectID: recordauth.ProjectIDDefault, Kind: recordauth.SourceKindVPS, ID: vps.VPSID}
}

func (f *sourceRetirementPGFixture) prepare(t *testing.T, ctx context.Context, key sourceretirement.SourceKey, id string, floor recordauth.VisibilityScope) (SourceRetirementPreview, PreparedSourceRetirement) {
	t.Helper()
	f.refresh()
	preview, err := f.service.Preview(ctx, "usr_000000000000000000000001", key)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	prepared, err := f.service.Prepare(ctx, SourceRetirementRequest{Source: key, RetirementID: id, ActorUserID: "usr_000000000000000000000001", ExpectedRevision: preview.Revision, FinalFloor: floor})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	return preview, prepared
}

func (f *sourceRetirementPGFixture) tombstoneReader(t *testing.T) *WitnessedRecordSubjectTombstoneReader {
	t.Helper()
	reader, err := NewWitnessedRecordSubjectTombstoneReader(recordplatform.DeploymentID(f.deployment), recordauth.ProjectIDDefault, f.reader, f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func TestPostgresIntegrationSourceRetirementAuthority(t *testing.T) {
	t.Run("legacy_coverage", sourceRetirementLegacyCoverage)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	f := newSourceRetirementPGFixture(t, ctx)
	floor := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin}, nil, 1)
	vps, key := f.newVPS(t, ctx, "retirement subject")
	repo := NewPostgresVPSAssetRepository(f.runtime)
	lifecycle := NewPostgresAssetLifecycleRepository(f.runtime)
	archiveInput := archiveSafetyRequest(t, ctx, lifecycle, vps, "blocked-retirement")
	preview, prepared := f.prepare(t, ctx, key, "retirement_subject", floor)
	if _, err := repo.PatchVPSAsset(ctx, key.ID, vpsassets.PatchInput{DisplayName: vpsassets.PatchString("after prepare")}); err != nil {
		t.Fatalf("prepare must not block live writer: %v", err)
	}
	if _, err := f.service.CommitBarrier(ctx, prepared); err == nil {
		t.Fatal("changed original source CAS accepted")
	}
	var count int
	if err := f.app.db.QueryRow(ctx, `select count(*) from public.source_retirement_barriers where retirement_id='retirement_subject'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale CAS created barrier count=%d error=%v", count, err)
	}
	// A new requested retirement has a new ID and explicitly observed CAS; it is
	// not an automatic retry of an uncertain operation.
	preview, prepared = f.prepare(t, ctx, key, "retirement_subject_final", floor)
	barrier, err := f.service.CommitBarrier(ctx, prepared)
	if err != nil {
		t.Fatalf("commit barrier: %v", err)
	}
	var storedLast, storedFloor, storedRevision, outboxDigest []byte
	if err := f.app.db.QueryRow(ctx, `select b.last_live,b.final_floor,b.expected_source_revision,o.barrier_digest from public.source_retirement_barriers b join public.source_retirement_outbox o using (retirement_id) where b.retirement_id=$1`, "retirement_subject_final").Scan(&storedLast, &storedFloor, &storedRevision, &outboxDigest); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(storedLast, preview.LastLive.CanonicalBytes()) || !bytes.Equal(storedFloor, floor.CanonicalBytes()) || !bytes.Equal(storedRevision, preview.Revision[:]) || !bytes.Equal(outboxDigest, barrier[:]) {
		t.Fatal("barrier and outbox did not freeze original observation")
	}
	if _, err := repo.PatchVPSAsset(ctx, key.ID, vpsassets.PatchInput{DisplayName: vpsassets.PatchString("late write")}); !errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
		t.Fatalf("retiring VPS patch = %v", err)
	}
	if _, err := lifecycle.ApplyVPSArchive(ctx, key.ID, archiveInput); !errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
		t.Fatalf("retiring VPS archive = %v", err)
	}
	if _, _, err := NewPostgresMonitoringInstanceRepository(f.runtime).CreateLinkedMonitoringInstance(ctx, key.ID, monitoringinstances.CreateInput{DisplayName: "late reference", LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment, Labels: []string{}}, ""); !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkNotFound) {
		t.Fatalf("retiring VPS accepted linked source: %v", err)
	}
	reader := f.tombstoneReader(t)
	if _, err := reader.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID); !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("barrier without terminal and pin = %v", err)
	}
	if _, err := repo.GetVPSAsset(ctx, key.ID); err != nil {
		t.Fatalf("live row unexpectedly absent: %v", err)
	}
	if err := f.service.Deliver(ctx, "retirement_subject_final"); err != nil {
		t.Fatalf("deliver committed barrier: %v", err)
	}
	observation, err := f.reader.Observe(ctx, key)
	if err != nil || observation.Terminal == nil || observation.Terminal.Sequence != observation.HeadSequence || observation.TerminalHash != observation.HeadHash {
		t.Fatalf("verified W2 terminal/head: %+v, %v", observation, err)
	}
	var pinCanonical, pinHash []byte
	var pinSequence int64
	if err := f.app.db.QueryRow(ctx, `select canonical_terminal,terminal_hash,terminal_sequence from public.source_retirement_pins where retirement_id=$1`, "retirement_subject_final").Scan(&pinCanonical, &pinHash, &pinSequence); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pinHash, observation.TerminalHash[:]) || uint64(pinSequence) != observation.Terminal.Sequence {
		t.Fatal("pin does not bind W2 terminal")
	}
	if err := f.service.Deliver(ctx, "retirement_subject_final"); err != nil {
		t.Fatalf("deliver exact replay: %v", err)
	}
	if err := f.service.InstallPin(ctx, "retirement_subject_final"); err != nil {
		t.Fatalf("pin exact replay: %v", err)
	}
	var afterCanonical, afterHash []byte
	var afterSequence int64
	if err := f.app.db.QueryRow(ctx, `select canonical_terminal,terminal_hash,terminal_sequence from public.source_retirement_pins where retirement_id=$1`, "retirement_subject_final").Scan(&afterCanonical, &afterHash, &afterSequence); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pinCanonical, afterCanonical) || !bytes.Equal(pinHash, afterHash) || pinSequence != afterSequence {
		t.Fatal("exact replay changed pin")
	}
	if err := f.witnessDB.QueryRow(ctx, `select count(*) from public.source_retirement_terminals where deployment_id=$1 and project_id='default' and source_kind='vps' and source_id=$2`, key.DeploymentID, key.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("terminal count=%d error=%v", count, err)
	}
	badBarrier := barrier
	badBarrier[0] ^= 1
	if _, err := f.producer.Append(ctx, "retirement_subject_final", badBarrier, preview.LastLive, floor); err == nil {
		t.Fatal("conflicting barrier accepted")
	}
	otherFloor := mustStoreProjectVisibility(t)
	if _, err := f.producer.Append(ctx, "retirement_subject_final", barrier, preview.LastLive, otherFloor); err == nil {
		t.Fatal("conflicting floor accepted")
	}
	if err := f.service.Deliver(ctx, "retirement_nonexistent"); err == nil {
		t.Fatal("unknown retirement delivered")
	}

	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{NewVPSRecordSubjectAdapter(repo)})
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewRecordSubjectReadResolver(registry, reader)
	capture := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleProjectAdmin, recordauth.RoleViewer}, nil, 1)
	input := RecordSubjectReadInput{Reference: storeRecordReference(records.SubjectKindVPS, key.ID), IdentitySnapshot: mustStoreRecordSnapshot(t, records.SubjectKindVPS, map[string]string{"display_name": "captured"}), CaptureAuthorization: mustStoreLiveSourceAuthorization(t, key.Kind, key.ID, capture)}
	actor := mustStoreRecordActor(t)
	resolve := func() {
		t.Helper()
		got, err := resolver.Resolve(ctx, actor, input)
		if err != nil {
			t.Fatalf("resolve retired subject: %v", err)
		}
		auth := got.CaptureAuthorization
		if got.LiveRoute != "" || auth.State != recordauth.SourceStateTombstoned || auth.CurrentScope != nil || auth.LastLiveScope == nil || auth.FinalFloor == nil || auth.CaptureScope.CanonicalHash != capture.CanonicalHash || auth.LastLiveScope.CanonicalHash != preview.LastLive.CanonicalHash || auth.FinalFloor.CanonicalHash != floor.CanonicalHash {
			t.Fatalf("incorrect tombstone intersection evidence: %+v", got)
		}
		resource := recordauth.ResourceScope{Version: recordauth.ResourceScopeVersionV1, ProjectID: key.ProjectID, Visibility: mustStoreProjectVisibility(t), Sources: []recordauth.SourceAuthorization{auth}}
		admin := recordauth.ActorScope{UserID: "usr_000000000000000000000001", Role: recordauth.RoleProjectAdmin, ProjectID: key.ProjectID}
		viewer := recordauth.ActorScope{UserID: "usr_000000000000000000000002", Role: recordauth.RoleViewer, ProjectID: key.ProjectID}
		if err := recordauth.Authorize(admin, recordauth.CapabilityRecordRead, resource); err != nil {
			t.Fatalf("admin floor should permit read: %v", err)
		}
		if err := recordauth.Authorize(viewer, recordauth.CapabilityRecordRead, resource); !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("floor must deny viewer: %v", err)
		}
		viewerCapture := mustStoreRestrictedVisibility(t, []recordauth.Role{recordauth.RoleViewer}, nil, 1)
		narrowInput := input
		narrowInput.CaptureAuthorization = mustStoreLiveSourceAuthorization(t, key.Kind, key.ID, viewerCapture)
		narrow, err := resolver.Resolve(ctx, actor, narrowInput)
		if err != nil {
			t.Fatalf("resolve narrower captured scope: %v", err)
		}
		resource.Sources = []recordauth.SourceAuthorization{narrow.CaptureAuthorization}
		if err := recordauth.Authorize(admin, recordauth.CapabilityRecordRead, resource); !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("capture must deny admin despite floor: %v", err)
		}
		if err := recordauth.Authorize(viewer, recordauth.CapabilityRecordRead, resource); !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("floor must deny viewer despite capture: %v", err)
		}
	}
	resolve()
	if _, err := f.app.db.Exec(ctx, `create table public.source_retirement_fixture_vps as select * from public.vps_assets where vps_id=$1`, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(ctx, `delete from public.vps_assets where vps_id=$1`, key.ID); err != nil {
		t.Fatalf("fixture absent-source restoration: %v", err)
	}
	resolve()
	// Model a restored/recreated row only through fixture bootstrap, then exercise
	// the real writer and reader: the old identity remains permanently fenced.
	if _, err := f.app.db.Exec(ctx, `insert into public.vps_assets select * from public.source_retirement_fixture_vps`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PatchVPSAsset(ctx, key.ID, vpsassets.PatchInput{DisplayName: vpsassets.PatchString("revived")}); !errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
		t.Fatalf("recreated source patch = %v", err)
	}
	resolve()

	sourceRetirementPGFailures(t, ctx, f, key, preview, floor, reader)
}

func sourceRetirementPGFailures(t *testing.T, ctx context.Context, f *sourceRetirementPGFixture, key sourceretirement.SourceKey, preview SourceRetirementPreview, floor recordauth.VisibilityScope, reader *WitnessedRecordSubjectTombstoneReader) {
	t.Helper()
	// The earlier stale-CAS prepare remains durable. It must not be usable to
	// install another terminal after the later retirement has completed.
	var barrier []byte
	if err := f.app.db.QueryRow(ctx, `select barrier_digest from public.source_retirement_barriers where retirement_id='retirement_subject_final'`).Scan(&barrier); err != nil {
		t.Fatal(err)
	}
	var digest [32]byte
	copy(digest[:], barrier)
	if _, err := f.producer.Append(ctx, "retirement_subject", digest, preview.LastLive, floor); err == nil {
		t.Fatal("different retirement ID installed second terminal")
	}
	var count int
	if err := f.witnessDB.QueryRow(ctx, `select count(*) from public.source_retirement_terminals where deployment_id=$1 and project_id='default' and source_kind='vps' and source_id=$2`, key.DeploymentID, key.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("conflicting terminal count=%d error=%v", count, err)
	}

	for _, change := range []struct{ name, update, restore string }{
		{"disabled", `update public.users set disabled_at=now() where user_id='usr_000000000000000000000001'`, `update public.users set disabled_at=null where user_id='usr_000000000000000000000001'`},
		{"role", `update public.users set role='viewer' where user_id='usr_000000000000000000000001'`, `update public.users set role='admin' where user_id='usr_000000000000000000000001'`},
	} {
		t.Run("actor_"+change.name, func(t *testing.T) {
			_, actorKey := f.newVPS(t, ctx, "actor change "+change.name)
			id := "retirement_actor_" + change.name
			_, prepared := f.prepare(t, ctx, actorKey, id, floor)
			if _, err := f.app.db.Exec(ctx, change.update); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := f.app.db.Exec(context.Background(), change.restore); err != nil {
					t.Errorf("restore fixture actor: %v", err)
				}
			}()
			if _, err := f.service.CommitBarrier(ctx, prepared); err == nil {
				t.Fatal("changed actor committed barrier")
			}
			var barriers, outboxes int
			if err := f.app.db.QueryRow(ctx, `select (select count(*) from public.source_retirement_barriers where retirement_id=$1), (select count(*) from public.source_retirement_outbox where retirement_id=$1)`, id).Scan(&barriers, &outboxes); err != nil || barriers != 0 || outboxes != 0 {
				t.Fatalf("actor change barriers=%d outboxes=%d error=%v", barriers, outboxes, err)
			}
			observed, err := f.reader.Observe(ctx, actorKey)
			if err != nil || observed.PrepareCount != 1 || observed.Terminal != nil {
				t.Fatalf("durable intent after actor revocation: %+v error=%v", observed, err)
			}
		})
	}

	t.Run("ordinary_archive_is_not_terminal", func(t *testing.T) {
		vps, archiveKey := f.newVPS(t, ctx, "ordinary archive")
		seedArchiveSafetySession(t, ctx, f.runtime, vps.VPSID)
		observer, err := NewReceiverHealthObserver(ctx, f.runtime)
		if err != nil {
			t.Fatal(err)
		}
		if err := observer.observe(ctx, true, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := f.app.db.Exec(ctx, `update receiver_health set healthy_since=now()-interval '181 minutes'`); err != nil {
			t.Fatal(err)
		}
		lifecycle := NewPostgresAssetLifecycleRepository(f.runtime)
		request := archiveSafetyRequest(t, ctx, lifecycle, vps, "ordinary-archive")
		if _, err := lifecycle.ApplyVPSArchive(ctx, vps.VPSID, request); err != nil {
			t.Fatalf("ordinary archive: %v", err)
		}
		observation, err := f.reader.Observe(ctx, archiveKey)
		if err != nil || observation.Terminal != nil || observation.PrepareCount != 0 {
			t.Fatalf("archive incorrectly created retirement: %+v error=%v", observation, err)
		}
	})

	// Corruption is performed only by this isolated superuser fixture. Trigger
	// disable/update/enable is one transaction, and original bytes are restored
	// after each subtest; no product authority path receives these privileges.
	mutate := func(t *testing.T, table, sql string, args ...any) {
		t.Helper()
		tx, err := f.app.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if table != "" {
			if _, err := tx.Exec(ctx, "alter table public."+table+" disable trigger user"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
		if table != "" {
			if _, err := tx.Exec(ctx, "alter table public."+table+" enable trigger user"); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertUnavailable := func(t *testing.T) {
		t.Helper()
		if _, err := reader.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID); !errors.Is(err, ErrRecordSubjectUnavailable) {
			t.Fatalf("corrupt authority returned %v", err)
		}
	}
	var originalHash, originalCanonical, originalBarrier []byte
	if err := f.app.db.QueryRow(ctx, `select terminal_hash,canonical_terminal,barrier_digest from public.source_retirement_pins where retirement_id='retirement_subject_final'`).Scan(&originalHash, &originalCanonical, &originalBarrier); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, column string
		bad, good    []byte
	}{
		{"pin_hash", "terminal_hash", bytes.Repeat([]byte{0x37}, 32), originalHash},
		{"pin_canonical", "canonical_terminal", []byte{1, 2, 3}, originalCanonical},
		{"pin_barrier", "barrier_digest", bytes.Repeat([]byte{0x39}, 32), originalBarrier},
	} {
		t.Run(test.name, func(t *testing.T) {
			sql := fmt.Sprintf(`update public.source_retirement_pins set %s=$1 where retirement_id='retirement_subject_final'`, test.column)
			mutate(t, "source_retirement_pins", sql, test.bad)
			defer mutate(t, "source_retirement_pins", sql, test.good)
			assertUnavailable(t)
		})
	}
	t.Run("missing_current_pin", func(t *testing.T) {
		sql := `update public.source_retirement_pins set installed_generation=$1 where retirement_id='retirement_subject_final'`
		mutate(t, "source_retirement_pins", sql, 2)
		defer mutate(t, "source_retirement_pins", sql, 1)
		assertUnavailable(t)
	})
	t.Run("wrong_domain", func(t *testing.T) {
		var domain string
		if err := f.app.db.QueryRow(ctx, `select witness_domain_id from public.source_authority_admission where singleton`).Scan(&domain); err != nil {
			t.Fatal(err)
		}
		sql := `update public.source_authority_admission set witness_domain_id=$1 where singleton`
		mutate(t, "", sql, "rd-"+string(bytes.Repeat([]byte{'a'}, 64)))
		defer mutate(t, "", sql, domain)
		assertUnavailable(t)
	})
	t.Run("wrong_generation", func(t *testing.T) {
		sql := `update public.source_authority_admission set generation=$1 where singleton`
		mutate(t, "", sql, 2)
		defer mutate(t, "", sql, 1)
		assertUnavailable(t)
	})
	t.Run("coverage_unknown", func(t *testing.T) {
		mutate(t, "", `insert into public.source_retirement_coverage_unknown(project_id,source_kind,source_id,recovery_id,installed_generation,prepare_ids) values ('default','vps',$1,'recovery_test',1,array['retirement_unknown'])`, key.ID)
		defer mutate(t, "source_retirement_coverage_unknown", `delete from public.source_retirement_coverage_unknown where source_id=$1`, key.ID)
		assertUnavailable(t)
	})

	// ForTransaction is only valid inside the prefetched proof/current APP guard
	// boundary. Closing the W2 reader proves this checker cannot make W2 calls;
	// it does not authorize ordinary offline resolution.
	t.Run("prepared_transaction_rechecks_fresh_app_facts_without_w2_io", func(t *testing.T) {
		prepared, err := reader.Prefetch(ctx, []sourceretirement.SourceKey{key})
		if err != nil {
			t.Fatalf("prefetch verified terminal: %v", err)
		}
		f.witnessReaderPool.Close()
		tx, err := f.runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		checker := prepared.ForTransaction(tx)
		if _, err := checker.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID); err != nil {
			t.Fatalf("prefetched proof after W2 disconnect: %v", err)
		}
		assertUnavailable(t)
		mutate(t, "", `update public.source_authority_admission set generation=2 where singleton`)
		if _, err := checker.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID); !errors.Is(err, ErrRecordSubjectUnavailable) {
			t.Fatalf("changed generation accepted: %v", err)
		}
		mutate(t, "", `update public.source_authority_admission set generation=1 where singleton`)
		mutate(t, "source_retirement_barriers", `update public.source_retirement_barriers set barrier_digest=$1 where retirement_id='retirement_subject_final'`, bytes.Repeat([]byte{0x51}, 32))
		defer mutate(t, "source_retirement_barriers", `update public.source_retirement_barriers set barrier_digest=$1 where retirement_id='retirement_subject_final'`, barrier)
		if _, err := checker.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID); !errors.Is(err, ErrRecordSubjectUnavailable) {
			t.Fatalf("changed barrier accepted: %v", err)
		}
	})
}
