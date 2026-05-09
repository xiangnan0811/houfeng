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

func main() {
	if err := run(); err != nil {
		slog.Error("import vps json failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseFlags()
	if err := validateOptions(opts); err != nil {
		return err
	}

	file, err := os.Open(opts.filePath)
	if err != nil {
		return fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()

	records, err := importing.DecodeRecords(file)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if opts.doImport {
		return runImport(ctx, records, opts.format, os.Stdout)
	}
	return runDryRun(ctx, records, opts.format, os.Stdout)
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
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	repos := importing.Repositories{}
	var cleanup func()
	var dbWarning string
	if databaseURL != "" {
		var err error
		repos, cleanup, err = openRepositories(ctx, databaseURL, false)
		if err != nil {
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
	return importing.WriteReport(output, report, format)
}

func runImport(ctx context.Context, records []importing.InputRecord, format string, output io.Writer) error {
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("HOUFENG_DATABASE_URL must be set for -import")
	}

	db, err := store.OpenPostgres(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer db.Close()

	if err := migrate.Apply(ctx, db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin vps import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	providerRepo, vpsRepo, subscriptionRepo := store.NewPostgresAssetLedgerRepositories(tx)
	report, err := importing.Import(ctx, records, importing.Repositories{
		Providers:     providerRepo,
		VPSAssets:     vpsRepo,
		Subscriptions: subscriptionRepo,
		Nodes:         store.NewPostgresNodeRepository(db),
	}, importing.Options{})
	if err != nil {
		if errors.Is(err, importing.ErrImportBlocked) {
			if writeErr := importing.WriteReport(output, report, format); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("%w: resolve validation or duplicate candidates before importing", err)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit vps import transaction: %w", err)
	}
	return importing.WriteReport(output, report, format)
}

func openRepositories(ctx context.Context, databaseURL string, applyMigrations bool) (importing.Repositories, func(), error) {
	db, err := store.OpenPostgres(ctx, databaseURL)
	if err != nil {
		return importing.Repositories{}, nil, fmt.Errorf("open postgres: %w", err)
	}
	if applyMigrations {
		if err := migrate.Apply(ctx, db); err != nil {
			db.Close()
			return importing.Repositories{}, nil, fmt.Errorf("apply migrations: %w", err)
		}
	}

	repos := importing.Repositories{
		Providers:     store.NewPostgresProviderRepository(db),
		VPSAssets:     store.NewPostgresVPSAssetRepository(db),
		Subscriptions: store.NewPostgresSubscriptionRepository(db),
		Nodes:         store.NewPostgresNodeRepository(db),
	}
	return repos, db.Close, nil
}
