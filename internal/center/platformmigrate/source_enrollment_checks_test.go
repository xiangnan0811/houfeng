package platformmigrate

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSourceApplicationIdentityCanonicalAndPhysical(t *testing.T) {
	id := DomainIdentity{ID: "rd-" + strings.Repeat("a", 64), Kind: DomainKindApplication,
		IdentityEpoch: 1, Mode: "postgres_system", PostgresSystemIdentifier: "123456",
		DatabaseOID: 123, DatabaseName: "application"}
	if !validSourceApplicationIdentity(id, "123456", 123, "application") {
		t.Fatal("valid provisioned application identity rejected")
	}
	for name, mutate := range map[string]func(*DomainIdentity){
		"uppercase ID":            func(v *DomainIdentity) { v.ID = "rd-" + strings.Repeat("A", 64) },
		"short ID":                func(v *DomainIdentity) { v.ID = "rd-a" },
		"wrong kind":              func(v *DomainIdentity) { v.Kind = DomainKindDeletionWitness },
		"wrong epoch":             func(v *DomainIdentity) { v.IdentityEpoch = 2 },
		"wrong mode":              func(v *DomainIdentity) { v.Mode = "other" },
		"different system":        func(v *DomainIdentity) { v.PostgresSystemIdentifier = "123457" },
		"noncanonical system":     func(v *DomainIdentity) { v.PostgresSystemIdentifier = "0123456" },
		"different database OID":  func(v *DomainIdentity) { v.DatabaseOID++ },
		"different database name": func(v *DomainIdentity) { v.DatabaseName = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := id
			mutate(&invalid)
			if validSourceApplicationIdentity(invalid, "123456", 123, "application") {
				t.Fatal("accepted invalid or mismatched identity")
			}
		})
	}
}

// This uses an isolated, ownership-checked database and rolls back all role and
// domain fixtures. It exercises actual pg_authid/pg_control_system access.
func TestPostgresIntegrationSourceEnrollmentChecks(t *testing.T) {
	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("source enrollment checks require the ownership-checked ephemeral PostgreSQL runner")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil {
		t.Fatalf("parse PostgreSQL fixture: %v", err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open PostgreSQL fixture: %v", err)
	}
	defer admin.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	database := "hf_src_check_" + suffix
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatalf("create isolated database: %v", err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "drop database "+pgx.Identifier{database}.Sanitize()); err != nil {
			t.Errorf("drop isolated database: %v", err)
		}
	}()
	config.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open isolated database: %v", err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fixture: %v", err)
	}
	defer tx.Rollback(ctx)
	role := "hf_src_role_" + suffix
	other := "hf_src_other_" + suffix
	quotedRole, quotedOther := pgx.Identifier{role}.Sanitize(), pgx.Identifier{other}.Sanitize()
	for _, statement := range []string{
		"create role " + quotedRole + " login noinherit password 'source secret'",
		"create role " + quotedOther + " login noinherit password 'other secret'",
		`create table public.record_platform_domain_identity (
			domain_id text, domain_kind text, identity_epoch bigint, identity_mode text,
			postgres_system_identifier text, database_oid oid, database_name text)`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	credential := []SourceRoleCredential{{Principal: role, Password: "source secret"}}
	check := func(want bool) {
		t.Helper()
		err := VerifySourceRoleCredentials(ctx, tx, credential)
		if (err == nil) != want {
			t.Fatalf("credential verification success = %v, want %v: %v", err == nil, want, err)
		}
		if err != nil && strings.Contains(err.Error(), credential[0].Password) {
			t.Fatal("credential verification disclosed password")
		}
	}
	check(true)
	credential[0].Password = "wrong secret"
	check(false)
	credential[0].Password = "source secret"
	for _, pair := range [][2]string{
		{"alter role " + quotedRole + " inherit", "alter role " + quotedRole + " noinherit"},
		{"alter role " + quotedRole + " valid until '2000-01-01'", "alter role " + quotedRole + " valid until 'infinity'"},
		{"alter role " + quotedRole + " set statement_timeout = '1s'", "alter role " + quotedRole + " reset all"},
		{"alter role " + quotedRole + " in database " + pgx.Identifier{database}.Sanitize() + " set statement_timeout = '1s'", "alter role " + quotedRole + " in database " + pgx.Identifier{database}.Sanitize() + " reset all"},
		{"grant " + quotedRole + " to " + quotedOther, "revoke " + quotedRole + " from " + quotedOther},
		{"grant " + quotedOther + " to " + quotedRole, "revoke " + quotedOther + " from " + quotedRole},
	} {
		if _, err := tx.Exec(ctx, pair[0]); err != nil {
			t.Fatalf("mutate role: %v", err)
		}
		check(false)
		if _, err := tx.Exec(ctx, pair[1]); err != nil {
			t.Fatalf("restore role: %v", err)
		}
		check(true)
	}
	if err := VerifySourceRoleCredentials(ctx, tx, append(credential, credential[0])); err == nil {
		t.Fatal("accepted duplicate role")
	}
	for _, credentials := range [][]SourceRoleCredential{
		nil,
		{{Principal: role, Password: ""}},
		{{Principal: "missing_" + suffix, Password: "source secret"}},
		{{Principal: "bad\x00name", Password: "source secret"}},
	} {
		if err := VerifySourceRoleCredentials(ctx, tx, credentials); err == nil {
			t.Fatal("accepted empty, invalid, or missing credentials")
		}
	}
	if _, err := ReadSourceApplicationIdentity(ctx, tx); err == nil {
		t.Fatal("accepted unprovisioned application")
	}
	if _, err := tx.Exec(ctx, `insert into public.record_platform_domain_identity
		select $1, 'application', 1, 'postgres_system', c.system_identifier::text, d.oid, d.datname
		from pg_catalog.pg_control_system() c join pg_catalog.pg_database d on d.datname = current_database()`,
		"rd-"+strings.Repeat("a", 64)); err != nil {
		t.Fatalf("insert provisioned identity: %v", err)
	}
	identity, err := ReadSourceApplicationIdentity(ctx, tx)
	if err != nil || identity.DatabaseName != database || identity.Kind != DomainKindApplication {
		t.Fatalf("read valid physical identity: %+v, %v", identity, err)
	}
	if _, err := tx.Exec(ctx, `update public.record_platform_domain_identity set database_oid = 0::oid`); err != nil {
		t.Fatalf("mutate physical identity: %v", err)
	}
	if _, err := ReadSourceApplicationIdentity(ctx, tx); err == nil {
		t.Fatal("accepted mismatched physical identity")
	}
	if _, err := tx.Exec(ctx, `insert into public.record_platform_domain_identity select * from public.record_platform_domain_identity`); err != nil {
		t.Fatalf("duplicate provisioned identity: %v", err)
	}
	if _, err := ReadSourceApplicationIdentity(ctx, tx); err == nil {
		t.Fatal("accepted multiple provisioned identities")
	}
	if _, err := tx.Exec(ctx, "set local role "+quotedRole); err != nil {
		t.Fatalf("switch to nonbootstrap role: %v", err)
	}
	if err := VerifySourceRoleCredentials(ctx, tx, credential); err == nil {
		t.Fatal("accepted nonbootstrap credential verifier")
	}
	if _, err := ReadSourceApplicationIdentity(ctx, tx); err == nil {
		t.Fatal("accepted nonbootstrap identity reader")
	}
}
