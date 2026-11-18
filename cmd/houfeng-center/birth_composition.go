package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/config"
	"houfeng/internal/center/recordbirth"
)

var errBirthProducerConfiguration = errors.New("record birth producer configuration is invalid")
var errBirthReaderConfiguration = errors.New("record birth reader configuration is invalid")

// The continuity reader remains available independently of the optional producer.
// Each observation verifies its own configured physical identity and APP binding.
func openProductionBirthReader(
	ctx context.Context,
	cfg config.CenterConfig,
	app, sourceReader *pgxpool.Pool,
) (*recordbirth.PostgresAuthority, *pgxpool.Pool, error) {
	reader := cfg.BirthReader
	if ctx == nil || cfg.RecordPlatformMode != config.RecordPlatformModeRuntimeAdmission ||
		reader.Connection == nil || reader.Connection.ConnConfig == nil ||
		reader.IdentityFile == "" || reader.CredentialFile == "" || app == nil {
		return nil, nil, errBirthReaderConfiguration
	}
	if !distinctBirthPrincipal(reader.Connection.ConnConfig.User, app, sourceReader) {
		return nil, nil, errBirthReaderConfiguration
	}
	pool, err := pgxpool.NewWithConfig(ctx, reader.Connection.Copy())
	if err != nil {
		return nil, nil, errBirthReaderConfiguration
	}
	authority, err := recordbirth.NewConfiguredPostgresAuthority(pool, app, reader.IdentityFile)
	if err != nil {
		pool.Close()
		return nil, nil, errBirthReaderConfiguration
	}
	return authority, pool, nil
}

func distinctBirthPrincipal(name string, pools ...*pgxpool.Pool) bool {
	if name == "" {
		return false
	}
	for _, pool := range pools {
		if pool == nil {
			continue
		}
		cfg := pool.Config()
		if cfg == nil || cfg.ConnConfig == nil || cfg.ConnConfig.User == "" || cfg.ConnConfig.User == name {
			return false
		}
	}
	return true
}

// The producer pool is lazy: temporary producer failures can select the ordinary
// domain only after the independent continuity reader has succeeded.
func openProductionBirthAuthority(
	ctx context.Context,
	cfg config.CenterConfig,
	app, sourceReader, birthReader *pgxpool.Pool,
) (recordbirth.Authority, *pgxpool.Pool, error) {
	producer := cfg.BirthProducer
	if producer.Connection == nil {
		if producer.IdentityFile != "" || producer.CredentialFile != "" {
			return nil, nil, errBirthProducerConfiguration
		}
		return nil, nil, nil
	}
	if ctx == nil || cfg.RecordPlatformMode != config.RecordPlatformModeRuntimeAdmission ||
		producer.IdentityFile == "" || producer.CredentialFile == "" ||
		producer.Connection.ConnConfig == nil || app == nil {
		return nil, nil, errBirthProducerConfiguration
	}
	if !distinctBirthPrincipal(producer.Connection.ConnConfig.User, app, sourceReader, birthReader) {
		return nil, nil, errBirthProducerConfiguration
	}
	pool, err := pgxpool.NewWithConfig(ctx, producer.Connection.Copy())
	if err != nil {
		return nil, nil, errBirthProducerConfiguration
	}
	authority, err := recordbirth.NewConfiguredPostgresAuthority(pool, app, producer.IdentityFile)
	if err != nil {
		pool.Close()
		return nil, nil, errBirthProducerConfiguration
	}
	return authority, pool, nil
}
