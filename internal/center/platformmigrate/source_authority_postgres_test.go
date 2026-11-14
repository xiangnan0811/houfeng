package platformmigrate

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	witnessMigrations "houfeng/db/deletionwitness/migrations"
	applicationMigrations "houfeng/db/migrations"
)

func TestSourceAuthorityRolesValidate(t *testing.T) {
	valid := SourceAuthorityRoles{Owner: "source_owner", Reader: "source_reader", Producer: "source_producer", Provisioner: "source_provisioner"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "Arole", "1role", "role-name", "role\x00name", "rôle", strings.Repeat("a", 64)} {
		roles := valid
		roles.Owner = name
		if err := roles.Validate(); err == nil {
			t.Errorf("accepted invalid identifier %q", name)
		}
	}
	valid.Reader = valid.Producer
	if err := valid.Validate(); err == nil {
		t.Fatal("accepted duplicate principals")
	}
}

func TestPostgresIntegrationSourceAuthorityProvisioning(t *testing.T) {
	t.Run("production_roles_and_exact_repeat", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		ctx := context.Background()
		before := f.w1Owners(t)
		identity, err := ProvisionSourceAuthority(ctx, f.db, f.roles)
		if err != nil {
			t.Fatalf("initial provisioning: %v", err)
		}
		again, err := ProvisionSourceAuthority(ctx, f.db, f.roles)
		if err != nil || again != identity {
			t.Fatalf("exact repeat identity = %+v, error = %v, want %+v", again, err, identity)
		}
		if after := f.w1Owners(t); after != before {
			t.Fatalf("W1/domain ownership changed: before %q, after %q", before, after)
		}
		var ownerLogin bool
		if err := f.db.QueryRow(ctx, `select rolcanlogin from pg_roles where rolname=$1`, f.roles.Owner).Scan(&ownerLogin); err != nil || ownerLogin {
			t.Fatalf("owner LOGIN = %v, error = %v", ownerLogin, err)
		}
		reader := f.login(t, f.roles.Reader)
		producer := f.login(t, f.roles.Producer)
		provisioner := f.login(t, f.roles.Provisioner)
		for _, pool := range []*pgxpool.Pool{reader, producer, provisioner} {
			var generation int64
			if err := pool.QueryRow(ctx, `select generation from public.source_authority_snapshot()`).Scan(&generation); err != nil || generation != 1 {
				t.Fatalf("direct principal snapshot generation=%d, error=%v", generation, err)
			}
			for _, table := range []string{"source_retirement_prepares", "source_retirement_terminals", "source_retirement_unknown", "deletion_witness_entries", "source_application_enrollments", "source_application_activations"} {
				if _, err := pool.Exec(ctx, "select * from public."+table+" limit 0"); err != nil {
					t.Fatalf("read %s as direct principal: %v", table, err)
				}
			}
			assertSourceAuthorityDenied(t, pool, `delete from public.source_authority_state`)
			assertSourceAuthorityDenied(t, pool, `insert into public.source_authority_principals values ('invalid','producer',1,null)`)
			assertSourceAuthorityDenied(t, pool, `select record_platform_internal.source_authority_lock('producer',null)`)
		}
		prepare := `select retirement_id from public.source_retirement_prepare($1,$2,'default','vps',$3,$4)`
		deployment := "dp-" + strings.Repeat("a", 64)
		source := "vps_" + strings.Repeat("b", 16)
		fingerprint := bytes.Repeat([]byte{1}, 32)
		app := f.applicationIdentity(t, "one")
		otherApp := f.applicationIdentity(t, "two")
		if app.PostgresSystemIdentifier == identity.PostgresSystemIdentifier {
			t.Fatal("APP and witness fixtures must have independent PostgreSQL system identifiers")
		}
		enroll := `select public.source_application_enroll($1,$2,$3,$4::oid,$5,$6)`
		activate := `select public.source_application_activate($1)`
		activation := bytes.Repeat([]byte{9}, 32)
		enrollment := []any{deployment, app.ID, app.PostgresSystemIdentifier, app.DatabaseOID, app.DatabaseName, app.IdentityEpoch}
		assertSourceAuthorityDenied(t, producer, prepare, "retirement_1", deployment, source, fingerprint)
		for _, pool := range []*pgxpool.Pool{reader, producer} {
			assertSourceAuthorityDenied(t, pool, enroll, enrollment...)
			assertSourceAuthorityDenied(t, pool, activate, activation)
		}
		for _, pool := range []*pgxpool.Pool{reader, producer, provisioner} {
			assertSourceAuthorityDenied(t, pool, `insert into public.source_application_enrollments
				(generation,deployment_id,application_domain_id,application_system_identifier,application_database_oid,application_database_name,application_identity_epoch)
				values (1,$1,$2,$3,$4::oid,$5,$6)`, enrollment...)
			assertSourceAuthorityDenied(t, pool, `insert into public.source_application_activations(generation,activation_digest) values (1,$1)`, activation)
		}
		assertSourceAuthorityError(t, provisioner, "55000", activate, activation)
		assertSourceAuthorityError(t, provisioner, "55000", enroll, deployment, identity.ID,
			identity.PostgresSystemIdentifier, identity.DatabaseOID, identity.DatabaseName, identity.IdentityEpoch)
		if _, err := provisioner.Exec(ctx, enroll, enrollment...); err != nil {
			t.Fatalf("enroll independent APP: %v", err)
		}
		if _, err := provisioner.Exec(ctx, enroll, enrollment...); err != nil {
			t.Fatalf("exact APP enrollment replay: %v", err)
		}
		assertSourceAuthorityError(t, provisioner, "23505", enroll, deployment, otherApp.ID,
			otherApp.PostgresSystemIdentifier, otherApp.DatabaseOID, otherApp.DatabaseName, otherApp.IdentityEpoch)
		assertSourceAuthorityDenied(t, producer, prepare, "retirement_1", deployment, source, fingerprint)
		assertSourceAuthorityError(t, provisioner, "55000", activate, activation)
		if _, err := provisioner.Exec(ctx, `select public.source_enrollment_credentials($1,$2,$3,$4)`,
			[]string{"runtime", "admin", "worker", "migrator", "producer"},
			[]string{"app_runtime", "app_admin", "app_worker", "app_migrator", f.roles.Producer},
			[][]byte{bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32),
				bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{5}, 32)},
			[][]byte{bytes.Repeat([]byte{11}, 32), bytes.Repeat([]byte{12}, 32), bytes.Repeat([]byte{13}, 32),
				bytes.Repeat([]byte{14}, 32), bytes.Repeat([]byte{15}, 32)}); err != nil {
			t.Fatalf("enroll five initial credential commitments: %v", err)
		}
		assertSourceAuthorityDenied(t, producer, prepare, "retirement_1", deployment, source, fingerprint)
		if _, err := provisioner.Exec(ctx, activate, activation); err != nil {
			t.Fatalf("activate enrolled APP: %v", err)
		}
		if _, err := provisioner.Exec(ctx, activate, activation); err != nil {
			t.Fatalf("exact APP activation replay: %v", err)
		}
		assertSourceAuthorityError(t, provisioner, "23505", activate, bytes.Repeat([]byte{8}, 32))
		assertSourceAuthorityDenied(t, producer, prepare, "retirement_wrong_deployment", "dp-"+strings.Repeat("c", 64), source, fingerprint)
		var prepareCount int
		if err := reader.QueryRow(ctx, `select count(*) from public.source_retirement_prepares`).Scan(&prepareCount); err != nil || prepareCount != 0 {
			t.Fatalf("rejected prepares left %d rows, error=%v", prepareCount, err)
		}
		assertSourceAuthorityDenied(t, reader, prepare, "retirement_1", deployment, source, fingerprint)
		assertSourceAuthorityDenied(t, provisioner, prepare, "retirement_1", deployment, source, fingerprint)
		var retirement string
		if err := producer.QueryRow(ctx, prepare, "retirement_1", deployment, source, fingerprint).Scan(&retirement); err != nil || retirement != "retirement_1" {
			t.Fatalf("producer prepare = %q, error = %v", retirement, err)
		}
		if err := producer.QueryRow(ctx, prepare, "retirement_1", deployment, source, fingerprint).Scan(&retirement); err != nil {
			t.Fatalf("producer exact prepare replay: %v", err)
		}
		if _, err := producer.Exec(ctx, `select public.source_retirement_append($1,$2,$3,$4)`, retirement,
			bytes.Repeat([]byte{2}, 32), []byte{3}, []byte{4}); err != nil {
			t.Fatalf("producer append under managed NOLOGIN owner: %v", err)
		}
		assertSourceAuthorityDenied(t, producer, `select public.source_authority_bind($1::name,'reader',1,null)`, f.roles.Reader)
		assertSourceAuthorityDenied(t, producer, `select public.source_recovery_begin('recover_1',$1,$2::name)`, bytes.Repeat([]byte{1}, 32), f.roles.Reader)
		assertSourceAuthorityDenied(t, producer, `select public.source_retirement_finalize('recover_1','retirement_1',$1,$2,$3)`, bytes.Repeat([]byte{2}, 32), []byte{3}, []byte{4})
		assertSourceAuthorityDenied(t, producer, `select public.source_enrollment_credentials($1,$2,$3,$4)`,
			[]string{"runtime", "admin", "worker", "migrator", "producer"},
			[]string{"app_runtime", "app_admin", "app_worker", "app_migrator", f.roles.Producer},
			[][]byte{fingerprint, fingerprint, fingerprint, fingerprint, fingerprint},
			[][]byte{fingerprint, fingerprint, fingerprint, fingerprint, fingerprint})
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'reader',1,null)`, f.roles.Reader); err != nil {
			t.Fatalf("provisioner exact binding: %v", err)
		}
		if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err != nil {
			t.Fatalf("repeat with committed prepare and terminal: %v", err)
		}
	})

	t.Run("preexisting_membership_both_directions", func(t *testing.T) {
		for _, incoming := range []bool{false, true} {
			t.Run(map[bool]string{false: "principal_is_member", true: "principal_has_member"}[incoming], func(t *testing.T) {
				f := newSourceAuthorityFixture(t)
				parent, child := f.roles.Reader, f.roles.Producer
				if incoming {
					parent, child = f.roles.Owner, f.roles.Reader
				}
				f.exec(t, "grant "+pgx.Identifier{parent}.Sanitize()+" to "+pgx.Identifier{child}.Sanitize())
				f.reject(t, "membership")
				f.assertUnenrolled(t)
			})
		}
	})

	t.Run("preexisting_privilege_not_repaired", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.exec(t, "grant insert on public.source_retirement_prepares to "+pgx.Identifier{f.roles.Producer}.Sanitize())
		f.reject(t, "privilege")
		f.assertUnenrolled(t)
		var retained bool
		if err := f.db.QueryRow(context.Background(), `select has_table_privilege($1,'public.source_retirement_prepares','INSERT')`, f.roles.Producer).Scan(&retained); err != nil || !retained {
			t.Fatalf("hostile privilege was silently removed: retained=%v, error=%v", retained, err)
		}
	})

	t.Run("unrelated_ownership", func(t *testing.T) {
		for _, owner := range []string{"owner", "reader"} {
			t.Run(owner, func(t *testing.T) {
				f := newSourceAuthorityFixture(t)
				role := f.roles.Owner
				if owner == "reader" {
					role = f.roles.Reader
				}
				f.exec(t, "create schema unrelated authorization "+pgx.Identifier{role}.Sanitize())
				f.reject(t, "owns")
				f.assertUnenrolled(t)
			})
		}
	})

	t.Run("repeat_detects_acl_and_owner_drift", func(t *testing.T) {
		for _, drift := range []string{"extra_dml", "missing_select", "grant_option", "owner", "public_column", "unknown_function"} {
			t.Run(drift, func(t *testing.T) {
				f := newSourceAuthorityFixture(t)
				f.provision(t)
				var statement string
				switch drift {
				case "extra_dml":
					statement = "grant delete on public.source_retirement_terminals to " + pgx.Identifier{f.roles.Producer}.Sanitize()
				case "missing_select":
					statement = "revoke select on public.source_retirement_unknown from " + pgx.Identifier{f.roles.Reader}.Sanitize()
				case "grant_option":
					statement = "grant select on public.source_retirement_prepares to " + pgx.Identifier{f.roles.Reader}.Sanitize() + " with grant option"
				case "owner":
					statement = "alter function public.source_authority_snapshot() owner to postgres"
				case "public_column":
					statement = "grant update(generation) on public.source_authority_state to public"
				case "unknown_function":
					statement = "create function public.source_unexpected() returns void language plpgsql as $$ begin return; end $$"
				}
				f.exec(t, statement)
				if _, err := ProvisionSourceAuthority(context.Background(), f.db, f.roles); err == nil {
					t.Fatal("repeat accepted catalog/ACL drift")
				}
			})
		}
	})

	t.Run("owner_must_stay_nologin", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		f.exec(t, "alter role "+pgx.Identifier{f.roles.Owner}.Sanitize()+" login")
		f.reject(t, "NOLOGIN")
	})

	t.Run("original_mapping_is_immutable", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		changed := f.roles
		changed.Reader, changed.Producer = changed.Producer, changed.Reader
		if _, err := ProvisionSourceAuthority(context.Background(), f.db, changed); err == nil {
			t.Fatal("accepted changed original principal mapping")
		}
	})

	t.Run("recovery_interlock_preserves_privileges", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		ctx := context.Background()
		// Obtain a real recovery binding through the provisioner. The initializer
		// itself never creates or grants a recovery principal.
		recovery := f.roles.Producer + "_r"
		f.createRole(t, recovery, true)
		provisioner := f.login(t, f.roles.Provisioner)
		if _, err := provisioner.Exec(ctx, `select public.source_enrollment_credentials($1,$2,$3,$4)`,
			[]string{"runtime", "admin", "worker", "migrator", "producer"},
			[]string{"app_runtime", "app_admin", "app_worker", "app_migrator", f.roles.Producer},
			[][]byte{bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32),
				bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{5}, 32)},
			[][]byte{bytes.Repeat([]byte{11}, 32), bytes.Repeat([]byte{12}, 32), bytes.Repeat([]byte{13}, 32),
				bytes.Repeat([]byte{14}, 32), bytes.Repeat([]byte{15}, 32)}); err != nil {
			t.Fatalf("enroll initial credential commitments: %v", err)
		}
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'recovery',2,'recovery_1')`, recovery); err != nil {
			t.Fatalf("bind recovery principal: %v", err)
		}
		if _, err := provisioner.Exec(ctx, `select public.source_recovery_begin('recovery_1',$1,$2::name)`, bytes.Repeat([]byte{7}, 32), recovery); err != nil {
			t.Fatalf("begin recovery: %v", err)
		}
		before := f.aclFingerprint(t)
		f.reject(t, "interlock")
		if after := f.aclFingerprint(t); after != before {
			t.Fatal("recovering initializer mutated privileges")
		}
		// Model the later activation cutpoint, while handoff remains incomplete.
		// No activation API is granted to the initializer's roles.
		f.exec(t, `update public.source_authority_state set mode='active'`)
		f.exec(t, `update public.source_authority_recoveries set phase='witness_activated'`)
		f.reject(t, "interlock")
		if after := f.aclFingerprint(t); after != before {
			t.Fatal("active/incomplete-RID initializer mutated privileges")
		}
	})

	t.Run("recovery_credential_secret_nonreuse", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.provision(t)
		ctx := context.Background()
		provisioner := f.login(t, f.roles.Provisioner)
		purposes := []string{"runtime", "admin", "worker", "migrator", "producer"}
		oldPrincipals := []string{"app_runtime", "app_admin", "app_worker", "app_migrator", f.roles.Producer}
		// Use the production canonical domains with synthetic test-only secrets.
		digest := func(domain string, fields ...string) []byte {
			h := sha256.New()
			for _, field := range append([]string{domain}, fields...) {
				var length [4]byte
				binary.BigEndian.PutUint32(length[:], uint32(len(field)))
				_, _ = h.Write(length[:])
				_, _ = h.Write([]byte(field))
			}
			return h.Sum(nil)
		}
		credentialSet := func(principals, secrets []string) ([][]byte, [][]byte) {
			commitments, fingerprints := make([][]byte, 5), make([][]byte, 5)
			for i := range purposes {
				commitments[i] = digest("HOUFENG-SOURCE-CREDENTIAL-V2", purposes[i], principals[i], secrets[i])
				fingerprints[i] = digest("HOUFENG-SOURCE-CREDENTIAL-SECRET-V2", secrets[i])
			}
			return commitments, fingerprints
		}
		oldSecrets := []string{"old-runtime-secret", "old-admin-secret", "old-worker-secret", "old-migrator-secret", "old-producer-secret"}
		oldCommitments, oldFingerprints := credentialSet(oldPrincipals, oldSecrets)
		enroll := `select public.source_enrollment_credentials($1,$2,$3,$4)`
		assertSourceAuthorityError(t, provisioner, "55000", enroll, purposes, oldPrincipals, oldCommitments, nil)
		invalidInitial := append([][]byte(nil), oldFingerprints...)
		invalidInitial[3] = make([]byte, 32)
		assertSourceAuthorityError(t, provisioner, "22023", enroll, purposes, oldPrincipals, oldCommitments, invalidInitial)
		var initialCount int
		if err := f.db.QueryRow(ctx, `select count(*) from public.source_authority_credentials`).Scan(&initialCount); err != nil || initialCount != 0 {
			t.Fatalf("invalid enrollment left credential rows: count=%d error=%v", initialCount, err)
		}
		if _, err := provisioner.Exec(ctx, enroll, purposes, oldPrincipals, oldCommitments, oldFingerprints); err != nil {
			t.Fatalf("enroll initial credentials: %v", err)
		}
		if _, err := provisioner.Exec(ctx, enroll, purposes, oldPrincipals, oldCommitments, oldFingerprints); err != nil {
			t.Fatalf("exact enrollment replay: %v", err)
		}
		changedInitial := append([][]byte(nil), oldFingerprints...)
		changedInitial[0] = bytes.Repeat([]byte{99}, 32)
		assertSourceAuthorityError(t, provisioner, "23505", enroll, purposes, oldPrincipals, oldCommitments, changedInitial)

		recoveryRole := f.roles.Producer + "_r"
		newProducer := f.roles.Producer + "_new"
		f.createRole(t, recoveryRole, true)
		f.createRole(t, newProducer, false)
		assertSourceAuthorityDenied(t, provisioner, `select public.source_authority_bind($1::name,'producer',1,null)`, newProducer)
		assertSourceAuthorityDenied(t, provisioner, `select public.source_authority_bind($1::name,'reader',1,null)`, newProducer)
		assertSourceAuthorityDenied(t, provisioner, `select public.source_authority_bind($1::name,'recovery',2,'recovery_1')`, newProducer)
		f.exec(t, "grant usage on schema public to "+pgx.Identifier{recoveryRole}.Sanitize())
		f.exec(t, "grant execute on function public.source_recovery_seal(text,bytea), public.source_recovery_credentials(text,text[],text[],bytea[],bytea[],bytea) to "+pgx.Identifier{recoveryRole}.Sanitize())
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'recovery',2,'recovery_1')`, recoveryRole); err != nil {
			t.Fatalf("bind restricted recovery role: %v", err)
		}
		if _, err := provisioner.Exec(ctx, `select public.source_recovery_begin('recovery_1',$1,$2::name)`, bytes.Repeat([]byte{7}, 32), recoveryRole); err != nil {
			t.Fatalf("begin recovery: %v", err)
		}
		assertSourceAuthorityDenied(t, provisioner, `select public.source_authority_bind($1::name,'reader',2,null)`, newProducer)
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'producer',2,null)`, newProducer); err != nil {
			t.Fatalf("bind new producer: %v", err)
		}
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'producer',2,null)`, newProducer); err != nil {
			t.Fatalf("replay NOLOGIN producer binding: %v", err)
		}
		recovery := f.login(t, recoveryRole)
		if _, err := recovery.Exec(ctx, `select public.source_recovery_seal('recovery_1',$1)`, bytes.Repeat([]byte{8}, 32)); err != nil {
			t.Fatalf("seal recovery coverage: %v", err)
		}
		newPrincipals := []string{"new_runtime", "new_admin", "new_worker", "new_migrator", newProducer}
		newSecrets := []string{"fresh-runtime-secret", "fresh-admin-secret", "fresh-worker-secret", "fresh-migrator-secret", "fresh-producer-secret"}
		commitments, fingerprints := credentialSet(newPrincipals, newSecrets)
		manifest := bytes.Repeat([]byte{9}, 32)
		stage := `select public.source_recovery_credentials('recovery_1',$1,$2,$3,$4,$5)`
		assertUnstaged := func() {
			t.Helper()
			var count int
			var phase string
			if err := f.db.QueryRow(ctx, `select (select count(*) from public.source_authority_credentials where generation=2), phase from public.source_authority_recoveries where recovery_id='recovery_1'`).Scan(&count, &phase); err != nil || count != 0 || phase != "coverage_sealed" {
				t.Fatalf("rejected stage changed rows or phase: count=%d phase=%q error=%v", count, phase, err)
			}
		}
		assertSourceAuthorityError(t, recovery, "55000", stage, purposes, newPrincipals, commitments, nil, manifest)
		assertUnstaged()
		assertSourceAuthorityError(t, recovery, "55000",
			`select public.source_recovery_credentials('recovery_1',$1,$2,$3,array_fill($4::bytea,ARRAY[1,5]),$5)`,
			purposes, newPrincipals, commitments, fingerprints[0], manifest)
		assertUnstaged()
		assertSourceAuthorityError(t, recovery, "55000",
			`select public.source_recovery_credentials('recovery_1',$1,$2,$3,array_fill($4::bytea,ARRAY[5],ARRAY[0]),$5)`,
			purposes, newPrincipals, commitments, fingerprints[0], manifest)
		assertUnstaged()
		zero := append([][]byte(nil), fingerprints...)
		zero[3] = make([]byte, 32)
		assertSourceAuthorityError(t, recovery, "22023", stage, purposes, newPrincipals, commitments, zero, manifest)
		assertUnstaged()
		missing := append([][]byte(nil), fingerprints...)
		missing[3] = nil
		assertSourceAuthorityError(t, recovery, "22023", stage, purposes, newPrincipals, commitments, missing, manifest)
		assertUnstaged()
		reusedSecrets := append([]string(nil), newSecrets...)
		reusedSecrets[3] = oldSecrets[0]
		reusedCommitments, reusedFingerprints := credentialSet(newPrincipals, reusedSecrets)
		if bytes.Equal(reusedCommitments[3], oldCommitments[0]) || !bytes.Equal(reusedFingerprints[3], oldFingerprints[0]) {
			t.Fatal("fixture does not model renamed cross-purpose secret reuse")
		}
		assertSourceAuthorityError(t, recovery, "23505", stage, purposes, newPrincipals, reusedCommitments, reusedFingerprints, manifest)
		assertUnstaged()
		// Reuse within the same generation is not newly forbidden.
		newSecrets[1] = newSecrets[0]
		commitments, fingerprints = credentialSet(newPrincipals, newSecrets)
		if _, err := recovery.Exec(ctx, stage, purposes, newPrincipals, commitments, fingerprints, manifest); err != nil {
			t.Fatalf("stage fresh credentials: %v", err)
		}
		if _, err := recovery.Exec(ctx, stage, purposes, newPrincipals, commitments, fingerprints, manifest); err != nil {
			t.Fatalf("exact stage replay: %v", err)
		}
		var canLogin bool
		if err := f.db.QueryRow(ctx, `select rolcanlogin from pg_roles where rolname=$1`, newProducer).Scan(&canLogin); err != nil || canLogin {
			t.Fatalf("staging changed producer LOGIN: canLogin=%v error=%v", canLogin, err)
		}
		producerConfig := f.config.Copy()
		producerConfig.ConnConfig.User = newProducer
		producerConfig.ConnConfig.Password = f.password
		producerPool, err := pgxpool.NewWithConfig(ctx, producerConfig)
		if err != nil {
			t.Fatalf("create producer connection pool: %v", err)
		}
		err = producerPool.Ping(ctx)
		producerPool.Close()
		var authError *pgconn.PgError
		if !errors.As(err, &authError) || authError.Code != "28000" {
			t.Fatalf("NOLOGIN producer authentication error=%v, want SQLSTATE 28000", err)
		}
		changed := append([][]byte(nil), fingerprints...)
		changed[2] = bytes.Repeat([]byte{98}, 32)
		assertSourceAuthorityError(t, recovery, "23505", stage, purposes, newPrincipals, commitments, changed, manifest)
		var count int
		var phase string
		if err := f.db.QueryRow(ctx, `select (select count(*) from public.source_authority_credentials where generation=2), phase from public.source_authority_recoveries where recovery_id='recovery_1'`).Scan(&count, &phase); err != nil || count != 5 || phase != "credentials_staged" {
			t.Fatalf("stage or replay state: count=%d phase=%q error=%v", count, phase, err)
		}

		// Complete generation two through the restricted handoff API, then prove
		// generation three also rejects a secret from the older generation one.
		f.exec(t, "grant execute on function public.source_recovery_advance(text,text,text,bytea,bytea,bytea) to "+pgx.Identifier{recoveryRole}.Sanitize())
		var seal []byte
		if err := f.db.QueryRow(ctx, `select coverage_seal from public.source_authority_recoveries where recovery_id='recovery_1'`).Scan(&seal); err != nil {
			t.Fatalf("read recovery seal: %v", err)
		}
		phases := []string{"credentials_staged", "witness_activated", "app_active_login_closed", "credentials_published", "logins_open", "completed"}
		for i := range len(phases) - 1 {
			if _, err := recovery.Exec(ctx, `select public.source_recovery_advance('recovery_1',$1,$2,$3,$4,$5)`,
				phases[i], phases[i+1], seal, manifest, bytes.Repeat([]byte{byte(20 + i)}, 32)); err != nil {
				t.Fatalf("advance recovery to %s: %v", phases[i+1], err)
			}
		}
		thirdRecovery := f.roles.Producer + "_r3"
		thirdProducer := f.roles.Producer + "_p3"
		f.createRole(t, thirdRecovery, true)
		f.createRole(t, thirdProducer, true)
		f.exec(t, "grant usage on schema public to "+pgx.Identifier{thirdRecovery}.Sanitize())
		f.exec(t, "grant execute on function public.source_recovery_seal(text,bytea), public.source_recovery_credentials(text,text[],text[],bytea[],bytea[],bytea) to "+pgx.Identifier{thirdRecovery}.Sanitize())
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'recovery',3,'recovery_2')`, thirdRecovery); err != nil {
			t.Fatalf("bind second recovery role: %v", err)
		}
		if _, err := provisioner.Exec(ctx, `select public.source_recovery_begin('recovery_2',$1,$2::name)`, bytes.Repeat([]byte{10}, 32), thirdRecovery); err != nil {
			t.Fatalf("begin second recovery: %v", err)
		}
		if _, err := provisioner.Exec(ctx, `select public.source_authority_bind($1::name,'producer',3,null)`, thirdProducer); err != nil {
			t.Fatalf("bind third-generation producer: %v", err)
		}
		recovery3 := f.login(t, thirdRecovery)
		if _, err := recovery3.Exec(ctx, `select public.source_recovery_seal('recovery_2',$1)`, bytes.Repeat([]byte{11}, 32)); err != nil {
			t.Fatalf("seal second recovery: %v", err)
		}
		thirdPrincipals := []string{"third_runtime", "third_admin", "third_worker", "third_migrator", thirdProducer}
		thirdSecrets := []string{"third-runtime", "third-admin", "third-worker", oldSecrets[0], "third-producer"}
		thirdCommitments, thirdFingerprints := credentialSet(thirdPrincipals, thirdSecrets)
		assertSourceAuthorityError(t, recovery3, "23505", `select public.source_recovery_credentials('recovery_2',$1,$2,$3,$4,$5)`,
			purposes, thirdPrincipals, thirdCommitments, thirdFingerprints, manifest)
		if err := f.db.QueryRow(ctx, `select (select count(*) from public.source_authority_credentials where generation=3), phase from public.source_authority_recoveries where recovery_id='recovery_2'`).Scan(&count, &phase); err != nil || count != 0 || phase != "coverage_sealed" {
			t.Fatalf("older-generation reuse changed rows or phase: count=%d phase=%q error=%v", count, phase, err)
		}
	})

	t.Run("non_witness_relations_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		f.exec(t, `create table public.application_sentinel(id bigint)`)
		f.reject(t, "dedicated witness")
	})

	t.Run("set_role_bootstrap_rejected", func(t *testing.T) {
		f := newSourceAuthorityFixture(t)
		config := f.config.Copy()
		config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "set role "+pgx.Identifier{f.roles.Provisioner}.Sanitize())
			return err
		}
		pool, err := pgxpool.NewWithConfig(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		if _, err := ProvisionSourceAuthority(context.Background(), pool, f.roles); err == nil || !strings.Contains(err.Error(), "direct bootstrap") {
			t.Fatalf("SET ROLE bootstrap error=%v", err)
		}
	})
}

type sourceAuthorityFixture struct {
	db       *pgxpool.Pool
	config   *pgxpool.Config
	roles    SourceAuthorityRoles
	allRoles []string
	password string
}

func newSourceAuthorityFixture(t *testing.T) *sourceAuthorityFixture {
	t.Helper()
	return newSourceAuthorityFixtureWithPredecessor(t, true)
}

func newSourceAuthorityFixtureWithPredecessor(t *testing.T, predecessor bool) *sourceAuthorityFixture {
	t.Helper()
	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 through the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("source authority provisioning requires the ownership-checked ephemeral PostgreSQL runner")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL"))
	if err != nil || config == nil {
		t.Fatalf("parse ephemeral witness PostgreSQL URL: %v", err)
	}
	if config.ConnConfig.User != "postgres" {
		t.Fatal("source authority fixture requires the ephemeral direct postgres bootstrap")
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	database := "hf_w2_" + suffix
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatalf("create task-owned witness database: %v", err)
	}
	f := &sourceAuthorityFixture{config: config.Copy(), password: "w2 fixture " + suffix,
		roles: SourceAuthorityRoles{Owner: "hf_w2_o_" + suffix, Reader: "hf_w2_r_" + suffix,
			Producer: "hf_w2_p_" + suffix, Provisioner: "hf_w2_c_" + suffix}}
	f.config.ConnConfig.Database = database
	t.Cleanup(func() {
		if f.db != nil {
			f.db.Close()
		}
		_, _ = admin.Exec(context.Background(), `select pg_terminate_backend(pid) from pg_stat_activity where datname=$1 and pid<>pg_backend_pid()`, database)
		if _, err := admin.Exec(context.Background(), "drop database "+pgx.Identifier{database}.Sanitize()); err != nil {
			t.Errorf("drop task-owned witness database: %v", err)
		}
		// Membership edges may deliberately remain after a rejection test.
		for _, role := range f.allRoles {
			if _, err := admin.Exec(context.Background(), "drop role "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Errorf("drop task-owned source authority principal: %v", err)
			}
		}
	})
	f.db, err = pgxpool.NewWithConfig(ctx, f.config.Copy())
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range f.roles.names() {
		f.createRole(t, role, role != f.roles.Owner)
	}
	if predecessor {
		if err := Apply(ctx, f.db, witnessPredecessorFS(t)); err != nil {
			t.Fatalf("migrate real witness fixture: %v", err)
		}
	}
	return f
}

// The predecessor fixture models the old generic migration runner before 0003
// existed. The successor itself is only installed by atomic convergence.
func witnessPredecessorFS(t *testing.T) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{}
	for _, name := range []string{"0001_create_full_witness.sql", "0002_source_retirement_authority.sql"} {
		data, err := witnessMigrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		result[name] = &fstest.MapFile{Data: data}
	}
	return result
}

func (f *sourceAuthorityFixture) applicationIdentity(t *testing.T, suffix string) DomainIdentity {
	t.Helper()
	ctx := context.Background()
	url := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if url == "" {
		t.Fatal("independent APP PostgreSQL fixture URL is required")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse independent APP PostgreSQL URL: %v", err)
	}
	if config.ConnConfig.User != "postgres" {
		t.Fatal("APP fixture requires the ephemeral direct postgres bootstrap")
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect independent APP bootstrap: %v", err)
	}
	t.Cleanup(admin.Close)
	database := "hf_app_" + strings.TrimPrefix(f.config.ConnConfig.Database, "hf_w2_") + "_" + suffix
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatalf("create task-owned APP database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `select pg_terminate_backend(pid) from pg_stat_activity where datname=$1 and pid<>pg_backend_pid()`, database)
		if _, err := admin.Exec(context.Background(), "drop database "+pgx.Identifier{database}.Sanitize()); err != nil {
			t.Errorf("drop task-owned APP database: %v", err)
		}
	})
	config.ConnConfig.Database = database
	app, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open task-owned APP database: %v", err)
	}
	t.Cleanup(app.Close)
	if err := Apply(ctx, app, applicationMigrations.FS); err != nil {
		t.Fatalf("migrate APP fixture: %v", err)
	}
	identity, err := ProvisionPostgresDomainIdentity(ctx, app, DomainKindApplication)
	if err != nil {
		t.Fatalf("provision actual APP domain identity: %v", err)
	}
	return identity
}

func (f *sourceAuthorityFixture) createRole(t *testing.T, role string, login bool) {
	t.Helper()
	attribute := "nologin"
	if login {
		attribute = "login"
	}
	var statement string
	if err := f.db.QueryRow(context.Background(), `select format('create role %I `+attribute+` noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L', $1::text,$2::text)`, role, f.password).Scan(&statement); err != nil {
		t.Fatal(err)
	}
	f.exec(t, statement)
	f.allRoles = append(f.allRoles, role)
}

func (f *sourceAuthorityFixture) login(t *testing.T, role string) *pgxpool.Pool {
	t.Helper()
	config := f.config.Copy()
	config.ConnConfig.User = role
	config.ConnConfig.Password = f.password
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("direct principal login: %v", err)
	}
	return pool
}

func (f *sourceAuthorityFixture) exec(t *testing.T, sql string) {
	t.Helper()
	if _, err := f.db.Exec(context.Background(), sql); err != nil {
		t.Fatalf("prepare source authority fixture: %v", err)
	}
}

func (f *sourceAuthorityFixture) provision(t *testing.T) {
	t.Helper()
	if _, err := ProvisionSourceAuthority(context.Background(), f.db, f.roles); err != nil {
		t.Fatalf("provision source authority: %v", err)
	}
}

func (f *sourceAuthorityFixture) reject(t *testing.T, message string) {
	t.Helper()
	if _, err := ProvisionSourceAuthority(context.Background(), f.db, f.roles); err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("provision error=%v, want %q rejection", err, message)
	}
}

func (f *sourceAuthorityFixture) assertUnenrolled(t *testing.T) {
	t.Helper()
	var empty bool
	if err := f.db.QueryRow(context.Background(), `select not exists(select 1 from public.source_authority_state)
		and not exists(select 1 from public.source_authority_principals)`).Scan(&empty); err != nil || !empty {
		t.Fatalf("failed provisioning left enrollment: empty=%v, error=%v", empty, err)
	}
}

func (f *sourceAuthorityFixture) w1Owners(t *testing.T) string {
	t.Helper()
	var owners string
	if err := f.db.QueryRow(context.Background(), `select string_agg(c.relname||':'||r.rolname,',' order by c.relname)
		from pg_class c join pg_roles r on r.oid=c.relowner join pg_namespace n on n.oid=c.relnamespace
		where n.nspname='public' and c.relname in ('record_platform_domain_identity','record_platform_domain_attestations',
		'deletion_witness_entries','deletion_witness_head','recovery_trust_witness_entries','recovery_trust_witness_head')`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	return owners
}

func (f *sourceAuthorityFixture) aclFingerprint(t *testing.T) string {
	t.Helper()
	var fingerprint string
	if err := f.db.QueryRow(context.Background(), `select string_agg(v, E'\n' order by v) from (
		select 'r:'||oid::text||':'||relowner::text||':'||coalesce(relacl::text,'') as v from pg_class
		union all select 'p:'||oid::text||':'||proowner::text||':'||coalesce(proacl::text,'') from pg_proc
		union all select 'n:'||oid::text||':'||nspowner::text||':'||coalesce(nspacl::text,'') from pg_namespace
		union all select 'd:'||oid::text||':'||datdba::text||':'||coalesce(datacl::text,'') from pg_database) a`).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func assertSourceAuthorityDenied(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	assertSourceAuthorityError(t, pool, "42501", sql, args...)
}

func assertSourceAuthorityError(t *testing.T, pool *pgxpool.Pool, code, sql string, args ...any) {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("source authority error=%v, want SQLSTATE %s", err, code)
	}
}
