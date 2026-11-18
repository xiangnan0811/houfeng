package main

import (
	"errors"
	"fmt"
	"houfeng/internal/center/platformmigrate"
	"os"
	"strings"
	"testing"
)

func TestSourceAuthorityRejectsInvalidInvocationBeforeEnvironmentLookup(t *testing.T) {
	for i, args := range [][]string{
		nil,
		{"source-authority"},
		{"source-authority", "provision"},
		{"source-authority", "provision", "--scope", "app"},
		{"source-authority", "provision", "--scope=witness"},
		{"source-authority", "provision", "-scope", "witness"},
		{"source-authority", "provision", "--scope", "witness", "positional-secret"},
		{"source-authority", "provision", "--scope", "witness", "--help"},
		{"source-authority", "provision", "--scope", "witness", "--database-url", "postgres://bootstrap:argument-secret@host:5432/witness"},
		{"source-authority", "provision", "--scope", "witness", "--role", "role-secret"},
		{"source-authority", "recover", "--scope", "witness"},
		{"source-authority", "retire", "--scope", "witness"},
		{"provision", "source-authority", "--scope", "witness"},
	} {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			config, _, err := parseSourceAuthorityConfig(args, func(string) (string, bool) {
				t.Fatal("invalid invocation looked up environment")
				return "", false
			})
			if config != nil || !errors.Is(err, errInvalidSourceAuthorityInvocation) {
				t.Fatalf("result = config %v, error %v; want invocation rejection", config != nil, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("invocation error exposed argument data: %v", err)
			}
		})
	}
}

func TestSourceAuthorityValidatesRolesBeforeReadingBootstrapSecret(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing owner", key: sourceWitnessOwnerRoleEnv},
		{name: "missing reader", key: sourceWitnessReaderRoleEnv},
		{name: "missing producer", key: sourceWitnessProducerRoleEnv},
		{name: "missing provisioner", key: sourceWitnessProvisionerRoleEnv},
		{name: "duplicate role", key: sourceWitnessProducerRoleEnv, value: "source_reader"},
		{name: "invalid role", key: sourceWitnessOwnerRoleEnv, value: "postgres://bootstrap:role-secret@db.invalid:5432/witness"},
		{name: "whitespace role", key: sourceWitnessReaderRoleEnv, value: " source_reader "},
		{name: "overlong role", key: sourceWitnessProvisionerRoleEnv, value: strings.Repeat("a", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := sourceAuthorityTestEnvironment()
			env[tc.key] = tc.value
			config, _, err := parseSourceAuthorityConfig(sourceAuthorityTestArgs(), func(key string) (string, bool) {
				if key == sourceWitnessBootstrapDatabaseURLEnv {
					t.Fatal("invalid roles reached bootstrap secret lookup")
				}
				value, ok := env[key]
				return value, ok
			})
			if config != nil || !errors.Is(err, errInvalidSourceAuthorityConfiguration) {
				t.Fatalf("result = config %v, error %v; want configuration rejection", config != nil, err)
			}
			if strings.Contains(err.Error(), "role-secret") || (tc.value != "" && strings.Contains(err.Error(), tc.value)) {
				t.Fatalf("configuration error exposed role input: %v", err)
			}
		})
	}
}

func TestSourceAuthorityRejectsMissingDedicatedBootstrapSecret(t *testing.T) {
	for _, value := range []string{"", " \t\n"} {
		env := sourceAuthorityTestEnvironment()
		env[sourceWitnessBootstrapDatabaseURLEnv] = value
		// None of these credentials is an acceptable provisioning fallback.
		env["DATABASE_URL"] = "postgres://app:app-secret@db.invalid:5432/app"
		env["HOUFENG_RECORD_APP_MIGRATOR_DATABASE_URL"] = "postgres://migrator:migrator-secret@db.invalid:5432/app"
		env["HOUFENG_SOURCE_WITNESS_DATABASE_URL"] = "postgres://reader:reader-secret@db.invalid:5432/witness"
		config, _, err := parseSourceAuthorityConfig(sourceAuthorityTestArgs(), sourceAuthorityTestLookup(t, env))
		if config != nil || !errors.Is(err, errInvalidSourceAuthorityConfiguration) {
			t.Fatalf("result = config %v, error %v; want dedicated-secret rejection", config != nil, err)
		}
	}
}

func TestSourceAuthorityRejectsUnsafeDatabaseURLsWithoutCredentialDisclosure(t *testing.T) {
	clearSourceAuthorityAmbientConfig(t)
	const prefix = "postgres://bootstrap:bootstrap-secret@db.invalid:5432/witness"
	urls := []string{
		"host=db.invalid port=5432 user=bootstrap password=bootstrap-secret dbname=witness",
		"https://bootstrap:bootstrap-secret@db.invalid:5432/witness?sslmode=disable",
		"postgres://bootstrap@db.invalid:5432/witness?sslmode=disable",
		"postgres://bootstrap:@db.invalid:5432/witness?sslmode=disable",
		"postgres://:bootstrap-secret@db.invalid:5432/witness?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@db.invalid/witness?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@:5432/witness?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@db.invalid:0/witness?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@db.invalid:65536/witness?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@db.invalid:5432?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@db.invalid:5432/one/two?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@db.invalid:5432/witness%00other?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret%00other@db.invalid:5432/witness?sslmode=disable",
		"postgres://bootstrap:bootstrap-secret@one.invalid:5432,two.invalid:5432/witness?sslmode=disable",
		prefix,
		prefix + "?sslmode=disable#fragment-secret",
		prefix + "?sslmode=disable#",
		prefix + "?sslmode=disable&sslmode=verify-full",
		prefix + "?sslmode=invalid-secret",
		prefix + "?sslmode=disable&sslpassword=%00",
		prefix + "?sslmode=disable&sslpassword=%zz",
		prefix + "?sslmode=verify-full&sslcert=/nonexistent/client-secret.crt",
	}
	for _, parameter := range []string{
		"service=service-secret", "servicefile=/nonexistent/service-secret", "passfile=/nonexistent/pass-secret",
		"dbname=other", "database=other", "user=other", "password=query-secret", "host=other.invalid", "hostaddr=127.0.0.1", "port=5433",
		"role=other", "options=-c%20role%3Dother", "search_path=public", "application_name=other",
		"pool_max_conns=2", "connect_timeout=1", "ssl=true", "target_session_attrs=read-write",
	} {
		urls = append(urls, prefix+"?sslmode=disable&"+parameter)
	}
	for i, databaseURL := range urls {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			sharedConfig, sharedErr := platformmigrate.ParseSourceAuthorityPostgresConfig(databaseURL)
			if sharedConfig != nil || sharedErr == nil {
				t.Fatalf("shared parser accepted unsafe configuration")
			}
			if strings.Contains(sharedErr.Error(), "secret") || strings.Contains(sharedErr.Error(), databaseURL) {
				t.Fatalf("shared parser error exposed database URL or credentials: %v", sharedErr)
			}
			env := sourceAuthorityTestEnvironment()
			env[sourceWitnessBootstrapDatabaseURLEnv] = databaseURL
			config, _, err := parseSourceAuthorityConfig(sourceAuthorityTestArgs(), sourceAuthorityTestLookup(t, env))
			if config != nil || !errors.Is(err, errInvalidSourceAuthorityConfiguration) {
				t.Fatalf("result = config %v, error %v; want configuration rejection", config != nil, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), databaseURL) {
				t.Fatalf("configuration error exposed database URL or credentials: %v", err)
			}
		})
	}
}

func TestSourceAuthorityRejectsAmbientPostgresOverrides(t *testing.T) {
	clearSourceAuthorityAmbientConfig(t)
	for _, variable := range []string{
		"PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSERVICE", "PGSERVICEFILE", "PGPASSFILE",
		"PGOPTIONS", "PGSSLMODE", "PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY", "PGSSLPASSWORD", "PGAPPNAME",
		"PGSSLNEGOTIATION", "PGSSLSNI", "PGTARGETSESSIONATTRS", "PGTZ", "PGCONNECT_TIMEOUT", "PGCHANNELBINDING",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(variable, "ambient-secret")
			sharedConfig, sharedErr := platformmigrate.ParseSourceAuthorityPostgresConfig(sourceAuthorityTestEnvironment()[sourceWitnessBootstrapDatabaseURLEnv])
			if sharedConfig != nil || sharedErr == nil || strings.Contains(sharedErr.Error(), "ambient-secret") {
				t.Fatalf("shared parser did not reject ambient override opaquely: %v", sharedErr)
			}
			config, _, err := parseSourceAuthorityConfig(sourceAuthorityTestArgs(), sourceAuthorityTestLookup(t, sourceAuthorityTestEnvironment()))
			if config != nil || !errors.Is(err, errInvalidSourceAuthorityConfiguration) {
				t.Fatalf("result = config %v, error %v; want ambient override rejection", config != nil, err)
			}
			if strings.Contains(err.Error(), "ambient-secret") {
				t.Fatalf("configuration error exposed ambient credentials: %v", err)
			}
		})
	}
}

func TestSourceAuthoritySharedParserAcceptsExplicitConnection(t *testing.T) {
	clearSourceAuthorityAmbientConfig(t)
	t.Setenv("HOME", t.TempDir())
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(sourceAuthorityTestEnvironment()[sourceWitnessBootstrapDatabaseURLEnv])
	if err != nil {
		t.Fatalf("shared parser rejected explicit connection: %v", err)
	}
	conn := config.ConnConfig
	if conn.User != "bootstrap" || conn.Password != "bootstrap-secret" || conn.Host != "db.invalid" || conn.Port != 5432 || conn.Database != "witness" ||
		conn.TLSConfig != nil || len(conn.Fallbacks) != 0 || len(conn.RuntimeParams) != 0 {
		t.Fatal("shared parser did not preserve the explicit connection identity and options")
	}
}

func sourceAuthorityTestArgs() []string {
	return []string{"source-authority", "provision", "--scope", "witness"}
}

func sourceAuthorityTestEnvironment() map[string]string {
	return map[string]string{
		sourceWitnessOwnerRoleEnv:            "source_owner",
		sourceWitnessReaderRoleEnv:           "source_reader",
		sourceWitnessProducerRoleEnv:         "source_producer",
		sourceWitnessProvisionerRoleEnv:      "source_provisioner",
		sourceWitnessBootstrapDatabaseURLEnv: "postgres://bootstrap:bootstrap-secret@db.invalid:5432/witness?sslmode=disable",
	}
}

func sourceAuthorityTestLookup(t *testing.T, env map[string]string) func(string) (string, bool) {
	t.Helper()
	return func(key string) (string, bool) {
		switch key {
		case sourceWitnessOwnerRoleEnv, sourceWitnessReaderRoleEnv, sourceWitnessProducerRoleEnv, sourceWitnessProvisionerRoleEnv, sourceWitnessBootstrapDatabaseURLEnv:
		default:
			t.Fatalf("source authority looked up unauthorized environment key %q", key)
		}
		value, ok := env[key]
		return value, ok
	}
}

func clearSourceAuthorityAmbientConfig(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR" {
			t.Setenv(name, "")
		}
	}
}
