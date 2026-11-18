//go:build linux

package deploy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/store"
)

type composeNamespaceAmbiguousRepository struct {
	composeLocalNamespaceRepository
}

func (r composeNamespaceAmbiguousRepository) ProvisionLocalNamespace(ctx context.Context, n attachments.LocalNamespace) (attachments.LocalNamespace, error) {
	if _, err := r.composeLocalNamespaceRepository.ProvisionLocalNamespace(ctx, n); err != nil {
		return attachments.LocalNamespace{}, err
	}
	return attachments.LocalNamespace{}, errors.New("lost provision response")
}

func (r composeNamespaceAmbiguousRepository) AdmitLocalNamespace(ctx context.Context, n attachments.LocalNamespaceAdmission) (attachments.LocalNamespace, error) {
	if _, err := r.composeLocalNamespaceRepository.AdmitLocalNamespace(ctx, n); err != nil {
		return attachments.LocalNamespace{}, err
	}
	return attachments.LocalNamespace{}, errors.New("lost admission response")
}

type composeNamespaceRecoveryRepository struct {
	composeLocalNamespaceRepository
}

func (r composeNamespaceRecoveryRepository) LoadLocalNamespace(ctx context.Context, root string) (attachments.LocalNamespace, error) {
	n, err := r.composeLocalNamespaceRepository.LoadLocalNamespace(ctx, root)
	if err == nil {
		recovery := "recovery-path"
		n.SourceRecoveryID = &recovery
	}
	return n, err
}

func TestPostgresIntegrationComposeLocalNamespace(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires the isolated PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("ownership-checked PostgreSQL runner required")
	}
	ctx := context.Background()
	baseURL := os.Getenv("HOUFENG_DATABASE_URL")
	base, err := pgxpool.ParseConfig(baseURL)
	if err != nil || base.ConnConfig.User != "postgres" {
		t.Fatal("direct postgres fixture required")
	}
	bootstrap, err := store.OpenPostgres(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer bootstrap.Close()
	assertComposeIntegrationFixtureHasNoProductionState(t, ctx, bootstrap)
	config := uniqueComposeIntegrationConfig(t, base.ConnConfig.Password)
	if _, err := bootstrap.Exec(ctx, formatComposeIntegrationDDL(t, ctx, bootstrap, `CREATE DATABASE %I`, config.DatabaseName)); err != nil {
		t.Fatal(err)
	}
	defer cleanupComposeIntegrationDatabase(t, context.Background(), bootstrap, config)
	open := func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		connection, err := composeIntegrationEndpointConfig(base, endpoint)
		if err != nil {
			return nil, err
		}
		pool, err := pgxpool.NewWithConfig(ctx, connection)
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
	if err := initializeComposeWithDependencies(ctx, config, composeLaunchIsolatedInitDependencies(open)); err != nil {
		t.Fatal(err)
	}
	admin, err := open(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.PlatformAdmin, Password: config.Passwords.PlatformAdmin})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	owner, err := open(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.BootstrapRole, Password: config.Passwords.Bootstrap})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	repo := store.NewPostgresLocalBlobRepository(admin, nil, nil)
	hasBinding := func(ctx context.Context, _ string) (bool, error) {
		var found bool
		err := admin.QueryRow(ctx, `select exists(select 1 from public.local_blob_namespaces)`).Scan(&found)
		return found, err
	}
	uid, gid := uint32(os.Geteuid()), uint32(os.Getegid())
	newPaths := func(t *testing.T) (string, string) {
		t.Helper()
		base := t.TempDir()
		metadata := filepath.Join(base, "metadata")
		if err := os.Mkdir(metadata, 0700); err != nil {
			t.Fatal(err)
		}
		return metadata, filepath.Join(base, "blobs")
	}
	t.Run("malformed journal and existing physical tree fail closed", func(t *testing.T) {
		metadata, root := newPaths(t)
		if err := os.WriteFile(filepath.Join(metadata, "identity.json"), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, repo, hasBinding); err == nil {
			t.Fatal("malformed journal accepted")
		}
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("malformed journal mutated BlobRoot")
		}
		if err := os.Remove(filepath.Join(metadata, "identity.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "legacy"), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, repo, hasBinding); err == nil {
			t.Fatal("unbound physical tree adopted")
		}
		if _, err := os.Lstat(filepath.Join(metadata, "identity.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("identity created for existing tree")
		}
	})
	t.Run("published identity survives crash and ambiguous commit", func(t *testing.T) {
		metadata, root := newPaths(t)
		id := composeNamespaceIdentity{Version: 1}
		id.NamespaceID, err = composeNamespaceUUID()
		if err != nil {
			t.Fatal(err)
		}
		id.IncarnationID, err = composeNamespaceUUID()
		if err != nil {
			t.Fatal(err)
		}
		id.RequestID, err = composeNamespaceUUID()
		if err != nil {
			t.Fatal(err)
		}
		dir, err := unix.Open(metadata, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := publishComposeNamespaceIdentity(dir, id); err != nil {
			unix.Close(dir)
			t.Fatal(err)
		}
		unix.Close(dir)
		before, err := os.ReadFile(filepath.Join(metadata, "identity.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, composeNamespaceAmbiguousRepository{repo}, hasBinding); err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, repo, hasBinding); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(metadata, "identity.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("identity changed during retry")
		}
		binding, err := repo.LoadLocalNamespace(ctx, root)
		if err != nil || binding.NamespaceID != id.NamespaceID || binding.IncarnationID != id.IncarnationID || binding.RequestID != id.RequestID || binding.AdmissionState != "active" {
			t.Fatal("retry did not retain the complete active identity")
		}
		identityPath := filepath.Join(metadata, "identity.json")
		if err := os.Rename(identityPath, identityPath+".saved"); err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, repo, hasBinding); err == nil {
			t.Fatal("missing journal adopted an existing SQL binding")
		}
		if _, err := os.Lstat(identityPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing journal was replaced")
		}
		if err := os.Rename(identityPath+".saved", identityPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, ".houfeng-local-namespace")); err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, repo, hasBinding); err == nil {
			t.Fatal("physical marker drift accepted")
		}
	})
	t.Run("source enrollment rejects before mutation", func(t *testing.T) {
		metadata, root := newPaths(t)
		_, sourceRoot := newPaths(t)
		ids := make([]string, 3)
		for i := range ids {
			ids[i], err = composeNamespaceUUID()
			if err != nil {
				t.Fatal(err)
			}
		}
		binding, err := attachments.ProvisionLocalNamespace(ctx, attachments.LocalProvisionRequest{
			Root: sourceRoot, NamespaceID: ids[0], IncarnationID: ids[1], RequestID: ids[2], OwnerUID: uid, OwnerGID: gid,
		}, repo)
		if err != nil {
			t.Fatal(err)
		}
		markerPath := filepath.Join(sourceRoot, ".houfeng-local-namespace")
		markerBefore, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatal(err)
		}
		_, err = owner.Exec(ctx, `insert into public.source_authority_admission(singleton,deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode) values(true,$1,$2,'1',1,'fixture',1,1,'active')`, "dp-"+strings.Repeat("a", 64), "rd-"+strings.Repeat("b", 64))
		if err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, root, uid, gid, repo, hasBinding); err == nil {
			t.Fatal("source-enabled database accepted")
		}
		if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("source refusal mutated BlobRoot")
		}
		entries, err := os.ReadDir(metadata)
		if err != nil || len(entries) != 0 {
			t.Fatal("source refusal mutated identity directory")
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, sourceRoot, uid, gid, repo, hasBinding); err != nil {
			t.Fatalf("matching admitted source namespace refused: %v", err)
		}
		after, err := repo.LoadLocalNamespace(ctx, sourceRoot)
		if err != nil || after != binding {
			t.Fatalf("source readback changed binding: %v", err)
		}
		markerAfter, err := os.ReadFile(markerPath)
		if err != nil || !bytes.Equal(markerBefore, markerAfter) {
			t.Fatal("source readback changed marker")
		}
		entries, err = os.ReadDir(metadata)
		if err != nil || len(entries) != 0 {
			t.Fatal("source readback mutated identity directory")
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, sourceRoot, uid, gid, composeNamespaceRecoveryRepository{repo}, hasBinding); !errors.Is(err, errComposeNamespaceSourceAdoption) {
			t.Fatalf("source recovery binding did not refuse: %v", err)
		}
		if err := os.WriteFile(markerPath, []byte("corrupt"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := initializeComposeLocalNamespace(ctx, metadata, sourceRoot, uid, gid, repo, hasBinding); !errors.Is(err, errComposeNamespaceSourceAdoption) {
			t.Fatalf("corrupt source marker did not refuse: %v", err)
		}
		markerAfter, err = os.ReadFile(markerPath)
		if err != nil || string(markerAfter) != "corrupt" {
			t.Fatal("source refusal repaired marker")
		}
	})
}

func TestComposeNamespaceIdentityRejectsNoncanonicalRecords(t *testing.T) {
	dirPath := t.TempDir()
	dir, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	for _, value := range []string{"{}\n", "{\"version\":1,\"version\":1}\n", "null\n", ""} {
		path := filepath.Join(dirPath, "identity.json")
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readComposeNamespaceIdentity(dir, uint32(os.Geteuid()), uint32(os.Getegid())); err == nil {
			t.Fatalf("accepted invalid record %q", value)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestComposeNamespaceSecretPublication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "houfeng_platform_admin_password")
	if err := publishComposeSecret(path, "admin secret", 0440, os.Getegid()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0440 {
		t.Fatal("dedicated admin secret has incorrect mode")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "admin secret" {
		t.Fatal("dedicated admin secret changed")
	}
}
