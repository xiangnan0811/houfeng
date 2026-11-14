package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/config"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/sourceretirement"
)

var centerTestDeployment = "dp-" + strings.Repeat("c", 64)

func centerTestLaunch(_ string, kind string) (recordbirth.LaunchBinding, [32]byte, error) {
	return recordbirth.LaunchBinding{ProcessKind: kind, DeploymentID: centerTestDeployment}, sha256.Sum256([]byte("center test launch")), nil
}

func centerTestVerifyLaunch(context.Context, *pgxpool.Pool, *recordbirth.PostgresAuthority, recordbirth.LaunchBinding) error {
	return nil
}

func clearBirthCompositionAmbient(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "PG") || name == "SSL_CERT_FILE" || name == "SSL_CERT_DIR" {
			t.Setenv(name, "")
		}
	}
}

func birthCompositionPool(t *testing.T, user string) *pgxpool.Pool {
	t.Helper()
	clearBirthCompositionAmbient(t)
	connection, err := pgxpool.ParseConfig("postgres://" + user + ":" + user + "-secret@127.0.0.1:1/fixture?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func birthCompositionConfig(t *testing.T, user string) config.CenterConfig {
	t.Helper()
	clearBirthCompositionAmbient(t)
	connection, err := pgxpool.ParseConfig("postgres://" + user + ":" + user + "-secret@127.0.0.1:1/fixture?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	identity := sourceretirement.DomainIdentity{
		ID: "rd-" + strings.Repeat("a", 64), SystemIdentifier: "12345",
		DatabaseOID: 16384, DatabaseName: "fixture", Epoch: 1,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "identity.json")
	if err := os.WriteFile(identityPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return config.CenterConfig{
		RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission,
		BirthProducer: config.BirthProducerConfig{
			Connection: connection, IdentityFile: identityPath, CredentialFile: "fixture-credential",
		},
	}
}

func TestOpenProductionBirthAuthority(t *testing.T) {
	t.Run("ordinary has no W2 dependency", func(t *testing.T) {
		authority, pool, err := openProductionBirthAuthority(context.Background(), config.CenterConfig{}, nil, nil, nil)
		if err != nil || authority != nil || pool != nil {
			t.Fatalf("ordinary authority = %v, pool = %v, error = %v", authority, pool, err)
		}
	})
	t.Run("incomplete configuration fails closed", func(t *testing.T) {
		cfg := config.CenterConfig{BirthProducer: config.BirthProducerConfig{IdentityFile: "fixture"}}
		if _, _, err := openProductionBirthAuthority(context.Background(), cfg, nil, nil, nil); err == nil {
			t.Fatal("partial birth producer configuration was accepted")
		}
	})
	t.Run("dedicated producer is opened without contacting W2", func(t *testing.T) {
		cfg := birthCompositionConfig(t, "birth_producer")
		app := birthCompositionPool(t, "app_runtime")
		reader := birthCompositionPool(t, "source_reader")
		authority, pool, err := openProductionBirthAuthority(context.Background(), cfg, app, reader, nil)
		if err != nil || authority == nil || pool == nil {
			t.Fatalf("dedicated birth producer: %v", err)
		}
		pool.Close()
	})
	t.Run("APP principal cannot be reused", func(t *testing.T) {
		cfg := birthCompositionConfig(t, "app_runtime")
		app := birthCompositionPool(t, "app_runtime")
		if _, _, err := openProductionBirthAuthority(context.Background(), cfg, app, nil, nil); err == nil {
			t.Fatal("APP principal was reused for birth")
		}
	})
	t.Run("source reader principal cannot be reused", func(t *testing.T) {
		cfg := birthCompositionConfig(t, "source_reader")
		app := birthCompositionPool(t, "app_runtime")
		reader := birthCompositionPool(t, "source_reader")
		if _, _, err := openProductionBirthAuthority(context.Background(), cfg, app, reader, nil); err == nil {
			t.Fatal("source reader principal was reused for birth")
		}
	})
	t.Run("invalid authority identity fails without fallback", func(t *testing.T) {
		cfg := birthCompositionConfig(t, "birth_producer")
		cfg.BirthProducer.IdentityFile += ".missing"
		app := birthCompositionPool(t, "app_runtime")
		if _, pool, err := openProductionBirthAuthority(context.Background(), cfg, app, nil, nil); err == nil || pool != nil {
			t.Fatal("invalid authority identity was accepted")
		}
	})
}

func TestOpenProductionBirthReader(t *testing.T) {
	for _, tc := range []struct {
		name       string
		readerUser string
		wantError  bool
	}{
		{"independent reader", "birth_reader", false},
		{"APP principal", "app_runtime", true},
		{"source reader principal", "source_reader", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := birthCompositionConfig(t, tc.readerUser)
			cfg.BirthReader = config.BirthReaderConfig{
				Connection: cfg.BirthProducer.Connection, IdentityFile: cfg.BirthProducer.IdentityFile,
				CredentialFile: cfg.BirthProducer.CredentialFile,
			}
			app := birthCompositionPool(t, "app_runtime")
			source := birthCompositionPool(t, "source_reader")
			reader, pool, err := openProductionBirthReader(context.Background(), cfg, app, source)
			if tc.wantError {
				if err == nil || pool != nil || reader != nil {
					t.Fatal("accepted a reused reader principal")
				}
				return
			}
			if err != nil || pool == nil || reader == nil {
				t.Fatalf("open independent reader: %v", err)
			}
			defer pool.Close()
			cfg.BirthProducer.Connection = cfg.BirthReader.Connection.Copy()
			if _, producerPool, err := openProductionBirthAuthority(context.Background(), cfg, app, source, pool); err == nil || producerPool != nil {
				t.Fatal("producer reused the birth reader principal")
			}
		})
	}
	cfg := config.CenterConfig{RecordPlatformMode: config.RecordPlatformModeRuntimeAdmission}
	if _, pool, err := openProductionBirthReader(context.Background(), cfg, nil, nil); err == nil || pool != nil {
		t.Fatal("missing installed reader was accepted")
	}
}
