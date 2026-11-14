package migrate

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/sourceretirement"
)

// These isolated APP fixtures exercise the actual activation projector and
// launch commands. Their source and birth rows are fixture data, not evidence
// that the protected initializer or the W2 deployment ceremony has run.
func TestPostgresIntegrationRecordBirthLaunchAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	t.Run("never_installed_and_replacement", func(t *testing.T) {
		fixture, owner, runtime, worker, activation := birthLaunchTestFixture(t, ctx)
		for _, pool := range []*pgxpool.Pool{runtime, worker} {
			birthLaunchAdmit(t, ctx, pool, "", 0, "")
		}

		center, processor := birthLaunchFixturePair(activation.DeploymentID, activation.ActivationBundleDigest, nil)
		centerBytes := birthLaunchEncode(t, center)
		processorBytes := birthLaunchEncode(t, processor)
		centerDigest := birthLaunchDigest(centerBytes)
		processorDigest := birthLaunchDigest(processorBytes)
		revision := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0)
		if revision != 1 {
			t.Fatalf("first staged revision = %d, want 1", revision)
		}
		for _, pool := range []*pgxpool.Pool{runtime, worker} {
			birthLaunchAdmit(t, ctx, pool, centerDigest, 0, "42501")
		}
		for _, expected := range []int64{0, 1} {
			if got := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, expected); got != revision {
				t.Fatalf("closed exact replay at %d = %d, want %d", expected, got, revision)
			}
		}
		birthLaunchAssertState(t, ctx, owner, fixture, revision, false, centerBytes, processorBytes)
		birthLaunchOpen(t, ctx, owner, revision)
		for _, role := range []struct {
			name string
			pool *pgxpool.Pool
		}{{"runtime", runtime}, {"worker", worker}} {
			t.Run(role.name, func(t *testing.T) {
				birthLaunchAdmit(t, ctx, role.pool, "", 0, "42501")
				birthLaunchAdmit(t, ctx, role.pool, strings.Repeat("0", 64), 0, "42501")
				birthLaunchAdmit(t, ctx, role.pool, strings.ToUpper(centerDigest), 0, "42501")
				birthLaunchAdmit(t, ctx, role.pool, birthLaunchDigest(append(append([]byte(nil), centerBytes...), '\n')), 0, "42501")
				birthLaunchAdmit(t, ctx, role.pool, centerDigest, 0, "")
				birthLaunchAdmit(t, ctx, role.pool, processorDigest, 0, "")
			})
		}
		birthLaunchAssertUnprivileged(t, ctx, fixture, owner, runtime, worker, centerBytes, processorBytes, revision)
		if got := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0); got != revision {
			t.Fatalf("open exact replay = %d, want %d", got, revision)
		}
		birthLaunchAssertState(t, ctx, owner, fixture, revision, true, centerBytes, processorBytes)

		// A replaced executable changes the canonical launch file, even though
		// deployment, witness, and birth identity are otherwise unchanged.
		replaced := center
		replaced.BinaryDigest = sha256.Sum256([]byte("replacement center executable"))
		replacedBytes := birthLaunchEncode(t, replaced)
		replacedDigest := birthLaunchDigest(replacedBytes)
		birthLaunchAdmit(t, ctx, runtime, replacedDigest, 0, "42501")
		_, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, replacedBytes, processorBytes, fixture.runtime, fixture.deletionWorker, revision)
		requirePostgresSQLState(t, err, "40001")
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision-1)
		requirePostgresSQLState(t, err, "40001")
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision-1)
		requirePostgresSQLState(t, err, "42501")
		birthLaunchAssertState(t, ctx, owner, fixture, revision, true, centerBytes, processorBytes)
		birthLaunchClose(t, ctx, owner, revision)
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, replacedBytes, processorBytes, fixture.runtime, fixture.deletionWorker, 0)
		requirePostgresSQLState(t, err, "40001")
		second := birthLaunchStage(t, ctx, owner, fixture, replacedBytes, processorBytes, revision)
		if second != revision+1 {
			t.Fatalf("replacement revision = %d, want %d", second, revision+1)
		}
		for _, expected := range []int64{revision, second} {
			if got := birthLaunchStage(t, ctx, owner, fixture, replacedBytes, processorBytes, expected); got != second {
				t.Fatalf("replacement replay at %d = %d, want %d", expected, got, second)
			}
		}
		birthLaunchAdmit(t, ctx, runtime, replacedDigest, 0, "42501")
		birthLaunchOpen(t, ctx, owner, second)
		birthLaunchAdmit(t, ctx, runtime, centerDigest, 0, "42501")
		birthLaunchAdmit(t, ctx, runtime, replacedDigest, 0, "")
		birthLaunchAdmit(t, ctx, worker, processorDigest, 0, "")

		// Source can be enrolled while birth remains never installed. A launch
		// from before enrollment must not survive the changed W2 identity.
		witness := sourceretirement.DomainIdentity{
			ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "1", DatabaseOID: 1,
			DatabaseName: "checkpoint_fixture", Epoch: 1,
		}
		if _, err := owner.Exec(ctx, `insert into public.source_authority_admission(singleton,deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode) values(true,$1,$2,$3,$4,$5,$6,1,'active')`, activation.DeploymentID, witness.ID, witness.SystemIdentifier, witness.DatabaseOID, witness.DatabaseName, witness.Epoch); err != nil {
			t.Fatalf("seed source-enrolled ordinary fixture: %v", err)
		}
		if _, err := owner.Exec(ctx, `insert into public.source_application_principals(principal,generation,purpose) values($1,1,'runtime'),($2,1,'worker')`, fixture.runtime, fixture.deletionWorker); err != nil {
			t.Fatalf("seed source-enrolled fixture principals: %v", err)
		}
		birthLaunchAdmit(t, ctx, runtime, replacedDigest, 0, "42501")
		birthLaunchClose(t, ctx, owner, second)
		enrolledCenter, enrolledProcessor := replaced, processor
		enrolledCenter.SourceGeneration, enrolledCenter.Witness = 1, witness
		enrolledProcessor.SourceGeneration, enrolledProcessor.Witness = 1, witness
		enrolledCenterBytes := birthLaunchEncode(t, enrolledCenter)
		enrolledProcessorBytes := birthLaunchEncode(t, enrolledProcessor)
		third := birthLaunchStage(t, ctx, owner, fixture, enrolledCenterBytes, enrolledProcessorBytes, second)
		if third != second+1 {
			t.Fatalf("source-enrolled revision = %d, want %d", third, second+1)
		}
		birthLaunchOpen(t, ctx, owner, third)
		birthLaunchAdmit(t, ctx, runtime, birthLaunchDigest(enrolledCenterBytes), 1, "")
		birthLaunchAdmit(t, ctx, worker, birthLaunchDigest(enrolledProcessorBytes), 1, "")
		birthLaunchAdmit(t, ctx, runtime, replacedDigest, 0, "42501")

		// A later source generation may name new runtime or worker principals
		// before launch registration changes. These are trusted owner-installed
		// fixture rows, not a production recovery or credential ceremony.
		rotated := fixture.openDirectRolePool(t, ctx, fixture.admin)
		for _, tc := range []struct {
			name       string
			generation int64
			runtime    string
			worker     string
		}{
			{"runtime", 2, fixture.admin, fixture.deletionWorker},
			{"worker", 3, fixture.runtime, fixture.admin},
		} {
			t.Run("unregistered_current_"+tc.name, func(t *testing.T) {
				if _, err := owner.Exec(ctx, `insert into public.source_application_principals(principal,generation,purpose)
					values($1,$4,'runtime'),($2,$4,'worker'),($3,$4,'admin')`, tc.runtime, tc.worker, fixture.migrator, tc.generation); err != nil {
					t.Fatalf("install rotated fixture principals: %v", err)
				}
				if _, err := owner.Exec(ctx, `update public.source_authority_admission set generation=$1 where singleton`, tc.generation); err != nil {
					t.Fatalf("activate rotated fixture generation: %v", err)
				}
				birthLaunchAdmit(t, ctx, rotated, "", 0, "42501")
				birthLaunchAdmit(t, ctx, rotated, birthLaunchDigest(enrolledCenterBytes), 0, "42501")
				// The source admin is not a launch participant and remains admitted.
				birthLaunchAdmit(t, ctx, owner, "", tc.generation, "")
				birthLaunchAssertState(t, ctx, owner, fixture, third, true, enrolledCenterBytes, enrolledProcessorBytes)
			})
		}
	})
	t.Run("never_enrolled_manifest_role_change", func(t *testing.T) {
		fixture, owner, runtime, worker, activation := birthLaunchTestFixture(t, ctx)
		admin := fixture.openDirectRolePool(t, ctx, fixture.admin)
		var sourceExists bool
		if err := owner.QueryRow(ctx, `select exists(select 1 from public.source_authority_admission)`).Scan(&sourceExists); err != nil || sourceExists {
			t.Fatalf("never-enrolled source exists=%t, error=%v", sourceExists, err)
		}
		center, processor := birthLaunchFixturePair(activation.DeploymentID, activation.ActivationBundleDigest, nil)
		centerBytes, processorBytes := birthLaunchEncode(t, center), birthLaunchEncode(t, processor)
		centerDigest, processorDigest := birthLaunchDigest(centerBytes), birthLaunchDigest(processorBytes)
		revision := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0)
		birthLaunchOpen(t, ctx, owner, revision)
		birthLaunchAdmit(t, ctx, runtime, centerDigest, 0, "")
		birthLaunchAdmit(t, ctx, worker, processorDigest, 0, "")
		birthLaunchAdmit(t, ctx, admin, "", 0, "")
		var inspectedRevision int64
		var inspectedOpen bool
		var inspectedCenter, inspectedProcessor []byte
		if err := owner.QueryRow(ctx, `select revision,opened,center_binding,processor_binding
			from public.record_birth_launch_inspect(null::bigint,null::bytea)`).Scan(&inspectedRevision, &inspectedOpen, &inspectedCenter, &inspectedProcessor); err != nil || inspectedRevision != revision || !inspectedOpen || !bytes.Equal(inspectedCenter, centerBytes) || !bytes.Equal(inspectedProcessor, processorBytes) {
			t.Fatalf("inspect current launch revision=%d opened=%t error=%v", inspectedRevision, inspectedOpen, err)
		}

		// The fixture swaps two real direct roles, applies the compiled APP ACL,
		// and appends a digest-validated successor with the production writer.
		// This models a manifest transition, not an online rotation ceremony.
		previous, successor := birthLaunchSwapFixtureManifest(t, ctx, owner, fixture)
		var registeredDigest []byte
		if err := owner.QueryRow(ctx, `select manifest_digest from record_platform_internal.record_birth_launch_state where singleton`).Scan(&registeredDigest); err != nil || !bytes.Equal(registeredDigest, previous.ManifestDigest[:]) {
			t.Fatalf("old launch manifest = %x, error=%v", registeredDigest, err)
		}
		birthLaunchAdmit(t, ctx, runtime, centerDigest, 0, "42501")
		birthLaunchAdmit(t, ctx, worker, processorDigest, 0, "42501")
		birthLaunchAdmit(t, ctx, admin, "", 0, "42501")
		birthLaunchAdmit(t, ctx, owner, "", 0, "42501")
		_, err := owner.Exec(ctx, `select * from public.record_birth_launch_inspect(null::bigint,null::bytea)`)
		requirePostgresSQLState(t, err, "42501")
		for _, expected := range []int64{revision - 1, revision} {
			_, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, centerBytes, processorBytes, fixture.runtime, fixture.deletionWorker, expected)
			requirePostgresSQLState(t, err, "40001")
		}
		birthLaunchAssertState(t, ctx, owner, fixture, revision, true, centerBytes, processorBytes)
		birthLaunchClose(t, ctx, owner, revision)
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision)
		requirePostgresSQLState(t, err, "42501")
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, centerBytes, processorBytes, fixture.runtime, fixture.deletionWorker, revision-1)
		requirePostgresSQLState(t, err, "40001")
		next := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, revision)
		if next != revision+1 {
			t.Fatalf("same launch under changed manifest revision=%d, want %d", next, revision+1)
		}
		if err := owner.QueryRow(ctx, `select manifest_digest from record_platform_internal.record_birth_launch_state where singleton`).Scan(&registeredDigest); err != nil || !bytes.Equal(registeredDigest, successor.ManifestDigest[:]) {
			t.Fatalf("restaged launch manifest = %x, error=%v", registeredDigest, err)
		}
		var rotatedRevision int64
		if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, centerBytes, processorBytes, fixture.deletionWorker, fixture.runtime, next).Scan(&rotatedRevision); err != nil || rotatedRevision != next+1 {
			t.Fatalf("stage rotated roles revision=%d, error=%v", rotatedRevision, err)
		}
		var registeredRuntime, registeredWorker string
		if err := owner.QueryRow(ctx, `select runtime_principal,worker_principal,manifest_digest
			from record_platform_internal.record_birth_launch_state where singleton`).Scan(&registeredRuntime, &registeredWorker, &registeredDigest); err != nil || registeredRuntime != fixture.deletionWorker || registeredWorker != fixture.runtime || !bytes.Equal(registeredDigest, successor.ManifestDigest[:]) {
			t.Fatalf("rotated launch principals=%q/%q manifest=%x error=%v", registeredRuntime, registeredWorker, registeredDigest, err)
		}
		birthLaunchOpen(t, ctx, owner, rotatedRevision)
		birthLaunchAdmit(t, ctx, worker, centerDigest, 0, "")
		birthLaunchAdmit(t, ctx, runtime, processorDigest, 0, "")
		birthLaunchAdmit(t, ctx, admin, "", 0, "")
		var replay int64
		if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, centerBytes, processorBytes, fixture.deletionWorker, fixture.runtime, next).Scan(&replay); err != nil || replay != rotatedRevision {
			t.Fatalf("rotated launch replay=%d, error=%v", replay, err)
		}
		if err := owner.QueryRow(ctx, `select revision,opened,center_binding,processor_binding
			from public.record_birth_launch_inspect(null::bigint,null::bytea)`).Scan(&inspectedRevision, &inspectedOpen, &inspectedCenter, &inspectedProcessor); err != nil || inspectedRevision != rotatedRevision || !inspectedOpen || !bytes.Equal(inspectedCenter, centerBytes) || !bytes.Equal(inspectedProcessor, processorBytes) {
			t.Fatalf("inspect rotated launch revision=%d opened=%t error=%v", inspectedRevision, inspectedOpen, err)
		}
	})

	t.Run("installed_lineage_and_live_authority", func(t *testing.T) {
		fixture, owner, runtime, worker, activation := birthLaunchTestFixture(t, ctx)
		binding := recordbirth.Binding{
			AuthorityID: "rd-" + strings.Repeat("a", 64), LineageID: "bl-" + strings.Repeat("b", 64),
			DeploymentID: activation.DeploymentID, ProjectID: "default", Generation: 1,
			KeyID: "bk-" + strings.Repeat("d", 64), CutoverDigest: sha256.Sum256([]byte("launch fixture cutover")),
		}
		key := bytes.Repeat([]byte{0x42}, 32)
		checkpointSeedAuthority(t, ctx, owner, fixture.runtime, fixture.deletionWorker, binding, key)
		center, processor := birthLaunchFixturePair(binding.DeploymentID, activation.ActivationBundleDigest, &binding)
		centerBytes := birthLaunchEncode(t, center)
		processorBytes := birthLaunchEncode(t, processor)
		centerDigest := birthLaunchDigest(centerBytes)
		processorDigest := birthLaunchDigest(processorBytes)
		for _, role := range []struct {
			pool   *pgxpool.Pool
			digest string
		}{{runtime, centerDigest}, {worker, processorDigest}} {
			birthLaunchAdmit(t, ctx, role.pool, "", 0, "42501")
			birthLaunchAdmit(t, ctx, role.pool, role.digest, 0, "42501")
		}

		ordinary := center
		ordinary.BirthInstalled = false
		ordinary.BirthBinding = recordbirth.Binding{}
		ordinaryBytes := birthLaunchEncode(t, ordinary)
		_, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`, ordinaryBytes, processorBytes, fixture.runtime, fixture.deletionWorker)
		requirePostgresSQLState(t, err, "42501")
		wrongWitness := center
		wrongWitness.Witness.DatabaseName = "another_witness"
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`, birthLaunchEncode(t, wrongWitness), processorBytes, fixture.runtime, fixture.deletionWorker)
		requirePostgresSQLState(t, err, "42501")
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`, centerBytes, processorBytes, fixture.admin, fixture.deletionWorker)
		requirePostgresSQLState(t, err, "42501")
		revision := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0)
		birthLaunchAdmit(t, ctx, runtime, centerDigest, 0, "42501")
		birthLaunchOpen(t, ctx, owner, revision)
		birthLaunchAdmit(t, ctx, runtime, centerDigest, 1, "")
		birthLaunchAdmit(t, ctx, worker, processorDigest, 1, "")
		birthLaunchAssertUnprivileged(t, ctx, fixture, owner, runtime, worker, centerBytes, processorBytes, revision)

		// Test-only owner mutations model a changed current authority. Each
		// mismatch must reject both an existing launch and a later open.
		changedActivation := sha256.Sum256([]byte("different activation"))
		birthLaunchAssertDrift(t, ctx, owner, runtime, centerDigest, revision,
			`update public.deployment_contract_state set activation_bundle_digest=$1 where project_id='default'`,
			[]any{changedActivation[:]}, []any{activation.ActivationBundleDigest[:]})
		birthLaunchAssertDrift(t, ctx, owner, runtime, centerDigest, revision,
			`update public.source_authority_admission set witness_database_name=$1 where singleton`,
			[]any{"changed_witness"}, []any{"checkpoint_fixture"})
		birthLaunchAssertDrift(t, ctx, owner, runtime, centerDigest, revision,
			`update public.source_authority_admission set generation=$1 where singleton`,
			[]any{int64(2)}, []any{int64(1)})
		birthLaunchAssertDrift(t, ctx, owner, runtime, centerDigest, revision,
			`update public.source_authority_admission set mode=$1 where singleton`,
			[]any{"closed"}, []any{"active"})

		// Disabling the producer does not turn an installed lineage back into
		// a never-installed launch. Its current binding remains required.
		if _, err := owner.Exec(ctx, `delete from record_platform_internal.record_birth_enabled where singleton`); err != nil {
			t.Fatalf("disable fixture birth producer: %v", err)
		}
		birthLaunchAdmit(t, ctx, runtime, centerDigest, 1, "")
		birthLaunchClose(t, ctx, owner, revision)
		_, err = owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, ordinaryBytes, processorBytes, fixture.runtime, fixture.deletionWorker, revision)
		requirePostgresSQLState(t, err, "42501")
		birthLaunchOpen(t, ctx, owner, revision)
		birthLaunchAdmit(t, ctx, worker, processorDigest, 1, "")
		if _, err := owner.Exec(ctx, `insert into record_platform_internal.record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,$1,$2,$3)`, binding.Generation, binding.KeyID, binding.CutoverDigest[:]); err != nil {
			t.Fatalf("restore fixture birth producer: %v", err)
		}

		otherKey := "bk-" + strings.Repeat("e", 64)
		otherCutover := sha256.Sum256([]byte("different birth binding"))
		otherKeyBytes := bytes.Repeat([]byte{0x43}, 32)
		otherCommitment := sha256.Sum256(otherKeyBytes)
		if _, err := owner.Exec(ctx, `insert into record_platform_internal.record_birth_keys(key_id,key_bytes,key_commitment) values($1,$2,$3)`, otherKey, otherKeyBytes, otherCommitment[:]); err != nil {
			t.Fatalf("seed second fixture birth key: %v", err)
		}
		if _, err := owner.Exec(ctx, `insert into record_platform_internal.record_birth_bindings(generation,key_id,cutover_digest,authority_id,lineage_id,deployment_id,project_id) values($1,$2,$3,$4,$5,$6,$7)`, binding.Generation, otherKey, otherCutover[:], binding.AuthorityID, binding.LineageID, binding.DeploymentID, binding.ProjectID); err != nil {
			t.Fatalf("seed second fixture birth binding: %v", err)
		}
		birthLaunchAssertDrift(t, ctx, owner, runtime, centerDigest, revision,
			`update record_platform_internal.record_birth_enabled set key_id=$1,cutover_digest=$2 where singleton`,
			[]any{otherKey, otherCutover[:]}, []any{binding.KeyID, binding.CutoverDigest[:]})
		birthLaunchAssertWriterDrain(t, ctx, fixture.db, owner, runtime, centerDigest, revision, binding, key)
	})
}

// A valid manifest successor and matching ACL are installed by the existing
// APP compiler and successor writer. It exercises launch invalidation; it is
// not evidence that role rotation is an available online deployment protocol.
func birthLaunchSwapFixtureManifest(t *testing.T, ctx context.Context, owner *pgxpool.Pool, fixture appACLConvergencePostgresFixture) (AppACLManifestPersistedV1, AppACLManifestPersistedV1) {
	t.Helper()
	source, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile fixture APP source: %v", err)
	}
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: fixture.deletionWorker},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: fixture.admin},
		{Subject: AppACLSubjectRecordDeletionWorker, CatalogRole: fixture.runtime},
	}
	contract, err := compileAppACLCurrentCatalogContract(source, fixture.databaseName, bindings, fixture.migrator)
	if err != nil {
		t.Fatalf("compile rotated APP role contract: %v", err)
	}
	privileges, err := canonicalPrivilegeSetBodyForBindings(contract.RoleBindings, contract.Privileges)
	if err != nil {
		t.Fatalf("encode rotated APP role contract: %v", err)
	}
	tx, err := owner.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	manifests, err := readAppACLManifestRevisionsV1(ctx, tx)
	if err != nil || len(manifests) == 0 {
		t.Fatalf("read APP manifest chain: count=%d error=%v", len(manifests), err)
	}
	head, err := readAppACLManifestHeadV1(ctx, tx)
	if err != nil || head == nil {
		t.Fatalf("read APP manifest head: %v", err)
	}
	if err := ValidateAppACLManifestChainV1(manifests, *head); err != nil {
		t.Fatalf("validate APP manifest predecessor: %v", err)
	}
	previous := manifests[len(manifests)-1]
	oldSet, err := ParseCanonicalPrivilegeSetBodyV2(previous.CanonicalPrivilegeSet)
	if err != nil {
		t.Fatalf("parse predecessor role bindings: %v", err)
	}
	wantOld := map[AppACLSubject]string{
		AppACLSubjectCenterRuntime: fixture.runtime, AppACLSubjectPlatformAdmin: fixture.admin,
		AppACLSubjectRecordDeletionWorker: fixture.deletionWorker,
	}
	for _, binding := range oldSet.RoleBindings {
		if binding.CatalogRole != wantOld[binding.Subject] {
			t.Fatalf("unexpected predecessor role binding %+v", binding)
		}
	}
	if !bytes.Equal(previous.CanonicalMigrationSet, source.sources.canonicalSet) || bytes.Equal(previous.CanonicalPrivilegeSet, privileges) {
		t.Fatal("fixture successor did not preserve migrations and change role bindings")
	}
	if err := applyAppACLConvergenceDCLForContractInTx(ctx, tx, contract); err != nil {
		t.Fatalf("apply rotated fixture role ACL: %v", err)
	}
	input, err := newAppACLEffectiveCatalogVerifierInput(contract, fixture.migrator)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input)
	if err != nil {
		t.Fatalf("read rotated fixture catalog: %v", err)
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(catalog, input); err != nil {
		t.Fatalf("verify rotated fixture catalog: %v", err)
	}
	successor, err := insertAppACLManifestSuccessorV1(ctx, tx, previous, source.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatalf("write rotated APP manifest successor: %v", err)
	}
	if successor.ManifestRevision != previous.ManifestRevision+1 || successor.PreviousManifestDigest != previous.ManifestDigest || successor.ManifestDigest == previous.ManifestDigest {
		t.Fatal("rotated APP manifest did not advance the original chain")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rotated fixture manifest: %v", err)
	}
	return previous, successor
}

func birthLaunchTestFixture(t *testing.T, ctx context.Context) (appACLConvergencePostgresFixture, *pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool, launchActivationFixture) {
	t.Helper()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtime, fixture.admin, fixture.deletionWorker); err != nil {
		t.Fatalf("converge APP: %v", err)
	}
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker)
	// The projector is the real APP activation entrypoint; arbitrary launch
	// bytes cannot be staged against the initial unactivated singleton.
	deployment := "dp-" + strings.Repeat("a", 64)
	center, processor := birthLaunchFixturePair(deployment, sha256.Sum256([]byte("unactivated fixture")), nil)
	_, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`, birthLaunchEncode(t, center), birthLaunchEncode(t, processor), fixture.runtime, fixture.deletionWorker)
	requirePostgresSQLState(t, err, "42501")
	activation := birthLaunchActivateFixture(t, ctx, owner, deployment)
	return fixture, owner, runtime, worker, activation
}

type launchActivationFixture struct {
	DeploymentID           string
	ActivationBundleDigest [32]byte
}

func birthLaunchActivateFixture(t *testing.T, ctx context.Context, owner *pgxpool.Pool, deployment string) launchActivationFixture {
	t.Helper()
	activation := projectorCASActivationCommand()
	activation.DeploymentID = deployment
	command, err := activation.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal fixture activation: %v", err)
	}
	var receipt []byte
	if err := owner.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, command).Scan(&receipt); err != nil {
		t.Fatalf("install fixture activation through projector: %v", err)
	}
	assertProjectorCASReceipt(t, command, receipt)
	return launchActivationFixture{DeploymentID: deployment, ActivationBundleDigest: activation.ActivationBundleDigest}
}

func birthLaunchFixturePair(deployment string, activation [32]byte, binding *recordbirth.Binding) (recordbirth.LaunchBinding, recordbirth.LaunchBinding) {
	center := recordbirth.LaunchBinding{
		ProtocolVersion: 1, ProcessKind: "center", DeploymentID: deployment,
		ActivationBundleDigest: activation, BinaryDigest: sha256.Sum256([]byte("fixture center executable")),
	}
	if binding != nil {
		center.SourceGeneration = binding.Generation
		center.BirthInstalled = true
		center.BirthBinding = *binding
		center.Witness = sourceretirement.DomainIdentity{
			ID: binding.AuthorityID, SystemIdentifier: "1", DatabaseOID: 1,
			DatabaseName: "checkpoint_fixture", Epoch: 1,
		}
	}
	processor := center
	processor.ProcessKind = "content-processor"
	processor.BinaryDigest = sha256.Sum256([]byte("fixture processor executable"))
	return center, processor
}

func birthLaunchEncode(t *testing.T, binding recordbirth.LaunchBinding) []byte {
	t.Helper()
	raw, err := recordbirth.EncodeLaunchBinding(binding)
	if err != nil {
		t.Fatalf("encode fixture launch binding: %v", err)
	}
	return raw
}

func birthLaunchDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func birthLaunchStage(t *testing.T, ctx context.Context, owner *pgxpool.Pool, fixture appACLConvergencePostgresFixture, center, processor []byte, expected int64) int64 {
	t.Helper()
	var revision int64
	if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, center, processor, fixture.runtime, fixture.deletionWorker, expected).Scan(&revision); err != nil {
		t.Fatalf("stage fixture launch at revision %d: %v", expected, err)
	}
	return revision
}

func birthLaunchOpen(t *testing.T, ctx context.Context, owner *pgxpool.Pool, revision int64) {
	t.Helper()
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatalf("open launch revision %d: %v", revision, err)
	}
}

func birthLaunchClose(t *testing.T, ctx context.Context, owner *pgxpool.Pool, revision int64) {
	t.Helper()
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision); err != nil {
		t.Fatalf("close launch revision %d: %v", revision, err)
	}
}

func birthLaunchAssertState(t *testing.T, ctx context.Context, owner *pgxpool.Pool, fixture appACLConvergencePostgresFixture, wantRevision int64, wantOpen bool, wantCenter, wantProcessor []byte) {
	t.Helper()
	var revision int64
	var opened bool
	var runtime, worker string
	var center, processor []byte
	if err := owner.QueryRow(ctx, `select revision,opened,runtime_principal,worker_principal,center_binding,processor_binding from record_platform_internal.record_birth_launch_state where singleton`).Scan(&revision, &opened, &runtime, &worker, &center, &processor); err != nil {
		t.Fatalf("read fixture launch state: %v", err)
	}
	if revision != wantRevision || opened != wantOpen || runtime != fixture.runtime || worker != fixture.deletionWorker || !bytes.Equal(center, wantCenter) || !bytes.Equal(processor, wantProcessor) {
		t.Fatalf("launch state revision=%d opened=%t principals=%q/%q center=%q processor=%q, want revision=%d opened=%t principals=%q/%q center=%q processor=%q", revision, opened, runtime, worker, center, processor, wantRevision, wantOpen, fixture.runtime, fixture.deletionWorker, wantCenter, wantProcessor)
	}
}

func birthLaunchAdmit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, digest string, wantGeneration int64, wantState string) {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if digest != "" {
		if _, err := tx.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, digest); err != nil {
			t.Fatal(err)
		}
	}
	var generation int64
	err = tx.QueryRow(ctx, `select public.source_application_admit()`).Scan(&generation)
	if wantState != "" {
		requirePostgresSQLState(t, err, wantState)
		return
	}
	if err != nil || generation != wantGeneration {
		t.Fatalf("launch admission generation = %d, error = %v, want %d", generation, err, wantGeneration)
	}
}

func birthLaunchPoolWithDigest(t *testing.T, ctx context.Context, existing *pgxpool.Pool, raw []byte) *pgxpool.Pool {
	t.Helper()
	config := existing.Config().Copy()
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["houfeng.birth_launch_digest"] = birthLaunchDigest(raw)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open launch-bound fixture pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func birthLaunchAssertUnprivileged(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture, owner, runtime, worker *pgxpool.Pool, center, processor []byte, revision int64) {
	t.Helper()
	for _, role := range []struct {
		name      string
		principal string
		pool      *pgxpool.Pool
	}{{"runtime", fixture.runtime, runtime}, {"worker", fixture.deletionWorker, worker}} {
		for _, identity := range []string{
			"record_platform_internal.record_birth_launch_stage(bytea,bytea,name,name,bigint)",
			"record_platform_internal.record_birth_launch_close(bigint)",
			"record_platform_internal.record_birth_launch_open(bigint)",
		} {
			var allowed bool
			if err := owner.QueryRow(ctx, `select pg_catalog.has_function_privilege($1::name,$2::text,'EXECUTE')`, role.principal, identity).Scan(&allowed); err != nil || allowed {
				t.Fatalf("%s execute privilege on %s = %t, error = %v", role.name, identity, allowed, err)
			}
		}
		for _, privilege := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			var allowed bool
			if err := owner.QueryRow(ctx, `select pg_catalog.has_table_privilege($1::name,'record_platform_internal.record_birth_launch_state',$2::text)`, role.principal, privilege).Scan(&allowed); err != nil || allowed {
				t.Fatalf("%s %s privilege on launch state = %t, error = %v", role.name, privilege, allowed, err)
			}
		}
		for _, operation := range []struct {
			name string
			sql  string
			args []any
		}{
			{"stage", `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, []any{center, processor, fixture.runtime, fixture.deletionWorker, revision}},
			{"close", `select record_platform_internal.record_birth_launch_close($1::bigint)`, []any{revision}},
			{"open", `select record_platform_internal.record_birth_launch_open($1::bigint)`, []any{revision}},
			{"insert", `insert into record_platform_internal.record_birth_launch_state(revision,opened,runtime_principal,worker_principal,center_binding,processor_binding) values(2,false,'runtime','worker',$1,$2)`, []any{center, processor}},
			{"update", `update record_platform_internal.record_birth_launch_state set opened=false`, nil},
			{"delete", `delete from record_platform_internal.record_birth_launch_state`, nil},
			{"truncate", `truncate record_platform_internal.record_birth_launch_state`, nil},
		} {
			if _, err := role.pool.Exec(ctx, operation.sql, operation.args...); err == nil {
				t.Fatalf("%s could %s launch state", role.name, operation.name)
			} else {
				requirePostgresSQLState(t, err, "42501")
			}
		}
	}
	birthLaunchAssertState(t, ctx, owner, fixture, revision, true, center, processor)
}

func birthLaunchAssertDrift(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, digest string, revision int64, update string, changed, original []any) {
	t.Helper()
	if _, err := owner.Exec(ctx, update, changed...); err != nil {
		t.Fatalf("change fixture authority: %v", err)
	}
	birthLaunchAdmit(t, ctx, runtime, digest, 0, "42501")
	birthLaunchClose(t, ctx, owner, revision)
	_, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision)
	requirePostgresSQLState(t, err, "42501")
	if _, err := owner.Exec(ctx, update, original...); err != nil {
		t.Fatalf("restore fixture authority: %v", err)
	}
	birthLaunchOpen(t, ctx, owner, revision)
	birthLaunchAdmit(t, ctx, runtime, digest, 1, "")
}

func birthLaunchAssertWriterDrain(t *testing.T, parent context.Context, observer, owner, runtime *pgxpool.Pool, digest string, revision int64, binding recordbirth.Binding, key []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	writer, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(context.Background()) }()
	if _, err := writer.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, digest); err != nil {
		t.Fatal(err)
	}
	proof := checkpointFixtureContinuity(t, binding, key, recordbirth.CheckpointHead{}, true)
	request := checkpointRequest("launch-in-flight-writer")
	allocation := checkpointBegin(t, ctx, writer, checkpointBeginCommand(request, nil, proof))
	if allocation.Domain != "ordinary" || allocation.State != "allocated" {
		t.Fatalf("admitted writer allocation = %+v", allocation)
	}
	writerPID := postgresTransactionBackendPID(t, ctx, writer)
	for _, statement := range []string{
		`select 1 from public.source_authority_admission where singleton for update nowait`,
		`select 1 from record_platform_internal.record_birth_launch_state where singleton for update nowait`,
	} {
		_, err := owner.Exec(ctx, statement)
		requirePostgresSQLState(t, err, "55P03")
	}
	closing, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = closing.Rollback(context.Background())
	}()
	closingPID := postgresTransactionBackendPID(t, ctx, closing)
	result := make(chan error, 1)
	go func() {
		_, err := closing.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, revision)
		result <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := observer.QueryRow(ctx, `select $1::int=any(pg_catalog.pg_blocking_pids($2::int))`, writerPID, closingPID).Scan(&blocked); err != nil {
			t.Fatalf("observe launch close waiting: %v", err)
		}
		if blocked {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("launch close returned before writer finished: %v", err)
		case <-ctx.Done():
			t.Fatalf("launch close did not wait for admitted writer: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit admitted in-flight writer: %v", err)
	}
	if err := awaitPostgresConflictResult(t, ctx, result); err != nil {
		t.Fatalf("close after writer drained: %v", err)
	}
	if err := closing.Commit(ctx); err != nil {
		t.Fatalf("commit launch close: %v", err)
	}
	var count int
	if err := observer.QueryRow(ctx, `select count(*)::int from record_platform_internal.record_birth_allocations where allocation_id=$1`, allocation.AllocationID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("in-flight allocation count = %d, error = %v", count, err)
	}
	birthLaunchAdmit(t, ctx, runtime, digest, 0, "42501")
	rejected, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rejected.Rollback(context.Background()) }()
	if _, err := rejected.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, digest); err != nil {
		t.Fatal(err)
	}
	blockedRequest := checkpointRequest("launch-after-close")
	_, err = rejected.Exec(ctx, `select public.record_birth_allocation_begin($1::bytea)`, checkpointJSON(t, checkpointBeginCommand(blockedRequest, nil, proof)))
	requirePostgresSQLState(t, err, "42501")
	if err := rejected.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := observer.QueryRow(ctx, `select count(*)::int from record_platform_internal.record_birth_allocations where idempotency_key=$1`, blockedRequest.IdempotencyKey).Scan(&count); err != nil || count != 0 {
		t.Fatalf("closed launch allocation count = %d, error = %v", count, err)
	}
}

// These tests seed predecessor data before C81. They exercise direct runtime
// mutations against an actual existing root and do not attest the deployment
// ceremony or the provenance of the fixture's comparison history.
func TestPostgresIntegrationRecordBirthLaunchExistingRootMutations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP80Migration)
	exact := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	owner := exact.openRolePool(t, ctx, exact.migratorRole)
	seedAppACLCurrentReleasedGenesis(t, ctx, exact, owner, profile)
	seedPostgresRecordMutationRows(t, ctx, owner)
	comparisonBody, comparisonDigest := birthLaunchSeedMutationPredecessor(t, ctx, owner)
	draftMutations := birthLaunchSeedDraftMutations(t, ctx, owner)
	if _, err := ConvergeAppACLCurrent(ctx, owner, exact.runtimeRole, exact.adminRole, exact.deletionWorkerRole); err != nil {
		t.Fatal(err)
	}
	fixture := exact.asConvergenceFixture()
	runtime := exact.openRolePool(t, ctx, exact.runtimeRole)
	if _, err := owner.Exec(ctx, `insert into public.comparison_result_dependencies
		(result_snapshot_id,source_record_id,source_revision_id,source_snapshot_id) values
		('evs_launchparent','rec_rowlock',null,'evs_rowlock'),
		('evs_launchparent','rec_rowlock','rrv_rowlock',null)`); err != nil {
		t.Fatalf("seed predecessor comparison dependencies: %v", err)
	}
	if _, err := owner.Exec(ctx, `insert into public.comparison_result_dependency_index_completions
		(result_snapshot_id,payload_digest) values ('evs_launchparent',$1)`, comparisonDigest[:]); err != nil {
		t.Fatalf("seed predecessor comparison completion: %v", err)
	}

	deployment := "dp-" + strings.Repeat("a", 64)
	activation := birthLaunchActivateFixture(t, ctx, owner, deployment)
	binding := recordbirth.Binding{
		AuthorityID: "rd-" + strings.Repeat("a", 64), LineageID: "bl-" + strings.Repeat("b", 64),
		DeploymentID: deployment, ProjectID: "default", Generation: 1,
		KeyID: "bk-" + strings.Repeat("d", 64), CutoverDigest: sha256.Sum256([]byte("mutation fixture cutover")),
	}
	checkpointSeedAuthority(t, ctx, owner, fixture.runtime, fixture.deletionWorker, binding, bytes.Repeat([]byte{0x42}, 32))
	center, processor := birthLaunchFixturePair(deployment, activation.ActivationBundleDigest, &binding)
	centerBytes, processorBytes := birthLaunchEncode(t, center), birthLaunchEncode(t, processor)
	digest := birthLaunchDigest(centerBytes)
	revision := birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, 0)
	birthLaunchOpen(t, ctx, owner, revision)

	comparisonCommand := birthLaunchMutationJSON(t, map[string]any{
		"record_id": "rec_rowlock", "snapshot_id": "evs_launchchild", "payload_digest": hex.EncodeToString(comparisonDigest[:]),
		"inherited_from": "evs_launchparent", "dependencies": []map[string]string{
			{"record_id": "rec_rowlock", "revision_id": "", "snapshot_id": "evs_rowlock"},
			{"record_id": "rec_rowlock", "revision_id": "rrv_rowlock", "snapshot_id": ""},
		},
	})
	outboxCommand := birthLaunchMutationJSON(t, map[string]any{
		"project_id": "default", "event_kind": "record_updated", "subject_kind": "record", "subject_id": "rec_rowlock",
		"source_version": 0, "authorization_epoch": 1, "record_fence_epoch": 0, "record_id": "rec_rowlock", "expires_after_us": int64(time.Hour / time.Microsecond),
	})
	mutations := []birthLaunchMutation{
		{name: "record_update", sql: `update public.records set current_title='launch mutation' where record_id='rec_rowlock'`,
			check: `select count(*)::int from public.records where record_id='rec_rowlock' and current_title='launch mutation'`, want: 1},
		{name: "evidence_intent", sql: `select public.record_evidence_intent_insert($1::bytea)`, args: []any{birthLaunchEvidenceIntent(t)},
			check: `select count(*)::int from public.evidence_capture_intents where intent_id='evi_111111111111111111111111' and record_id='rec_rowlock'`, want: 1},
		{name: "comparison_index", sql: `select public.record_comparison_index_write($1::bytea,$2::bytea)`, args: []any{comparisonCommand, comparisonBody},
			check: `select count(*)::int from public.comparison_result_dependencies d join public.comparison_result_dependency_index_completions c using(result_snapshot_id) where d.result_snapshot_id='evs_launchchild'`, want: 2},
		{name: "outbox_enqueue", sql: `select * from public.record_outbox_enqueue($1::bytea)`, args: []any{outboxCommand},
			check: `select count(*)::int from public.record_outbox where record_id='rec_rowlock' and event_kind='record_updated'`, want: 1},
		{name: "import_plan_lock", sql: `select * from public.record_import_plan_lock($1::bytea)`, args: []any{[]byte(`{"import_job_id":"rij_launch","namespace_ids":""}`)}, importLock: true},
	}
	mutations = append(mutations, draftMutations...)

	// Validate every command, including its owner, payload and route, before
	// treating a later 42501 as evidence about the launch guard.
	for _, mutation := range mutations {
		t.Run("healthy_before/"+mutation.name, func(t *testing.T) {
			birthLaunchExerciseMutation(t, ctx, owner, runtime, digest, mutation, false, false)
		})
	}
	assertRejected := func(name, claim string) {
		t.Helper()
		for _, mutation := range mutations {
			t.Run(name+"/"+mutation.name, func(t *testing.T) {
				birthLaunchExerciseMutation(t, ctx, owner, runtime, claim, mutation, true, false)
			})
		}
	}
	assertRejected("missing_digest", "")
	assertRejected("stale_digest", strings.Repeat("0", 64))
	birthLaunchClose(t, ctx, owner, revision)
	assertRejected("closed_registration", digest)
	birthLaunchAssertClosedDraftCleanupScope(t, ctx, owner, runtime, digest)
	birthLaunchOpen(t, ctx, owner, revision)

	changedActivation := sha256.Sum256([]byte("different mutation fixture activation"))
	if _, err := owner.Exec(ctx, `update public.deployment_contract_state set activation_bundle_digest=$1 where project_id='default'`, changedActivation[:]); err != nil {
		t.Fatalf("change activation fixture: %v", err)
	}
	assertRejected("activation_drift", digest)
	if _, err := owner.Exec(ctx, `update public.deployment_contract_state set activation_bundle_digest=$1 where project_id='default'`, activation.ActivationBundleDigest[:]); err != nil {
		t.Fatalf("restore activation fixture: %v", err)
	}

	// Install a genuine, digest-validated manifest successor with the same ACL.
	// The runtime retains its privileges, isolating rejection to the stale launch.
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := readAppACLManifestRevisionsV1(ctx, tx)
	if err != nil || len(manifests) == 0 {
		_ = tx.Rollback(ctx)
		t.Fatalf("read manifest for fixture successor: count=%d error=%v", len(manifests), err)
	}
	previous := manifests[len(manifests)-1]
	if _, err := insertAppACLManifestSuccessorV1(ctx, tx, previous, previous.CanonicalMigrationSet, previous.CanonicalPrivilegeSet); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("append fixture manifest successor: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit fixture manifest successor: %v", err)
	}
	assertRejected("manifest_drift", digest)
	birthLaunchClose(t, ctx, owner, revision)
	revision = birthLaunchStage(t, ctx, owner, fixture, centerBytes, processorBytes, revision)
	birthLaunchOpen(t, ctx, owner, revision)

	for _, lock := range []struct{ name, sql string }{
		{"row", `select revision from record_platform_internal.record_birth_launch_state where singleton for update`},
		{"table", `lock table record_platform_internal.record_birth_launch_state in exclusive mode`},
	} {
		t.Run("launch_lock_"+lock.name, func(t *testing.T) {
			birthLaunchAssertMutationLockRetry(t, ctx, owner, runtime, digest, lock.sql, mutations[0])
		})
	}
	for _, mutation := range mutations {
		t.Run("healthy_commit/"+mutation.name, func(t *testing.T) {
			birthLaunchExerciseMutation(t, ctx, owner, runtime, digest, mutation, false, true)
		})
	}
}

type birthLaunchMutation struct {
	name       string
	sql        string
	args       []any
	check      string
	want       int
	importLock bool
}

func birthLaunchExerciseMutation(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, digest string, mutation birthLaunchMutation, reject, commit bool) {
	t.Helper()
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `set local statement_timeout='2s'`); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, mutation.sql, mutation.args...)
	if reject {
		requirePostgresSQLState(t, err, "42501")
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if mutation.importLock {
			var id string
			if err := owner.QueryRow(ctx, `select import_job_id from public.record_import_jobs where import_job_id='rij_launch' for update nowait`).Scan(&id); err != nil || id != "rij_launch" {
				t.Fatalf("%s left its job unavailable: id=%q error=%v", mutation.name, id, err)
			}
		} else {
			var got int
			if err := owner.QueryRow(ctx, mutation.check).Scan(&got); err != nil || got != 0 {
				t.Fatalf("%s rejected launch left %d effects, error=%v", mutation.name, got, err)
			}
		}
		return
	}
	if err != nil {
		t.Fatalf("%s with current launch: %v", mutation.name, err)
	}
	if mutation.importLock {
		var id string
		err := owner.QueryRow(ctx, `select import_job_id from public.record_import_jobs where import_job_id='rij_launch' for update nowait`).Scan(&id)
		requirePostgresSQLState(t, err, "55P03")
	} else {
		var got int
		if err := tx.QueryRow(ctx, mutation.check).Scan(&got); err != nil || got != mutation.want {
			t.Fatalf("%s visible effect = %d, error = %v, want %d", mutation.name, got, err, mutation.want)
		}
	}
	if commit {
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit %s: %v", mutation.name, err)
		}
		if !mutation.importLock {
			var got int
			if err := owner.QueryRow(ctx, mutation.check).Scan(&got); err != nil || got != mutation.want {
				t.Fatalf("%s committed effect = %d, error = %v, want %d", mutation.name, got, err, mutation.want)
			}
		}
	}
}

func birthLaunchAssertMutationLockRetry(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, digest, lockSQL string, mutation birthLaunchMutation) {
	t.Helper()
	holder, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if _, err := holder.Exec(ctx, lockSQL); err != nil {
		t.Fatalf("hold launch lock: %v", err)
	}
	writer, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(context.Background()) }()
	if _, err := writer.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(ctx, `set local statement_timeout='2s'`); err != nil {
		t.Fatal(err)
	}
	_, err = writer.Exec(ctx, mutation.sql, mutation.args...)
	requirePostgresSQLState(t, err, "40001")
	if err := writer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	birthLaunchExerciseMutation(t, ctx, owner, runtime, digest, mutation, false, false)
}

func birthLaunchMutationJSON(t *testing.T, value any) []byte {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func birthLaunchEvidenceIntent(t *testing.T) []byte {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	selection := evidence.Selection{Key: evidence.MonitoringHostV1Key(), SourceType: "vps", SourceID: "vps_aaaaaaaaaaaaaaaa"}
	preview := evidence.Preview{
		IntentID: "evi_111111111111111111111111", Key: selection.Key, Selection: selection,
		EstimatedCanonicalBytes: 1024, SourceDigest: sha256.Sum256([]byte("launch evidence source")),
		PreviewedAt: now, ValidUntil: now.Add(evidence.CaptureIntentTTL),
	}
	selectionBytes := birthLaunchMutationJSON(t, selection)
	previewBytes := birthLaunchMutationJSON(t, preview)
	var framed []byte
	framed = binary.BigEndian.AppendUint64(framed, uint64(len("houfeng.evidence.capture-preview.v1")))
	framed = append(framed, "houfeng.evidence.capture-preview.v1"...)
	framed = binary.BigEndian.AppendUint64(framed, uint64(len(previewBytes)))
	framed = append(framed, previewBytes...)
	previewDigest := sha256.Sum256(framed)
	actorDigest := actor.CanonicalHash()
	metadata := birthLaunchMutationJSON(t, map[string]string{
		"actor_id": actor.UserID, "actor_scope_digest": hex.EncodeToString(actorDigest[:]), "allocation_id": "",
		"record_id": "rec_rowlock", "snapshot_id": "evs_launchintent", "intent_id": preview.IntentID,
		"kind": string(preview.Key.Kind), "schema_version": "1", "preview_digest": hex.EncodeToString(previewDigest[:]),
		"source_digest": hex.EncodeToString(preview.SourceDigest[:]), "estimated_size_bytes": "1024",
		"created_at": now.Format(time.RFC3339Nano), "valid_until": preview.ValidUntil.Format(time.RFC3339Nano),
	})
	command := binary.BigEndian.AppendUint32(nil, uint32(len(metadata)))
	command = append(command, metadata...)
	command = binary.BigEndian.AppendUint32(command, uint32(len(selectionBytes)))
	command = append(command, selectionBytes...)
	return append(command, previewBytes...)
}

func birthLaunchSeedMutationPredecessor(t *testing.T, ctx context.Context, owner *pgxpool.Pool) ([]byte, [32]byte) {
	t.Helper()
	if _, err := owner.Exec(ctx, `insert into public.users(user_id,username,password_hash,display_name,role)
		values('usr_aaaaaaaaaaaaaaaaaaaaaaaa','launch-actor','fixture','Launch actor','admin')`); err != nil {
		t.Fatalf("seed launch evidence actor: %v", err)
	}
	if _, err := owner.Exec(ctx, `insert into public.vps_assets(vps_id,display_name,lifecycle_status) values('vps_aaaaaaaaaaaaaaaa','Launch source','active')`); err != nil {
		t.Fatalf("seed launch evidence source: %v", err)
	}
	if _, err := owner.Exec(ctx, `insert into public.record_import_jobs
		(import_job_id,actor_id,idempotency_key,job_state,identity_classification,archive_digest,expires_at)
		values('rij_launch','usr_rowlock','launch','uploaded','unknown',decode(repeat('31',32),'hex'),clock_timestamp()+interval '1 hour')`); err != nil {
		t.Fatalf("seed launch import job: %v", err)
	}
	canonical := []byte(`{"canonicalization_version":1,"kind":"comparison.result","payload":{"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","items":[{"original_snapshot_id":"evs_rowlock","record_id":"rec_rowlock"},{"record_id":"rec_rowlock","revision_id":"rrv_rowlock"}],"version":"comparison_result/v1"},"schema_version":1}`)
	digest := sha256.Sum256(append([]byte("houfeng.evidence.canonical-payload.v1\x00"), canonical...))
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	if _, err := zipper.Write(canonical); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `insert into public.evidence_payloads(payload_digest,canonical_size_bytes,compressed_size_bytes,compressed_payload)
		values($1,$2,$3,$4)`, digest[:], len(canonical), compressed.Len(), compressed.Bytes()); err != nil {
		t.Fatalf("seed launch comparison payload: %v", err)
	}
	for _, id := range []string{"evs_launchparent", "evs_launchchild"} {
		if _, err := owner.Exec(ctx, `insert into public.evidence_snapshots
			select (jsonb_populate_record(null::public.evidence_snapshots,to_jsonb(s)||jsonb_build_object(
				'snapshot_id',$1::text,'kind','comparison.result','schema_version',1,
				'canonical_hash','\x'||$2::text,'payload_digest','\x'||$2::text,'logical_size_bytes',$3::bigint))).*
			from public.evidence_snapshots s where s.snapshot_id='evs_rowlock'`, id, hex.EncodeToString(digest[:]), len(canonical)); err != nil {
			t.Fatalf("seed launch comparison snapshot %s: %v", id, err)
		}
	}
	if _, err := owner.Exec(ctx, `insert into public.evidence_copy_lineage(snapshot_id,copied_from_snapshot_id,copy_reason)
		values('evs_launchchild','evs_launchparent','explicit comparison copy')`); err != nil {
		t.Fatalf("seed launch comparison lineage: %v", err)
	}
	return canonical, digest
}

// The owner installs only predecessor rows; the operations below use the
// current direct runtime LOGIN and production draft payload/ETag encodings.
func birthLaunchSeedDraftMutations(t *testing.T, ctx context.Context, owner *pgxpool.Pool) []birthLaunchMutation {
	t.Helper()
	const actor = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	original, err := records.NewDraftPayload([]byte(`{"title":"Before launch"}`))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := records.NewDraftPayload([]byte(`{"title":"After launch"}`))
	if err != nil {
		t.Fatal(err)
	}
	makeETag := func(id string, version uint64, payload records.DraftPayload) [32]byte {
		t.Helper()
		etag, err := records.NewDraftETag(id, actor, version, payload)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := etag.Digest()
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	originalHash, updatedHash := original.Hash(), updated.Hash()
	for _, id := range []string{"rdf_launchpatch", "rdf_launchcheckpointinsert", "rdf_launchcheckpointdelete", "rdf_launchdraftdelete"} {
		etag := makeETag(id, 1, original)
		if _, err := owner.Exec(ctx, `insert into public.record_drafts
			(draft_id,author_id,payload,payload_hash,draft_version,etag_digest,warning_at,expires_at)
			values($1,$2,$3::jsonb,$4,1,$5,transaction_timestamp()+interval '83 days',transaction_timestamp()+interval '90 days')`,
			id, actor, string(original.JSON()), originalHash[:], etag[:]); err != nil {
			t.Fatalf("seed standalone draft %s: %v", id, err)
		}
	}
	boundETag := makeETag("rdf_launchbound", 1, original)
	if _, err := owner.Exec(ctx, `insert into public.record_drafts
		(draft_id,record_id,base_revision_id,author_id,payload,payload_hash,draft_version,etag_digest,warning_at,expires_at)
		values('rdf_launchbound','rec_rowlock','rrv_rowlock',$1,$2::jsonb,$3,1,$4,transaction_timestamp()+interval '83 days',transaction_timestamp()+interval '90 days')`,
		actor, string(original.JSON()), originalHash[:], boundETag[:]); err != nil {
		t.Fatalf("seed record-bound draft: %v", err)
	}
	if _, err := owner.Exec(ctx, `insert into public.record_draft_checkpoints
		(checkpoint_id,draft_id,checkpoint_bucket,checkpoint_payload,checkpoint_payload_hash,checkpoint_draft_version,created_at,checkpoint_expires_at)
		values('rdc_launchbound','rdf_launchbound',transaction_timestamp(),$1::jsonb,$2,1,transaction_timestamp(),transaction_timestamp()+interval '7 days')`,
		string(original.JSON()), originalHash[:]); err != nil {
		t.Fatalf("seed record-bound draft checkpoint: %v", err)
	}
	if _, err := owner.Exec(ctx, `insert into public.record_draft_checkpoints
		(checkpoint_id,draft_id,checkpoint_bucket,checkpoint_payload,checkpoint_payload_hash,checkpoint_draft_version,created_at,checkpoint_expires_at)
		values('rdc_launchdelete','rdf_launchcheckpointdelete',transaction_timestamp()-interval '8 days',
		$1::jsonb,$2,1,transaction_timestamp()-interval '8 days',transaction_timestamp()-interval '1 day')`,
		string(original.JSON()), originalHash[:]); err != nil {
		t.Fatalf("seed standalone draft checkpoint: %v", err)
	}
	createETag := makeETag("rdf_launchcreate", 1, original)
	oldETag := makeETag("rdf_launchpatch", 1, original)
	newETag := makeETag("rdf_launchpatch", 2, updated)
	return []birthLaunchMutation{
		{name: "draft_insert", sql: `insert into public.record_drafts
			(draft_id,author_id,payload,payload_hash,draft_version,etag_digest,warning_at,expires_at)
			values('rdf_launchcreate',$1,$2::jsonb,$3,1,$4,transaction_timestamp()+interval '83 days',transaction_timestamp()+interval '90 days')`,
			args:  []any{actor, string(original.JSON()), originalHash[:], createETag[:]},
			check: `select count(*)::int from public.record_drafts where draft_id='rdf_launchcreate' and record_id is null and author_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa'`, want: 1},
		{name: "draft_update", sql: `update public.record_drafts set payload=$1::jsonb,payload_hash=$2,draft_version=2,etag_digest=$3,
			updated_at=transaction_timestamp(),warning_at=transaction_timestamp()+interval '83 days',expires_at=transaction_timestamp()+interval '90 days'
			where draft_id='rdf_launchpatch' and author_id=$4 and etag_digest=$5`,
			args:  []any{string(updated.JSON()), updatedHash[:], newETag[:], actor, oldETag[:]},
			check: `select count(*)::int from public.record_drafts where draft_id='rdf_launchpatch' and record_id is null and draft_version=2 and payload->>'title'='After launch'`, want: 1},
		{name: "draft_checkpoint_insert", sql: `insert into public.record_draft_checkpoints
			(checkpoint_id,draft_id,checkpoint_bucket,checkpoint_payload,checkpoint_payload_hash,checkpoint_draft_version,created_at,checkpoint_expires_at)
			values('rdc_launchinsert','rdf_launchcheckpointinsert',transaction_timestamp(),$1::jsonb,$2,1,transaction_timestamp(),transaction_timestamp()+interval '7 days')`,
			args:  []any{string(original.JSON()), originalHash[:]},
			check: `select count(*)::int from public.record_draft_checkpoints where checkpoint_id='rdc_launchinsert' and draft_id='rdf_launchcheckpointinsert'`, want: 1},
		{name: "draft_checkpoint_delete", sql: `delete from public.record_draft_checkpoints
			where draft_id='rdf_launchcheckpointdelete' and checkpoint_expires_at<=transaction_timestamp()`,
			check: `select case when exists(select 1 from public.record_draft_checkpoints where checkpoint_id='rdc_launchdelete') then 0 else 1 end`, want: 1},
		{name: "draft_delete", sql: `delete from public.record_drafts where draft_id='rdf_launchdraftdelete' and author_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa'`,
			check: `select case when exists(select 1 from public.record_drafts where draft_id='rdf_launchdraftdelete') then 0 else 1 end`, want: 1},
	}
}

// The owner-seeded scope tests only the SQL guard predicate in this isolated
// fixture. It is not verified ledger, core-purge, or recovery acceptance.
func birthLaunchAssertClosedDraftCleanupScope(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, digest string) {
	t.Helper()
	const insertScope = `insert into record_platform_internal.record_deletion_effect_scopes
		(transaction_id,backend_pid,record_id,operation_id,authority_kind,authority_digest,effect_kind)
		values($1::text::xid8,$2,$3,'rpo_launchguard',$4,decode(repeat('32',32),'hex'),$5)`
	t.Run("closed_scope/runtime_cannot_insert", func(t *testing.T) {
		tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		var xid string
		var pid int32
		if err := tx.QueryRow(ctx, `select pg_current_xact_id()::text,pg_backend_pid()`).Scan(&xid, &pid); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, insertScope, xid, pid, "rec_rowlock", "online_v2", "core")
		requirePostgresSQLState(t, err, "42501")
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := owner.QueryRow(ctx, `select count(*)::int from record_platform_internal.record_deletion_effect_scopes
			where transaction_id=$1::text::xid8 and backend_pid=$2 and operation_id='rpo_launchguard'`, xid, pid).Scan(&count); err != nil || count != 0 {
			t.Fatalf("runtime scope insertion left %d rows, error=%v", count, err)
		}
	})

	type scopeCase struct {
		name, record, authority, effect string
		wrongBackend, wrongTransaction  bool
		allow                           bool
	}
	probe := func(t *testing.T, tc scopeCase, surface string) {
		t.Helper()
		tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err := tx.Exec(ctx, `select set_config('houfeng.birth_launch_digest',$1,true)`, digest); err != nil {
			t.Fatal(err)
		}
		var xid string
		var pid int32
		if err := tx.QueryRow(ctx, `select pg_current_xact_id()::text,pg_backend_pid()`).Scan(&xid, &pid); err != nil {
			t.Fatal(err)
		}
		scopeXID, scopePID := xid, pid
		if tc.wrongBackend {
			if err := owner.QueryRow(ctx, `select pg_backend_pid()`).Scan(&scopePID); err != nil || scopePID == pid {
				t.Fatalf("fixture backend identity = %d, runtime=%d error=%v", scopePID, pid, err)
			}
		}
		if tc.wrongTransaction {
			if err := owner.QueryRow(ctx, `select pg_current_xact_id()::text`).Scan(&scopeXID); err != nil || scopeXID == xid {
				t.Fatalf("fixture transaction identity = %q, runtime=%q error=%v", scopeXID, xid, err)
			}
		}
		if _, err := owner.Exec(ctx, insertScope, scopeXID, scopePID, tc.record, tc.authority, tc.effect); err != nil {
			t.Fatalf("seed isolated scope predicate: %v", err)
		}
		defer func() {
			if _, err := owner.Exec(context.Background(), `delete from record_platform_internal.record_deletion_effect_scopes
				where transaction_id=$1::text::xid8 and backend_pid=$2 and record_id=$3 and operation_id='rpo_launchguard' and effect_kind=$4`,
				scopeXID, scopePID, tc.record, tc.effect); err != nil {
				t.Errorf("remove isolated scope fixture: %v", err)
			}
		}()
		checkpointSQL := `delete from public.record_draft_checkpoints where checkpoint_id='rdc_launchbound'`
		draftSQL := `delete from public.record_drafts where draft_id='rdf_launchbound'`
		if !tc.allow {
			statement := checkpointSQL
			if surface == "draft" {
				statement = draftSQL
			}
			_, err := tx.Exec(ctx, statement)
			requirePostgresSQLState(t, err, "42501")
		} else {
			for _, statement := range []string{checkpointSQL, draftSQL} {
				result, err := tx.Exec(ctx, statement)
				if err != nil || result.RowsAffected() != 1 {
					t.Fatalf("scoped draft cleanup rows=%d error=%v", result.RowsAffected(), err)
				}
			}
			var drafts, checkpoints int
			if err := tx.QueryRow(ctx, `select
				(select count(*)::int from public.record_drafts where draft_id='rdf_launchbound'),
				(select count(*)::int from public.record_draft_checkpoints where checkpoint_id='rdc_launchbound')`).Scan(&drafts, &checkpoints); err != nil || drafts != 0 || checkpoints != 0 {
				t.Fatalf("scoped cleanup effects = %d drafts/%d checkpoints, error=%v", drafts, checkpoints, err)
			}
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var drafts, checkpoints int
		if err := owner.QueryRow(ctx, `select
			(select count(*)::int from public.record_drafts where draft_id='rdf_launchbound'),
			(select count(*)::int from public.record_draft_checkpoints where checkpoint_id='rdc_launchbound')`).Scan(&drafts, &checkpoints); err != nil || drafts != 1 || checkpoints != 1 {
			t.Fatalf("rolled-back cleanup = %d drafts/%d checkpoints, error=%v", drafts, checkpoints, err)
		}
	}
	for _, tc := range []scopeCase{
		{name: "core", record: "rec_rowlock", authority: "online_v2", effect: "core", allow: true},
		{name: "recovery", record: "rec_rowlock", authority: "recovery_v1", effect: "recovery", allow: true},
	} {
		t.Run("closed_scope/"+tc.name, func(t *testing.T) { probe(t, tc, "both") })
	}
	for _, tc := range []scopeCase{
		{name: "wrong_record", record: "rec_wrongscope", authority: "online_v2", effect: "core"},
		{name: "wrong_effect", record: "rec_rowlock", authority: "online_v1", effect: "evidence"},
		{name: "wrong_backend", record: "rec_rowlock", authority: "online_v2", effect: "core", wrongBackend: true},
		{name: "wrong_transaction", record: "rec_rowlock", authority: "online_v2", effect: "core", wrongTransaction: true},
	} {
		for _, surface := range []string{"checkpoint", "draft"} {
			t.Run("closed_scope/"+tc.name+"/"+surface, func(t *testing.T) { probe(t, tc, surface) })
		}
	}
}
