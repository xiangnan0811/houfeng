package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordplatform"
)

func TestInitializeComposeUsesBootstrapThenCurrentWriterAndRuntimeAdmission(t *testing.T) {
	t.Parallel()

	config := composeInitConfigFixture(t)
	authorityState, err := recordauthority.CreateComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	bootstrapPool := &pgxpool.Pool{}
	migratorPool := &pgxpool.Pool{}
	authorityPool := &pgxpool.Pool{}
	runtimePool := &pgxpool.Pool{}
	var events []string
	err = initializeComposeWithDependencies(t.Context(), config, composeInitDependencies{
		openPostgres: func(_ context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
			events = append(events, "open:"+endpoint.Role)
			switch endpoint.Role {
			case config.BootstrapRole:
				if endpoint.Password != config.Passwords.Bootstrap {
					t.Fatal("bootstrap opener received wrong password")
				}
				return bootstrapPool, nil
			case config.Roles.Migrator:
				if endpoint.Password != config.Passwords.Migrator {
					t.Fatal("migrator opener received wrong password")
				}
				return migratorPool, nil
			case config.AuthorityRole:
				if endpoint.Password != authorityState.DatabasePassword() {
					t.Fatal("authority opener received wrong state-derived password")
				}
				return authorityPool, nil
			case config.Roles.CenterRuntime:
				if endpoint.Password != config.Passwords.Runtime {
					t.Fatal("runtime opener received wrong password")
				}
				return runtimePool, nil
			default:
				t.Fatalf("unexpected PostgreSQL login role %q", endpoint.Role)
				return nil, nil
			}
		},
		closePostgres: func(pool *pgxpool.Pool) {
			switch pool {
			case bootstrapPool:
				events = append(events, "close:bootstrap")
			case migratorPool:
				events = append(events, "close:migrator")
			case authorityPool:
				events = append(events, "close:authority")
			case runtimePool:
				events = append(events, "close:runtime")
			default:
				t.Fatal("closed unknown PostgreSQL pool")
			}
		},
		prepareAuthority: func(_ context.Context, pool *pgxpool.Pool, root string) (recordauthority.VerifiedComposeState, error) {
			events = append(events, "prepare-authority")
			if pool != bootstrapPool || root != config.AuthorityStateRoot {
				t.Fatal("authority preparation did not use bootstrap pool and fixed state root")
			}
			return authorityState, nil
		},
		provisionBootstrap: func(_ context.Context, pool *pgxpool.Pool, got platformmigrate.ComposeBootstrapConfig) error {
			events = append(events, "provision")
			if pool != bootstrapPool || got.DatabaseName != config.DatabaseName || got.BootstrapRole != config.BootstrapRole || got.AuthorityRole != config.AuthorityRole || got.Roles != config.Roles || got.Passwords != (platformmigrate.ComposeRolePasswords{
				Runtime:       config.Passwords.Runtime,
				PlatformAdmin: config.Passwords.PlatformAdmin,
				Migrator:      config.Passwords.Migrator,
				Authority:     authorityState.DatabasePassword(),
			}) {
				t.Fatal("bootstrap provisioner did not receive the fixed role contract")
			}
			return nil
		},
		activateAuthority: func(_ context.Context, pool *pgxpool.Pool, root string, state recordauthority.VerifiedComposeState) error {
			events = append(events, "activate-authority")
			if pool != migratorPool || root != config.AuthorityStateRoot || state.DeploymentID != authorityState.DeploymentID {
				t.Fatal("authority activation did not use migrator pool and verified state")
			}
			return nil
		},
		publishAuthority: func(root string, state recordauthority.VerifiedComposeState, destination string) error {
			events = append(events, "publish-authority")
			if root != config.AuthorityStateRoot || destination != config.CenterDeploymentIDPath || state.DeploymentID != authorityState.DeploymentID {
				t.Fatal("authority publication did not use verified state and fixed Center-only destination")
			}
			return nil
		},
		heartbeatAuthority: func(_ context.Context, pool *pgxpool.Pool, state recordauthority.VerifiedComposeState) error {
			events = append(events, "heartbeat-authority")
			if pool != authorityPool || state.DeploymentID != authorityState.DeploymentID {
				t.Fatal("authority heartbeat did not use constrained authority pool and verified state")
			}
			return nil
		},
		convergeCurrent: func(_ context.Context, pool *pgxpool.Pool, runtimeRole, adminRole string) error {
			events = append(events, "converge-current")
			if pool != migratorPool || runtimeRole != config.Roles.CenterRuntime || adminRole != config.Roles.PlatformAdmin {
				t.Fatal("current APP writer received wrong direct-role contract")
			}
			return nil
		},
		admitRuntime: func(_ context.Context, pool *pgxpool.Pool) error {
			events = append(events, "admit-runtime")
			if pool != runtimePool {
				t.Fatal("runtime admission did not use the direct runtime pool")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("initializeComposeWithDependencies() error = %v", err)
	}
	wantEvents := []string{
		"open:postgres",
		"prepare-authority",
		"provision",
		"close:bootstrap",
		"open:houfeng_migrator",
		"converge-current",
		"activate-authority",
		"publish-authority",
		"close:migrator",
		"open:houfeng_records_authority",
		"heartbeat-authority",
		"close:authority",
		"open:houfeng_runtime",
		"admit-runtime",
		"close:runtime",
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("Compose initializer events = %q, want %q", events, wantEvents)
	}
}

func TestNewComposeInitConfigFixesRecordsAuthorityIdentity(t *testing.T) {
	t.Parallel()

	config := composeInitConfigFixture(t)
	if got, want := config.AuthorityRole, "houfeng_records_authority"; got != want {
		t.Fatalf("Compose authority role = %q, want %q", got, want)
	}
	if !filepath.IsAbs(config.CenterDeploymentIDPath) || filepath.Base(config.CenterDeploymentIDPath) != "deployment-id" {
		t.Fatalf("Compose Center deployment identity path = %q, want fixed absolute public file", config.CenterDeploymentIDPath)
	}
	if got, want := config.AuthorityStateRoot, filepath.Join(string(filepath.Separator), "var", "lib", "houfeng", "records-authority"); got == want {
		// The fixture overrides the fixed production path with a task-owned test root.
	} else if !filepath.IsAbs(got) {
		t.Fatalf("Compose authority state root = %q, want an absolute fixed topology path", got)
	}
	config.AuthorityStateRoot = "relative"
	if !errors.Is(config.Validate(), ErrInvalidComposeInitConfig) {
		t.Fatal("Compose initializer accepted a relative authority state root")
	}
}

func TestNewComposeInitConfigRejectsReusedOperatorPasswords(t *testing.T) {
	t.Parallel()

	passwords := ComposeInitPasswords{
		Bootstrap:     "bootstrap-secret",
		Runtime:       "runtime-secret",
		PlatformAdmin: "admin-secret",
		Migrator:      "migrator-secret",
	}
	placeholderCollision := passwords
	placeholderCollision.Runtime = "state-derived-placeholder"
	if _, err := NewComposeInitConfig(placeholderCollision); err != nil {
		t.Fatalf("NewComposeInitConfig() rejected a distinct password that only matched an internal validation placeholder: %v", err)
	}
	for name, mutate := range map[string]func(*ComposeInitPasswords){
		"bootstrap and runtime": func(values *ComposeInitPasswords) { values.Runtime = values.Bootstrap },
		"runtime and admin":     func(values *ComposeInitPasswords) { values.PlatformAdmin = values.Runtime },
		"admin and migrator":    func(values *ComposeInitPasswords) { values.Migrator = values.PlatformAdmin },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := passwords
			mutate(&candidate)
			if _, err := NewComposeInitConfig(candidate); !errors.Is(err, ErrInvalidComposeInitConfig) {
				t.Fatalf("NewComposeInitConfig() error = %v, want %v", err, ErrInvalidComposeInitConfig)
			}
		})
	}
}

func TestInitializeComposeRejectsAuthorityCredentialReuseBeforeBootstrapMutation(t *testing.T) {
	t.Parallel()

	config := composeInitConfigFixture(t)
	authorityState, err := recordauthority.CreateComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	config.Passwords.Runtime = authorityState.DatabasePassword()
	bootstrapPool := &pgxpool.Pool{}
	provisioned := false
	err = initializeComposeWithDependencies(t.Context(), config, composeInitDependencies{
		openPostgres: func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error) {
			return bootstrapPool, nil
		},
		closePostgres: func(*pgxpool.Pool) {},
		prepareAuthority: func(context.Context, *pgxpool.Pool, string) (recordauthority.VerifiedComposeState, error) {
			return authorityState, nil
		},
		provisionBootstrap: func(context.Context, *pgxpool.Pool, platformmigrate.ComposeBootstrapConfig) error {
			provisioned = true
			return nil
		},
		convergeCurrent:   func(context.Context, *pgxpool.Pool, string, string) error { return nil },
		activateAuthority: func(context.Context, *pgxpool.Pool, string, recordauthority.VerifiedComposeState) error { return nil },
		publishAuthority:  func(string, recordauthority.VerifiedComposeState, string) error { return nil },
		heartbeatAuthority: func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState) error {
			return nil
		},
		admitRuntime: func(context.Context, *pgxpool.Pool) error { return nil },
	})
	if !errors.Is(err, ErrComposeInitPrepareAuthority) {
		t.Fatalf("initializeComposeWithDependencies() error = %v, want %v", err, ErrComposeInitPrepareAuthority)
	}
	if provisioned {
		t.Fatal("authority credential reuse reached bootstrap mutation")
	}
}

func TestInitializeComposeStopsAndClosesAtEveryFailedStage(t *testing.T) {
	t.Parallel()

	config := composeInitConfigFixture(t)
	authorityState, err := recordauthority.CreateComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	wantFailure := errors.New("sensitive operational failure")
	wantStageError := map[string]error{
		"open-bootstrap": ErrComposeInitOpenBootstrap,
		"prepare":        ErrComposeInitPrepareAuthority,
		"provision":      ErrComposeInitProvisionBootstrap,
		"open-migrator":  ErrComposeInitOpenMigrator,
		"converge":       ErrComposeInitConvergeCurrent,
		"activate":       ErrComposeInitActivateAuthority,
		"publish":        ErrComposeInitPublishAuthority,
		"open-authority": ErrComposeInitOpenAuthority,
		"heartbeat":      ErrComposeInitHeartbeatAuthority,
		"open-runtime":   ErrComposeInitOpenRuntime,
		"admit":          ErrComposeInitAdmitRuntime,
	}
	for _, failedStage := range []string{"open-bootstrap", "prepare", "provision", "open-migrator", "converge", "activate", "publish", "open-authority", "heartbeat", "open-runtime", "admit"} {
		failedStage := failedStage
		t.Run(failedStage, func(t *testing.T) {
			var opened, closed, prepared, provisioned, converged, activated, published, heartbeated, admitted int
			err := initializeComposeWithDependencies(t.Context(), config, composeInitDependencies{
				openPostgres: func(_ context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
					if (failedStage == "open-bootstrap" && endpoint.Role == config.BootstrapRole) ||
						(failedStage == "open-migrator" && endpoint.Role == config.Roles.Migrator) ||
						(failedStage == "open-authority" && endpoint.Role == config.AuthorityRole) ||
						(failedStage == "open-runtime" && endpoint.Role == config.Roles.CenterRuntime) {
						return nil, wantFailure
					}
					opened++
					return &pgxpool.Pool{}, nil
				},
				closePostgres: func(*pgxpool.Pool) { closed++ },
				prepareAuthority: func(context.Context, *pgxpool.Pool, string) (recordauthority.VerifiedComposeState, error) {
					prepared++
					if failedStage == "prepare" {
						return recordauthority.VerifiedComposeState{}, wantFailure
					}
					return authorityState, nil
				},
				provisionBootstrap: func(context.Context, *pgxpool.Pool, platformmigrate.ComposeBootstrapConfig) error {
					provisioned++
					if failedStage == "provision" {
						return wantFailure
					}
					return nil
				},
				activateAuthority: func(context.Context, *pgxpool.Pool, string, recordauthority.VerifiedComposeState) error {
					activated++
					if failedStage == "activate" {
						return wantFailure
					}
					return nil
				},
				publishAuthority: func(string, recordauthority.VerifiedComposeState, string) error {
					published++
					if failedStage == "publish" {
						return wantFailure
					}
					return nil
				},
				heartbeatAuthority: func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState) error {
					heartbeated++
					if failedStage == "heartbeat" {
						return wantFailure
					}
					return nil
				},
				convergeCurrent: func(context.Context, *pgxpool.Pool, string, string) error {
					converged++
					if failedStage == "converge" {
						return wantFailure
					}
					return nil
				},
				admitRuntime: func(context.Context, *pgxpool.Pool) error {
					admitted++
					if failedStage == "admit" {
						return wantFailure
					}
					return nil
				},
			})
			if err == nil {
				t.Fatal("initializer unexpectedly succeeded")
			}
			if !errors.Is(err, wantStageError[failedStage]) {
				t.Fatalf("initializer error = %v, want safe stage %v", err, wantStageError[failedStage])
			}
			if strings.Contains(err.Error(), wantFailure.Error()) {
				t.Fatalf("initializer stage error leaked underlying failure: %q", err)
			}
			if opened-closed != 0 {
				t.Fatalf("initializer leaked pools: opened=%d closed=%d", opened, closed)
			}
			if failedStage == "provision" && (converged != 0 || admitted != 0) {
				t.Fatalf("provision failure continued to converge/admit: %d/%d", converged, admitted)
			}
			if failedStage == "converge" && admitted != 0 {
				t.Fatal("convergence failure continued to runtime admission")
			}
			if prepared > 1 || provisioned > 1 || converged > 1 || activated > 1 || published > 1 || heartbeated > 1 || admitted > 1 {
				t.Fatal("initializer repeated a failed stage")
			}
		})
	}
}

func TestPublishComposeAuthorityDeploymentIDCreatesOnlyPublicDerivedCopy(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "records-authority")
	state, err := recordauthority.CreateComposeState(root)
	if err != nil {
		t.Fatalf("CreateComposeState() error = %v", err)
	}
	publicDirectory := filepath.Join(t.TempDir(), "center-config")
	if err := os.Mkdir(publicDirectory, 0o755); err != nil {
		t.Fatalf("create Center public directory: %v", err)
	}
	destination := filepath.Join(publicDirectory, "deployment-id")
	for attempt := 0; attempt < 2; attempt++ {
		if err := publishComposeAuthorityDeploymentID(root, state, destination); err != nil {
			t.Fatalf("publishComposeAuthorityDeploymentID(attempt %d) error = %v", attempt, err)
		}
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read published deployment ID: %v", err)
	}
	if got, want := string(body), string(state.DeploymentID)+"\n"; got != want {
		t.Fatalf("published deployment ID = %q, want %q", got, want)
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("published deployment ID mode = %v/error %v, want regular 0644", info, err)
	}
	entries, err := os.ReadDir(publicDirectory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "deployment-id" {
		t.Fatalf("Center public directory entries = %v/error %v, want deployment-id only", entries, err)
	}
}

func TestReconcileComposeAuthorityStateClosesRecoveryMatrix(t *testing.T) {
	t.Parallel()

	t.Run("absent state and inactive database generates once", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "records-authority")
		state, err := reconcileComposeAuthorityState(root, composeDatabaseContractState{})
		if err != nil {
			t.Fatalf("reconcileComposeAuthorityState() error = %v", err)
		}
		loaded, err := recordauthority.LoadComposeState(root)
		if err != nil {
			t.Fatalf("LoadComposeState() error = %v", err)
		}
		if state.DeploymentID != loaded.DeploymentID || state.DatabasePassword() != loaded.DatabasePassword() {
			t.Fatal("generated authority result does not match durable verified state")
		}
	})

	t.Run("valid state supports inactive recovery and exact active repeat", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "records-authority")
		original, err := recordauthority.CreateComposeState(root)
		if err != nil {
			t.Fatalf("CreateComposeState() error = %v", err)
		}
		for _, contract := range []composeDatabaseContractState{
			{},
			{Active: true, DeploymentID: original.DeploymentID},
		} {
			got, err := reconcileComposeAuthorityState(root, contract)
			if err != nil {
				t.Fatalf("reconcileComposeAuthorityState(%+v) error = %v", contract, err)
			}
			if got.DeploymentID != original.DeploymentID || got.DatabasePassword() != original.DatabasePassword() {
				t.Fatal("reconciliation replaced or changed valid authority state")
			}
		}
	})

	t.Run("active database rejects absent state", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "records-authority")
		_, err := reconcileComposeAuthorityState(root, composeDatabaseContractState{
			Active:       true,
			DeploymentID: recordplatform.DeploymentID("dp-" + strings.Repeat("1", 64)),
		})
		if err == nil {
			t.Fatal("active database accepted absent authority state")
		}
		if _, statErr := os.Lstat(root); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed reconciliation created authority path: %v", statErr)
		}
	})

	t.Run("active database rejects mismatched valid state", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "records-authority")
		original, err := recordauthority.CreateComposeState(root)
		if err != nil {
			t.Fatalf("CreateComposeState() error = %v", err)
		}
		_, err = reconcileComposeAuthorityState(root, composeDatabaseContractState{
			Active:       true,
			DeploymentID: recordplatform.DeploymentID("dp-" + strings.Repeat("2", 64)),
		})
		if err == nil {
			t.Fatal("active database accepted mismatched authority state")
		}
		loaded, loadErr := recordauthority.LoadComposeState(root)
		if loadErr != nil || loaded.DeploymentID != original.DeploymentID {
			t.Fatalf("mismatch failure changed original authority state: state=%+v error=%v", loaded, loadErr)
		}
	})

	for _, active := range []bool{false, true} {
		active := active
		t.Run(fmt.Sprintf("corrupt state is never regenerated active=%t", active), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "records-authority")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatalf("create corrupt authority root: %v", err)
			}
			marker := filepath.Join(root, "operator-recovery-marker")
			if err := os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
				t.Fatalf("write corrupt-state marker: %v", err)
			}
			contract := composeDatabaseContractState{Active: active}
			if active {
				contract.DeploymentID = recordplatform.DeploymentID("dp-" + strings.Repeat("3", 64))
			}
			if _, err := reconcileComposeAuthorityState(root, contract); err == nil {
				t.Fatal("corrupt authority state was accepted")
			}
			body, err := os.ReadFile(marker)
			if err != nil || string(body) != "preserve" {
				t.Fatalf("corrupt authority state was overwritten: body=%q error=%v", body, err)
			}
		})
	}
}

func composeInitConfigFixture(t *testing.T) ComposeInitConfig {
	t.Helper()
	config, err := NewComposeInitConfig(ComposeInitPasswords{
		Bootstrap:     "bootstrap-secret",
		Runtime:       "runtime-secret",
		PlatformAdmin: "admin-secret",
		Migrator:      "migrator-secret",
	})
	if err != nil {
		t.Fatalf("NewComposeInitConfig() error = %v", err)
	}
	config.AuthorityStateRoot = filepath.Join(t.TempDir(), "records-authority")
	config.CenterDeploymentIDPath = filepath.Join(t.TempDir(), "center-config", "deployment-id")
	return config
}
