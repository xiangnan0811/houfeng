package platformmigrate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	ledgerMigrations "houfeng/db/deletionledger/migrations"
)

func TestRecordLedgerScopeRequiresSeparatedCredentials(t *testing.T) {
	valid := RecordLedgerScopeRoles{Reader: SourceRoleCredential{Principal: "ledger_reader", Password: "reader-password"},
		Writer: SourceRoleCredential{Principal: "ledger_writer", Password: "writer-password"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*RecordLedgerScopeRoles){
		"same role":      func(r *RecordLedgerScopeRoles) { r.Writer.Principal = r.Reader.Principal },
		"same password":  func(r *RecordLedgerScopeRoles) { r.Writer.Password = r.Reader.Password },
		"empty password": func(r *RecordLedgerScopeRoles) { r.Reader.Password = "" },
		"invalid role":   func(r *RecordLedgerScopeRoles) { r.Reader.Principal = "reader;drop" },
	} {
		t.Run(name, func(t *testing.T) {
			roles := valid
			mutate(&roles)
			if err := roles.Validate(); err == nil {
				t.Fatal("invalid role pair accepted")
			}
		})
	}
}

func TestPostgresIntegrationRecordWitnessScope(t *testing.T) {
	f := newSourceAuthorityFixture(t)
	ctx := context.Background()
	if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err != nil {
		t.Fatal(err)
	}
	suffix := strings.TrimPrefix(f.roles.Reader, "hf_w2_r_")
	reader, writer := "hf_lgr_"+suffix, "hf_lgw_"+suffix
	f.createRole(t, reader, true)
	f.createRole(t, writer, true)
	writerPassword := "record writer " + suffix
	var alter string
	if err := f.db.QueryRow(ctx, `select format('alter role %I password %L', $1::text, $2::text)`, writer, writerPassword).Scan(&alter); err != nil {
		t.Fatal(err)
	}
	f.exec(t, alter)
	roles := RecordLedgerScopeRoles{Reader: SourceRoleCredential{Principal: reader, Password: f.password}, Writer: SourceRoleCredential{Principal: writer, Password: writerPassword}}
	badCredentials := roles
	badCredentials.Writer.Password = "incorrect secret"
	if _, err := ProvisionRecordLedgerScope(ctx, f.db, DomainKindDeletionWitness, badCredentials); err == nil {
		t.Fatal("incorrect writer credential accepted")
	}
	var writerHasInsert bool
	if err := f.db.QueryRow(ctx, `select pg_catalog.has_table_privilege($1::text,'public.deletion_witness_entries','INSERT')`, writer).Scan(&writerHasInsert); err != nil || writerHasInsert {
		t.Fatalf("rejected credentials changed writer privileges: %v", err)
	}
	first, err := ProvisionRecordLedgerScope(ctx, f.db, DomainKindDeletionWitness, roles)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProvisionRecordLedgerScope(ctx, f.db, DomainKindDeletionWitness, roles)
	if err != nil || first != second {
		t.Fatalf("exact repeat: %+v %v", second, err)
	}
	if _, err := ProvisionSourceAuthority(ctx, f.db, f.roles); err != nil {
		t.Fatalf("source convergence after record scope: %v", err)
	}

	login := func(role, password string) *pgxpool.Pool {
		config := f.config.Copy()
		config.ConnConfig.User, config.ConnConfig.Password = role, password
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		if err := pool.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		return pool
	}
	readPool, writePool := login(reader, f.password), login(writer, writerPassword)
	for _, pool := range []*pgxpool.Pool{readPool, writePool} {
		var sequence int64
		if err := pool.QueryRow(ctx, `select sequence from public.deletion_witness_head where singleton`).Scan(&sequence); err != nil || sequence != 0 {
			t.Fatalf("read witness head: %d %v", sequence, err)
		}
		var system string
		if err := pool.QueryRow(ctx, `select system_identifier::text from pg_catalog.pg_control_system()`).Scan(&system); err != nil || system != first.PostgresSystemIdentifier {
			t.Fatalf("verify physical identity: %v", err)
		}
	}
	if _, err := readPool.Exec(ctx, `update public.deletion_witness_head set sequence=sequence`); err == nil {
		t.Fatal("reader changed witness head")
	}
	if _, err := writePool.Exec(ctx, `update public.deletion_witness_head set sequence=sequence`); err != nil {
		t.Fatalf("dedicated writer cannot update head: %v", err)
	}
	if _, err := writePool.Exec(ctx, `insert into public.record_birth_keys(key_id,key_bytes,key_commitment) values ($1,$2,$3)`,
		"bk-"+strings.Repeat("1", 64), make([]byte, 32), make([]byte, 32)); err == nil {
		t.Fatal("record ledger writer obtained birth key authority")
	}
	if _, err := writePool.Exec(ctx, `delete from public.deletion_witness_entries`); err == nil {
		t.Fatal("record ledger writer obtained delete authority")
	}
	if _, err := f.db.Exec(ctx, "grant select on public.source_authority_credentials to "+pgx.Identifier{reader}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionRecordLedgerScope(ctx, f.db, DomainKindDeletionWitness, roles); err == nil {
		t.Fatal("unrelated pre-existing privilege accepted")
	}
	if _, err := f.db.Exec(ctx, fmt.Sprintf("revoke select on public.source_authority_credentials from %s", pgx.Identifier{reader}.Sanitize())); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(ctx, `grant select on public.record_birth_keys to public`); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionRecordLedgerScope(ctx, f.db, DomainKindDeletionWitness, roles); err == nil {
		t.Fatal("PUBLIC access to birth keys accepted")
	}
	if _, err := f.db.Exec(ctx, `revoke select on public.record_birth_keys from public`); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIntegrationRecordPrimaryScope(t *testing.T) {
	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skip("requires the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("record ledger scope test requires the ownership-checked ephemeral runner")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DELETION_LEDGER_DATABASE_URL"))
	if err != nil || config.ConnConfig.User != "postgres" {
		t.Fatal("expected direct ephemeral ledger bootstrap")
	}
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	database, reader, writer := "hf_w1_"+suffix, "hf_w1_r_"+suffix, "hf_w1_w_"+suffix
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{database}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `select pg_catalog.pg_terminate_backend(pid) from pg_catalog.pg_stat_activity where datname=$1 and pid<>pg_catalog.pg_backend_pid()`, database)
		_, _ = admin.Exec(context.Background(), "drop database "+pgx.Identifier{database}.Sanitize())
		_, _ = admin.Exec(context.Background(), "drop role if exists "+pgx.Identifier{reader}.Sanitize()+", "+pgx.Identifier{writer}.Sanitize())
	}()
	config.ConnConfig.Database = database
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Apply(ctx, db, ledgerMigrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionPostgresDomainIdentity(ctx, db, DomainKindDeletionLedger); err != nil {
		t.Fatal(err)
	}
	credentials := []SourceRoleCredential{{Principal: reader, Password: "reader secret " + suffix}, {Principal: writer, Password: "writer secret " + suffix}}
	for _, credential := range credentials {
		var ddl string
		if err := db.QueryRow(ctx, `select pg_catalog.format('create role %I login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L', $1::text,$2::text)`, credential.Principal, credential.Password).Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	roles := RecordLedgerScopeRoles{Reader: credentials[0], Writer: credentials[1]}
	if _, err := ProvisionRecordLedgerScope(ctx, db, DomainKindDeletionLedger, roles); err != nil {
		t.Fatal(err)
	}
	login := func(credential SourceRoleCredential) *pgxpool.Pool {
		cfg := config.Copy()
		cfg.ConnConfig.User, cfg.ConnConfig.Password = credential.Principal, credential.Password
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		if err := pool.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		return pool
	}
	readPool, writePool := login(credentials[0]), login(credentials[1])
	var allowed bool
	if err := writePool.QueryRow(ctx, `select pg_catalog.has_table_privilege(current_user,'public.deletion_ledger_entries','INSERT')`).Scan(&allowed); err != nil || !allowed {
		t.Fatalf("dedicated writer cannot insert: %v", err)
	}
	if err := readPool.QueryRow(ctx, `select pg_catalog.has_table_privilege(current_user,'public.deletion_ledger_entries','INSERT')`).Scan(&allowed); err != nil || allowed {
		t.Fatalf("reader obtained insert privilege: %v", err)
	}
	var count int
	if err := readPool.QueryRow(ctx, `select count(*) from public.deletion_ledger_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read empty ledger: %d %v", count, err)
	}
	if _, err := writePool.Exec(ctx, `update public.deletion_ledger_head set sequence=sequence`); err != nil {
		t.Fatalf("dedicated writer cannot lock head: %v", err)
	}
	var digest []byte
	if err := writePool.QueryRow(ctx, `select record_platform_internal.digest($1::bytea,'sha256')`, []byte("record scope credential check")).Scan(&digest); err != nil || len(digest) != 32 {
		t.Fatalf("writer cannot evaluate ledger check digest: %v", err)
	}
	if _, err := readPool.Exec(ctx, `update public.deletion_ledger_head set sequence=sequence`); err == nil {
		t.Fatal("reader updated head")
	}
	if _, err := writePool.Exec(ctx, `delete from public.deletion_ledger_entries`); err == nil {
		t.Fatal("writer deleted ledger entries")
	}
}
