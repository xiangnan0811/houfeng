package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/sourceenrollment"
	"houfeng/internal/center/sourceretirement"
)

const SourceMaintenanceConfigPath = "/var/lib/houfeng-maintenance/source-authority.json"

// SourceMaintenanceConfig is provisioned outside the application recovery unit.
// Both database URLs contain secrets and must never be logged.
type SourceMaintenanceConfig struct {
	GuardDirectory         string                          `json:"guard_directory"`
	GuardIdentity          MaintenanceGuardIdentity        `json:"guard_identity"`
	Witness                sourceretirement.DomainIdentity `json:"witness"`
	ReaderDatabaseURL      string                          `json:"reader_database_url"`
	BirthInstalled         bool                            `json:"birth_installed"`
	BirthLineageID         string                          `json:"birth_lineage_id"`
	BirthProtocolVersion   uint64                          `json:"birth_protocol_version"`
	BirthReaderDatabaseURL string                          `json:"birth_reader_database_url"`
}

var errSourceMaintenance = errors.New("source maintenance admission rejected")

type sourceAdmission struct {
	DeploymentID string
	Witness      sourceretirement.DomainIdentity
	Generation   int64
	Mode         string
	RecoveryID   string
}

func validateSourceConfig(path string, config SourceMaintenanceConfig) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." ||
		config.GuardDirectory != filepath.Join(filepath.Dir(path), "guard") ||
		config.ReaderDatabaseURL == "" || config.GuardIdentity.DirectoryInode == 0 || config.GuardIdentity.FileInode == 0 || config.GuardIdentity.Nonce == [32]byte{} {
		return errSourceMaintenance
	}
	// NewAuthority validates every field of the physical identity without I/O.
	if _, err := sourceretirement.NewAuthority(&pgxpool.Pool{}, config.Witness); err != nil {
		return errSourceMaintenance
	}
	if !config.BirthInstalled {
		if config.BirthLineageID != "" || config.BirthProtocolVersion != 0 || config.BirthReaderDatabaseURL != "" {
			return errSourceMaintenance
		}
	} else {
		if len(config.BirthLineageID) != len("bl-")+64 || !strings.HasPrefix(config.BirthLineageID, "bl-") || config.BirthProtocolVersion != 1 {
			return errSourceMaintenance
		}
		for _, c := range config.BirthLineageID[len("bl-"):] {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return errSourceMaintenance
			}
		}
		sourceReader, sourceErr := platformmigrate.ParseSourceAuthorityPostgresConfig(config.ReaderDatabaseURL)
		birthReader, birthErr := platformmigrate.ParseSourceAuthorityPostgresConfig(config.BirthReaderDatabaseURL)
		if sourceErr != nil || birthErr != nil || sourceReader.ConnConfig.User == birthReader.ConnConfig.User {
			return errSourceMaintenance
		}
	}
	// The production application recovery unit must never contain this anchor.
	if path == "/var/lib/houfeng" || len(path) > len("/var/lib/houfeng/") && path[:len("/var/lib/houfeng/")] == "/var/lib/houfeng/" {
		return errSourceMaintenance
	}
	return nil
}

// LoadSourceMaintenanceConfig loads a protected immutable control file. An absent
// file is distinguishable from every unsafe or malformed configuration.
func LoadSourceMaintenanceConfig(path string) (SourceMaintenanceConfig, error) {
	config, err := loadSourceMaintenanceConfig(path)
	if err != nil {
		return SourceMaintenanceConfig{}, err
	}
	if err := validateSourceConfig(path, config); err != nil {
		return SourceMaintenanceConfig{}, err
	}
	return config, nil
}

// ProvisionSourceMaintenanceConfig is an explicit trusted operator operation;
// normal participants must only load the previously published identity.
func ProvisionSourceMaintenanceConfig(path string, witness sourceretirement.DomainIdentity, readerDatabaseURL string) (SourceMaintenanceConfig, error) {
	// Reject invalid or recovery-unit locations before creating a guard.
	provisional := SourceMaintenanceConfig{GuardDirectory: filepath.Join(filepath.Dir(path), "guard"), Witness: witness, ReaderDatabaseURL: readerDatabaseURL}
	provisional.GuardIdentity.DirectoryInode, provisional.GuardIdentity.FileInode, provisional.GuardIdentity.Nonce[0] = 1, 1, 1
	if err := validateSourceConfig(path, provisional); err != nil {
		return SourceMaintenanceConfig{}, err
	}
	ctx := context.Background()
	pool, observation, err := sourceWitness(ctx, witness, readerDatabaseURL)
	if err != nil {
		return SourceMaintenanceConfig{}, err
	}
	pool.Close()
	if observation.Domain != witness {
		return SourceMaintenanceConfig{}, errSourceMaintenance
	}
	if err := finishSourceConfigPublication(path); err != nil {
		return SourceMaintenanceConfig{}, err
	}
	old, err := LoadSourceMaintenanceConfig(path)
	if err == nil {
		if old.Witness != witness || old.ReaderDatabaseURL != readerDatabaseURL {
			return SourceMaintenanceConfig{}, errSourceMaintenance
		}
		if err := WithMaintenanceGuard(ctx, old.GuardDirectory, old.GuardIdentity, true, func() error {
			current, err := LoadSourceMaintenanceConfig(path)
			if err != nil || current != old {
				return errSourceMaintenance
			}
			return syncSourceConfigReplay(path, old)
		}); err != nil {
			return SourceMaintenanceConfig{}, err
		}
		return old, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return SourceMaintenanceConfig{}, err
	}
	if err := checkSourceConfigParent(path); err != nil {
		return SourceMaintenanceConfig{}, err
	}
	guard := filepath.Join(filepath.Dir(path), "guard")
	identity, err := ProvisionMaintenanceGuard(guard)
	if err != nil {
		return SourceMaintenanceConfig{}, err
	}
	config := SourceMaintenanceConfig{GuardDirectory: guard, GuardIdentity: identity, Witness: witness, ReaderDatabaseURL: readerDatabaseURL}
	if err := validateSourceConfig(path, config); err != nil {
		return SourceMaintenanceConfig{}, err
	}
	if err := publishSourceMaintenanceConfig(path, config); err != nil {
		return SourceMaintenanceConfig{}, err
	}
	return config, nil
}

func readSourceAdmission(ctx context.Context, app *pgxpool.Pool) (*sourceAdmission, error) {
	if app == nil {
		return nil, errSourceMaintenance
	}
	var exists bool
	var hasStatus bool
	if err := app.QueryRow(ctx, `select pg_catalog.to_regclass('public.source_authority_admission') is not null, pg_catalog.to_regprocedure('public.source_maintenance_status()') is not null`).Scan(&exists, &hasStatus); err != nil {
		return nil, errSourceMaintenance
	}
	if !exists && !hasStatus {
		return nil, nil
	}
	if !exists || !hasStatus {
		return nil, errSourceMaintenance
	}
	var value sourceAdmission
	var epoch int64
	err := app.QueryRow(ctx, `select deployment_id,witness_domain_id,witness_system_identifier,witness_database_oid,witness_database_name,witness_identity_epoch,generation,mode,coalesce(recovery_id,'') from public.source_maintenance_status() where singleton`).Scan(&value.DeploymentID, &value.Witness.ID, &value.Witness.SystemIdentifier, &value.Witness.DatabaseOID, &value.Witness.DatabaseName, &epoch, &value.Generation, &value.Mode, &value.RecoveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil || epoch <= 0 || value.Generation <= 0 {
		return nil, errSourceMaintenance
	}
	value.Witness.Epoch = uint64(epoch)
	return &value, nil
}

// sourceWitness authenticates both the physical domain and the actual direct
// reader login. All errors are deliberately opaque because the DSN is secret.
func sourceWitness(ctx context.Context, domain sourceretirement.DomainIdentity, dsn string) (*pgxpool.Pool, sourceretirement.Observation, error) {
	var empty sourceretirement.Observation
	connection, err := platformmigrate.ParseSourceAuthorityPostgresConfig(dsn)
	if err != nil {
		return nil, empty, errSourceMaintenance
	}
	pool, err := pgxpool.NewWithConfig(ctx, connection)
	if err != nil {
		return nil, empty, errSourceMaintenance
	}
	fail := func() (*pgxpool.Pool, sourceretirement.Observation, error) {
		pool.Close()
		return nil, empty, errSourceMaintenance
	}
	authority, err := sourceretirement.NewAuthority(pool, domain)
	if err != nil {
		return fail()
	}
	state, err := authority.Snapshot(ctx)
	if err != nil {
		return fail()
	}
	var isReader bool
	if err := pool.QueryRow(ctx, `select session_user = current_user and exists (select 1 from public.source_authority_principals where principal=session_user and purpose='reader' and generation > 0 and generation <= $1 and recovery_id is null)`, int64(state.Generation)).Scan(&isReader); err != nil || !isReader {
		return fail()
	}
	return pool, state, nil
}

func sourceActive(admission *sourceAdmission, config *SourceMaintenanceConfig, state sourceretirement.Observation) bool {
	return admission != nil && config != nil && admission.Witness == config.Witness && state.Domain == config.Witness &&
		admission.Generation > 0 && uint64(admission.Generation) == state.Generation && admission.Mode == "active" &&
		state.Mode == "active" && state.HandoffCompleted && admission.RecoveryID == state.RecoveryID
}

// WithSourceMaintenance keeps the configured guard over the complete callback.
// Enrollment must hold its exclusive side while creating the admission row.
func WithSourceMaintenance(ctx context.Context, app *pgxpool.Pool, configPath string, exclusive bool, fn func(*SourceMaintenanceConfig) error) error {
	if ctx == nil || fn == nil {
		return errSourceMaintenance
	}
	before, err := readSourceAdmission(ctx, app)
	if err != nil {
		return err
	}
	config, err := LoadSourceMaintenanceConfig(configPath)
	if errors.Is(err, os.ErrNotExist) {
		if before != nil {
			return errSourceMaintenance
		}
		after, err := readSourceAdmission(ctx, app)
		if err != nil || after != nil {
			return errSourceMaintenance
		}
		return fn(nil)
	}
	if err != nil {
		return err
	}
	if before != nil && before.Witness != config.Witness {
		return errSourceMaintenance
	}
	return WithMaintenanceGuard(ctx, config.GuardDirectory, config.GuardIdentity, exclusive, func() error {
		// Detect replacement between the initial load and acquisition.
		current, err := LoadSourceMaintenanceConfig(configPath)
		if err != nil || current != config {
			return errSourceMaintenance
		}
		pool, state, err := sourceWitness(ctx, config.Witness, config.ReaderDatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		after, err := readSourceAdmission(ctx, app)
		if err != nil || (after != nil && after.Witness != config.Witness) {
			return errSourceMaintenance
		}
		if before != nil && after == nil {
			return errSourceMaintenance
		}
		if !exclusive {
			// A published control configuration starts the closed cutover. Neither
			// a missing APP row nor an APP commit without W2 activation reopens it.
			if !sourceActive(after, &config, state) {
				return errSourceMaintenance
			}
			authority, err := sourceretirement.NewAuthority(pool, config.Witness)
			if err != nil {
				return errSourceMaintenance
			}
			enrollment, err := authority.ReadApplicationEnrollment(ctx, after.DeploymentID)
			if err != nil || enrollment.ActivationDigest == nil || enrollment.Generation != state.Generation {
				return errSourceMaintenance
			}
		}
		return fn(&config)
	})
}

// WithSourceMaintenanceAuthority uses the narrow security-definer status
// projection, not table privileges, for authority heartbeat participants.
func WithSourceMaintenanceAuthority(ctx context.Context, app *pgxpool.Pool, configPath string, fn func(*SourceMaintenanceConfig) error) error {
	return WithSourceMaintenance(ctx, app, configPath, false, fn)
}

// VerifySourceMaintenanceCredentials checks, but never repairs, the currently
// enrolled application and witness credential commitments.
func VerifySourceMaintenanceCredentials(ctx context.Context, app *pgxpool.Pool, config *SourceMaintenanceConfig, roles platformmigrate.AppRoleSetV2, authorityRole string, passwords platformmigrate.ComposeRolePasswords) error {
	admission, err := readSourceAdmission(ctx, app)
	if err != nil {
		return err
	}
	if admission == nil {
		if config != nil {
			return errSourceMaintenance
		}
		return nil
	}
	if config == nil || roles.Validate() != nil {
		return errSourceMaintenance
	}
	pool, state, err := sourceWitness(ctx, config.Witness, config.ReaderDatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if !sourceActive(admission, config, state) {
		return errSourceMaintenance
	}
	purposes := []string{"runtime", "admin", "worker", "migrator", "authority"}
	principals := []string{roles.CenterRuntime, roles.PlatformAdmin, roles.DeletionWorker, roles.Migrator, authorityRole}
	secrets := []string{passwords.Runtime, passwords.PlatformAdmin, passwords.DeletionWorker, passwords.Migrator, passwords.Authority}
	for i, purpose := range purposes {
		if principals[i] == "" || secrets[i] == "" || uint64(len(secrets[i])) > uint64(^uint32(0)) {
			return errSourceMaintenance
		}
		commitment := sourceenrollment.CredentialCommitment(purpose, principals[i], secrets[i])
		if i < 4 {
			var matches bool
			if err := pool.QueryRow(ctx, `select exists(select 1 from public.source_authority_credentials where generation=$1 and purpose=$2 and principal::text=$3 and commitment=$4)`, admission.Generation, purpose, principals[i], commitment[:]).Scan(&matches); err != nil || !matches {
				return errSourceMaintenance
			}
		}
		var matches bool
		if err := app.QueryRow(ctx, `select exists(select 1 from public.source_application_principals where generation=$1 and purpose=$2 and principal::text=$3 and credential_commitment=$4)`, admission.Generation, purpose, principals[i], commitment[:]).Scan(&matches); err != nil || !matches {
			return errSourceMaintenance
		}
	}
	// Recheck both systems after the separate reads without holding a transaction
	// across databases; the outer maintenance guard serializes transitions.
	authority, err := sourceretirement.NewAuthority(pool, config.Witness)
	if err != nil {
		return errSourceMaintenance
	}
	state, err = authority.Snapshot(ctx)
	if err != nil {
		return errSourceMaintenance
	}
	current, err := readSourceAdmission(ctx, app)
	if err != nil || !sourceActive(current, config, state) || *current != *admission {
		return errSourceMaintenance
	}
	return nil
}
