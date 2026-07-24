package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/config"
	"houfeng/internal/center/importing"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

type cliOptions struct {
	filePath string
	dryRun   bool
	doImport bool
	format   string
}

type importerDeps struct {
	openFile        func(string) (io.ReadCloser, error)
	openPostgres    func(context.Context, string) (*pgxpool.Pool, error)
	closePostgres   func(*pgxpool.Pool)
	applyMigrations func(context.Context, *pgxpool.Pool) error
	admitRuntime    func(context.Context, *pgxpool.Pool) error
	output          io.Writer
}

func main() {
	if err := run(); err != nil {
		slog.Error("import vps json failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()
	mode, err := validateAndLoadRecordPlatformMode(opts)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runWithMode(ctx, opts, mode, importerDeps{})
}

func runWithDeps(ctx context.Context, opts cliOptions, deps importerDeps) error {
	mode, err := validateAndLoadRecordPlatformMode(opts)
	if err != nil {
		return err
	}
	return runWithMode(ctx, opts, mode, deps)
}

func validateAndLoadRecordPlatformMode(opts cliOptions) (config.RecordPlatformMode, error) {
	if err := validateOptions(opts); err != nil {
		return config.RecordPlatformModeLegacy, err
	}
	return config.LoadRecordPlatformMode()
}

func runWithMode(ctx context.Context, opts cliOptions, mode config.RecordPlatformMode, deps importerDeps) error {
	deps = deps.withDefaults()
	file, err := deps.openFile(opts.filePath)
	if err != nil {
		return fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()

	records, err := importing.DecodeRecords(file)
	if err != nil {
		return err
	}

	if opts.doImport {
		return runImportWithDeps(ctx, records, opts.format, mode, deps)
	}
	return runDryRunWithDeps(ctx, records, opts.format, mode, deps)
}

func parseFlags() cliOptions {
	var opts cliOptions
	flag.StringVar(&opts.filePath, "file", "", "path to VPS JSON input file")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "validate and report without writing data")
	flag.BoolVar(&opts.doImport, "import", false, "write providers, VPS assets, and subscriptions when validation passes")
	flag.StringVar(&opts.format, "format", "text", "report format: text or json")
	flag.Parse()
	return opts
}

func validateOptions(opts cliOptions) error {
	if strings.TrimSpace(opts.filePath) == "" {
		return errors.New("-file is required")
	}
	if opts.dryRun && opts.doImport {
		return errors.New("-dry-run and -import are mutually exclusive")
	}
	switch strings.ToLower(strings.TrimSpace(opts.format)) {
	case "", "text", "json":
		return nil
	default:
		return fmt.Errorf("unsupported -format %q", opts.format)
	}
}

func runDryRun(ctx context.Context, records []importing.InputRecord, format string, output io.Writer) error {
	return runDryRunWithDeps(ctx, records, format, config.RecordPlatformModeLegacy, importerDeps{output: output})
}

func runDryRunWithDeps(
	ctx context.Context,
	records []importing.InputRecord,
	format string,
	mode config.RecordPlatformMode,
	deps importerDeps,
) error {
	deps = deps.withDefaults()
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	repos := importing.Repositories{}
	var cleanup func()
	var dbWarning string
	if databaseURL != "" {
		var err error
		repos, cleanup, err = openRepositories(ctx, databaseURL, mode, deps)
		if err != nil {
			if mode == config.RecordPlatformModeRuntimeAdmission {
				return err
			}
			dbWarning = fmt.Sprintf("database check skipped: %v", err)
			repos = importing.Repositories{}
		} else {
			defer cleanup()
		}
	}

	report, err := importing.DryRun(ctx, records, repos, importing.Options{IgnoreRepositoryErrors: true})
	if err != nil {
		return err
	}
	report.AddWarning(dbWarning)
	return importing.WriteReport(deps.output, report, format)
}

func runImport(ctx context.Context, records []importing.InputRecord, format string, output io.Writer) error {
	return runImportWithDeps(ctx, records, format, config.RecordPlatformModeLegacy, importerDeps{output: output})
}

func runImportWithDeps(
	ctx context.Context,
	records []importing.InputRecord,
	format string,
	mode config.RecordPlatformMode,
	deps importerDeps,
) error {
	deps = deps.withDefaults()
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("HOUFENG_DATABASE_URL must be set for -import")
	}

	db, err := deps.openPostgres(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer deps.closePostgres(db)

	switch mode {
	case config.RecordPlatformModeLegacy:
		if err := deps.applyMigrations(ctx, db); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
	case config.RecordPlatformModeRuntimeAdmission:
		if err := deps.admitRuntime(ctx, db); err != nil {
			return fmt.Errorf("admit app runtime: %w", err)
		}
	default:
		return fmt.Errorf("unknown record-platform mode %d", mode)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin vps import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	providerRepo, vpsRepo, subscriptionRepo := store.NewPostgresAssetLedgerRepositories(tx)
	report, err := importing.Import(ctx, records, importing.Repositories{
		Providers:           providerRepo,
		VPSAssets:           vpsRepo,
		Subscriptions:       subscriptionRepo,
		MonitoringInstances: store.NewPostgresMonitoringInstanceRepository(db),
	}, importing.Options{})
	if err != nil {
		if errors.Is(err, importing.ErrImportBlocked) {
			if writeErr := importing.WriteReport(deps.output, report, format); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("%w: resolve validation or duplicate candidates before importing", err)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit vps import transaction: %w", err)
	}
	return importing.WriteReport(deps.output, report, format)
}

func openRepositories(
	ctx context.Context,
	databaseURL string,
	mode config.RecordPlatformMode,
	deps importerDeps,
) (importing.Repositories, func(), error) {
	db, err := deps.openPostgres(ctx, databaseURL)
	if err != nil {
		return importing.Repositories{}, nil, fmt.Errorf("open postgres: %w", err)
	}
	if mode == config.RecordPlatformModeRuntimeAdmission {
		if err := deps.admitRuntime(ctx, db); err != nil {
			deps.closePostgres(db)
			return importing.Repositories{}, nil, fmt.Errorf("admit app runtime: %w", err)
		}
	} else if mode != config.RecordPlatformModeLegacy {
		deps.closePostgres(db)
		return importing.Repositories{}, nil, fmt.Errorf("unknown record-platform mode %d", mode)
	}

	repos := importing.Repositories{
		Providers:           store.NewPostgresProviderRepository(db),
		VPSAssets:           store.NewPostgresVPSAssetRepository(db),
		Subscriptions:       store.NewPostgresSubscriptionRepository(db),
		MonitoringInstances: store.NewPostgresMonitoringInstanceRepository(db),
	}
	return repos, func() { deps.closePostgres(db) }, nil
}

func (d importerDeps) withDefaults() importerDeps {
	if d.openFile == nil {
		d.openFile = func(path string) (io.ReadCloser, error) {
			return os.Open(path)
		}
	}
	if d.openPostgres == nil {
		d.openPostgres = store.OpenPostgres
	}
	if d.closePostgres == nil {
		d.closePostgres = func(db *pgxpool.Pool) {
			db.Close()
		}
	}
	if d.applyMigrations == nil {
		d.applyMigrations = migrate.Apply
	}
	if d.admitRuntime == nil {
		d.admitRuntime = migrate.AdmitAppACLRuntime
	}
	if d.output == nil {
		d.output = os.Stdout
	}
	return d
}
